package walletmgr

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flokiorg/go-flokicoin/chainutil"
	"github.com/flokiorg/walletd/chain"
	"github.com/flokiorg/walletd/chain/electrum"
	"github.com/flokiorg/walletd/waddrmgr"
	"github.com/flokiorg/walletd/wallet"
	"github.com/flokiorg/walletd/walletseed/bip39"
)

type WalletService struct {
	*WalletAccess
	params         *WalletParams
	electrumHealth <-chan error
	electrumClient *electrum.Client
	stop           chan struct{}
	wg             sync.WaitGroup
	synced         *int32

	accountNotif   chan *wallet.AccountNotification
	txNotif        chan *wallet.TransactionNotifications
	spentNessNotif chan *wallet.SpentnessNotifications
	healthNotif    chan error
	notifications  chan interface{}
}

func NewWalletService(params *WalletParams) *WalletService {
	return &WalletService{
		WalletAccess:   NewWalletAccess(params),
		params:         params,
		stop:           make(chan struct{}),
		accountNotif:   make(chan *wallet.AccountNotification, 10),
		txNotif:        make(chan *wallet.TransactionNotifications, 10),
		spentNessNotif: make(chan *wallet.SpentnessNotifications, 10),
		healthNotif:    make(chan error, 10),
		notifications:  make(chan interface{}, 10),
		synced:         new(int32),
	}
}

func (ws *WalletService) IsOpened() bool {
	return ws.isOpened
}

func (ws *WalletService) IsSynced() bool {
	return atomic.LoadInt32(ws.synced) == 1
}

func (ws *WalletService) Synchronize() (*waddrmgr.BlockStamp, error) {
	return ws.synchronize(true)
}

func (ws *WalletService) SynchronizeWatchless() (bestBlock *waddrmgr.BlockStamp, err error) {
	return ws.synchronize(false)
}

func (ws *WalletService) synchronize(watch bool) (bestBlock *waddrmgr.BlockStamp, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	client := electrum.NewClient(ws.params.ElectrumServer, nil)
	if err = client.Start(ctx); err != nil {
		return
	}

	defer func() {
		if err != nil {
			client.Shutdown()
		}
	}()

	chainClient := chain.NewElectrumClient(ws.params.Network, client)
	if err = chainClient.Start(); err != nil {
		return
	}

	ws.stopService()
	if ws.IsOpened() {
		ws.CloseWallet()
	}

	if err = ws.OpenWallet(); err != nil {
		err = fmt.Errorf("failed to open wallet: %v", err)
		return
	}

	if cc, ok := chainClient.(*chain.ElectrumClient); ok {
		ws.electrumHealth = cc.Health()
	}
	ws.stop = make(chan struct{})
	atomic.StoreInt32(ws.synced, 1)
	if watch {
		ws.watch()
		ws.healthCheck()
		ws.notificationsHandler()
	}

	ws.SynchronizeRPC(chainClient)
	ws.SetChainSynced(true)
	ws.electrumClient = client

	bestBlock, err = ws.CurrentBestBlock()
	return
}

func (ws *WalletService) healthCheck() {
	ws.wg.Add(1)

	go func() {
		defer ws.wg.Done()
		for {
			select {
			case err := <-ws.electrumHealth:
				ws.healthNotif <- err

			case <-ws.stop:
				return
			}
		}
	}()

}

func (ws *WalletService) notificationsHandler() {
	ws.wg.Add(1)

	go func() {
		defer ws.wg.Done()
		for {
			chainClient, ok := ws.Wallet.ChainClient().(*chain.ElectrumClient)
			if !ok {
				return
			}
			select {
			case n := <-chainClient.ClientNotifications():
				ws.notifications <- n

			case <-ws.stop:
				return
			}
		}
	}()

}

func (ws *WalletService) watch() {
	ws.wg.Add(1)

	accountNotif := ws.NtfnServer.AccountNotifications()
	txtNotif := ws.NtfnServer.TransactionNotifications()
	spentNessNotif := ws.NtfnServer.AccountSpentnessNotifications(ws.account.AccountNumber)

	go func() {
		defer ws.wg.Done()

		for {
			select {

			case <-time.After(time.Second * 5):
				ws.accountNotif <- &wallet.AccountNotification{
					AccountNumber: ws.account.AccountNumber,
				}

			case n := <-accountNotif.C:
				ws.accountNotif <- n

			case n := <-txtNotif.C:
				ws.txNotif <- n

			case n := <-spentNessNotif.C:
				ws.spentNessNotif <- n

			case <-ws.stop:
				return

			}
		}
	}()

}

func (ws *WalletService) Watch() (<-chan *wallet.AccountNotification, <-chan *wallet.TransactionNotifications, <-chan *wallet.SpentnessNotifications, chan interface{}, chan error) {
	return ws.accountNotif, ws.txNotif, ws.spentNessNotif, ws.notifications, ws.healthNotif
}

func (ws *WalletService) Create(seedLen uint8, name, passphrase string) (hexData string, words []string, err error) {
	defer func() {
		if err != nil {
			ws.DestroyWallet()
		}
	}()
	var seed []byte
	seed, err = ws.CreateWallet([]byte(passphrase), seedLen, name)
	if err != nil {
		return
	}

	hexData, words, err = ws.backupData(seed)
	return
}

func (ws *WalletService) Balance() float64 {
	if !ws.isOpened {
		return 0
	}

	balance, err := ws.CalculateAccountBalances(ws.account.AccountNumber, 0)
	if err != nil {
		return 0
	}
	return balance.Total.ToFLC()
}

func (ws *WalletService) CurrentWalletBlock() (*waddrmgr.BlockStamp, error) {
	if !ws.isOpened {
		return nil, wallet.ErrNotLoaded
	}

	ret, err := ws.Wallet.Accounts(ws.params.AddressScope)
	if err != nil {
		return nil, err
	}
	return &waddrmgr.BlockStamp{
		Height: ret.CurrentBlockHeight,
		Hash:   *ret.CurrentBlockHash,
	}, nil
}

func (ws *WalletService) CurrentBestBlock() (*waddrmgr.BlockStamp, error) {
	if atomic.LoadInt32(ws.synced) == 0 {
		return nil, electrum.ErrServerShutdown
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	block, height, err := ws.electrumClient.GetBestBlock(ctx)
	if err != nil {
		return nil, err
	}

	return &waddrmgr.BlockStamp{
		Height: height,
		Hash:   *block,
	}, nil
}

func (ws *WalletService) GetLastAddress() (chainutil.Address, error) {
	if !ws.isOpened || ws.account == nil {
		return nil, wallet.ErrNotLoaded
	}

	if ws.account.AccountPubKey == nil {
		return nil, fmt.Errorf("account %d can't derive", ws.account.AccountNumber)
	}

	var nextAddrIndex uint32
	if ws.account.ExternalKeyCount > 0 {
		nextAddrIndex = ws.account.ExternalKeyCount - 1
	}

	branchKey, err := ws.account.AccountPubKey.Derive(waddrmgr.ExternalBranch)
	if err != nil {
		return nil, err
	}

	key, err := branchKey.Derive(nextAddrIndex)
	if err != nil {
		return nil, err
	}

	return key.Address(ws.params.Network)
}

func (ws *WalletService) GetNextAddress() (chainutil.Address, error) {
	return ws.CreateNewAddress()
}

func (ws *WalletService) RelayFee() (float32, error) {
	if !ws.isOpened {
		return 0, wallet.ErrNotLoaded
	}

	if atomic.LoadInt32(ws.synced) == 0 {
		return 0, electrum.ErrServerShutdown
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
	return ws.electrumClient.GetRelayFee(ctx)
}

func (ws *WalletService) EstimateFee(target uint32) (float32, error) {
	if !ws.isOpened {
		return 0, wallet.ErrNotLoaded
	}

	if atomic.LoadInt32(ws.synced) == 0 {
		return 0, electrum.ErrServerShutdown
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
	return ws.electrumClient.GetFee(ctx, target)
}

func (ws *WalletService) FetchTransactions() ([]TransactionServiceResult, error) {

	txsResult, err := ws.ListAllTransactions()
	if err != nil {
		return nil, err
	}

	return ws.aggregateTransactions(txsResult), nil

}

func (ws *WalletService) RestoreByHex(input string, name, passphrase string) (hexData string, words []string, err error) {

	defer func() {
		if err != nil {
			ws.DestroyWallet()
		}
	}()

	var seed []byte
	seed, err = hex.DecodeString(input)
	if err != nil {
		return
	}

	err = ws.RestoreWallet(seed, []byte(passphrase), name)
	if err != nil {
		return
	}

	hexData, words, err = ws.backupData(seed)
	return
}

func (ws *WalletService) RestoreByMnemonic(input []string, name, passphrase string) (hexData string, words []string, err error) {

	defer func() {
		if err != nil {
			ws.DestroyWallet()
		}
	}()

	var seed []byte
	seed, err = bip39.EntropyFromMnemonic(strings.Join(input, " "))
	if err != nil {
		return
	}

	err = ws.RestoreWallet(seed, []byte(passphrase), name)
	if err != nil {
		return
	}

	hexData, words, err = ws.backupData(seed)

	return
}

func (ws *WalletService) Recover(counter chan<- uint32) (*waddrmgr.BlockStamp, error) {

	if !ws.isOpened {
		return nil, wallet.ErrNotLoaded
	}

	bestBlock, err := ws.synchronize(false)
	if err != nil {
		return nil, err
	}

	electrumClient, ok := ws.Wallet.ChainClient().(*chain.ElectrumClient)
	if !ok {
		return nil, fmt.Errorf("recovering not supported") // skip recovering
	}

	quit := make(chan struct{})
	defer close(quit)

	go func() {
		for {
			select {
			case notif := <-electrumClient.Notifications():
				c, ok := notif.(uint32)
				if !ok {
					continue
				}

				select {
				case counter <- c:
				default:
				}

			case <-quit:
				return
			}
		}
	}()

	externalAddrCount, internalAddrCount, err := electrumClient.Recover(ws.account, recoveryWindow)
	if err != nil {
		return nil, err
	}

	if externalAddrCount > 0 {
		_, err = ws.NewAddressRPCLess(ws.account.AccountNumber, ws.account.KeyScope, externalAddrCount)
		if err != nil {
			return nil, err
		}
	}

	if internalAddrCount > 0 {
		_, err = ws.NewChangeAddressRPCLess(ws.account.AccountNumber, ws.account.KeyScope, internalAddrCount)
		if err != nil {
			return nil, err
		}
	}

	return bestBlock, nil
}

func (ws *WalletService) backupData(seed []byte) (hexData string, words []string, err error) {

	// HEX format
	hexData = hex.EncodeToString(seed)

	mnemonic, err := bip39.NewMnemonic(seed)
	if err != nil {
		err = fmt.Errorf("unable to generate mnemonic: seed:%d seedLen:%d %v", len(seed), len(seed), err)
		return
	}
	words = strings.Fields(mnemonic)

	return
}

func (ws *WalletService) stopService() {
	if atomic.LoadInt32(ws.synced) == 0 {
		return
	}

	ws.Stop()
	atomic.StoreInt32(ws.synced, 0)
	close(ws.stop)
	ws.wg.Wait()
}
