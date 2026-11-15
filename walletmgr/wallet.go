package walletmgr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/flokiorg/go-flokicoin/chaincfg"
	"github.com/flokiorg/go-flokicoin/chainutil"
	"github.com/flokiorg/go-flokicoin/chainutil/hdkeychain"
	"github.com/flokiorg/go-flokicoin/txscript"
	"github.com/flokiorg/go-flokicoin/wire"
	"github.com/flokiorg/walletd/waddrmgr"
	"github.com/flokiorg/walletd/wallet"
)

type WalletParams struct {
	Network        *chaincfg.Params
	Path           string
	Timeout        time.Duration
	PublicPassword string
	AddressScope   waddrmgr.KeyScope
	ElectrumServer string
	AccountID      uint32
}

type WalletAccess struct {
	*wallet.Wallet
	loader   *wallet.Loader
	params   *WalletParams
	isOpened bool
	account  *waddrmgr.AccountProperties
}

var (
	ErrWalletNotfound        = errors.New("wallet not found")
	recoveryWindow    uint32 = 250
)

func NewWalletAccess(params *WalletParams) *WalletAccess {
	return &WalletAccess{
		params: params,
	}
}

func (wa *WalletAccess) Loader() *wallet.Loader {
	return wa.loader
}

func (wa *WalletAccess) CreateWallet(privPass []byte, seedLen uint8, accountName string) ([]byte, error) {

	seed, err := hdkeychain.GenerateSeed(seedLen)
	if err != nil {
		return nil, fmt.Errorf("unable to generate seed: %v", err)
	}

	return seed, wa.createSimpleWallet(seed, privPass, accountName)
}

func (wa *WalletAccess) RestoreWallet(seed, privPass []byte, accountName string) error {
	return wa.createSimpleWallet(seed, privPass, accountName)
}

func (wa *WalletAccess) createSimpleWallet(seed, privPass []byte, accountName string) error {

	wa.loader = wallet.NewLoader(wa.params.Network, wa.params.Path, true, wa.params.Timeout, recoveryWindow)

	w, err := wa.loader.CreateNewWallet([]byte(wa.params.PublicPassword), privPass, seed, time.Now()) // wa.params.Network.GenesisBlock.Header.Timestamp
	if err != nil {
		return fmt.Errorf("unable to create wallet: %w", err)
	}

	if err := w.Unlock(privPass, nil); err != nil {
		return fmt.Errorf("failed to unlock wallet: %w", err)
	}
	defer w.Lock()

	accountID, err := w.NextAccount(wa.params.AddressScope, accountName)
	if err != nil {
		return fmt.Errorf("unable to create account: %w", err)
	}

	account, err := w.AccountProperties(wa.params.AddressScope, accountID)
	if err != nil {
		return err
	}

	if _, err = w.NewAddressRPCLess(account.AccountNumber, wa.params.AddressScope, 1); err != nil {
		return fmt.Errorf("unable to create new address: %v", err)
	}

	wa.Wallet = w
	wa.account = account
	wa.isOpened = true

	return nil
}

func (wa *WalletAccess) WalletExists() (bool, error) {

	loader := wallet.NewLoader(wa.params.Network, wa.params.Path, true, wa.params.Timeout, recoveryWindow)

	// Check if the wallet already exists
	walletExists, err := loader.WalletExists()
	if err != nil {
		return false, fmt.Errorf("unable to find wallet db: %v", err)
	}

	return walletExists, nil
}

func (wa *WalletAccess) DestroyWallet() {
	os.Remove(filepath.Join(wa.params.Path, wallet.WalletDBName))
}

func (wa *WalletAccess) OpenWallet() error {
	if exists, _ := wa.WalletExists(); !exists {
		return ErrWalletNotfound
	}

	wa.loader = wallet.NewLoader(wa.params.Network, wa.params.Path, true, wa.params.Timeout, recoveryWindow)
	w, err := wa.loader.OpenExistingWallet([]byte(wa.params.PublicPassword), false)
	if err != nil {
		return err
	}

	account, err := w.AccountProperties(wa.params.AddressScope, wa.params.AccountID)
	if err != nil {
		return err
	}

	wa.account = account
	wa.Wallet = w
	wa.isOpened = true
	return nil
}

func (wa *WalletAccess) CloseWallet() error {

	if db := wa.Database(); db != nil {
		db.Close()
	}

	wa.isOpened = false
	return nil
}

func (wa *WalletAccess) OpenIfExists() (bool, error) {
	exists, _ := wa.WalletExists()
	if !exists {
		return false, nil
	}

	return true, wa.OpenWallet()
}

func (wa *WalletAccess) CreateNewAddress() (chainutil.Address, error) {
	if !wa.isOpened || wa.account == nil {
		return nil, wallet.ErrNotLoaded
	}
	addr, err := wa.NewAddressRPCLess(wa.account.AccountNumber, wa.params.AddressScope, 1)
	if err != nil {
		return nil, err
	}

	return addr, nil
}

func (wa *WalletAccess) SimpleTransferFee(address chainutil.Address, amount chainutil.Amount, feePerByte chainutil.Amount) (*chainutil.Amount, error) {

	script, err := txscript.PayToAddrScript(address)
	if err != nil {
		return nil, err
	}

	output := &wire.TxOut{
		Value:    int64(amount.ToUnit(chainutil.AmountLoki)),
		PkScript: script,
	}
	outputs := []*wire.TxOut{output}

	minconf := int32(1)

	return wa.SimpleTxFee(
		&wa.params.AddressScope,
		wa.account.AccountNumber,
		outputs,
		minconf,
		feePerByte*1000,
		nil,
	)

}

func (wa *WalletAccess) SimpleTransfer(privPass []byte, address chainutil.Address, amount chainutil.Amount, feePerByte chainutil.Amount) (*wire.MsgTx, error) {
	if !wa.isOpened {
		return nil, wallet.ErrNotLoaded
	}

	if err := wa.Unlock(privPass, nil); err != nil {
		return nil, err
	}
	defer wa.Lock()

	script, err := txscript.PayToAddrScript(address)
	if err != nil {
		return nil, err
	}

	// Create a TxOut for the destination address
	output := &wire.TxOut{
		Value:    int64(amount.ToUnit(chainutil.AmountLoki)),
		PkScript: script,
	}
	outputs := []*wire.TxOut{output}

	minconf := int32(1) // Minimum confirmations required

	// Create the transaction
	tx, err := wa.SendOutputs(
		outputs,                      // Outputs
		&wa.params.AddressScope,      // Key scope
		wa.account.AccountNumber,     // Account ID
		minconf,                      // Minimum confirmations
		feePerByte*1000,              // Fee rate
		&wallet.RandomCoinSelector{}, // Coin selection strategy
		time.Now().GoString(),
	)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func (wa *WalletAccess) BulkSimpleTransfer(privPass []byte, addresses []chainutil.Address, amounts []chainutil.Amount, feePerByte chainutil.Amount) (*wire.MsgTx, error) {
	if !wa.isOpened {
		return nil, wallet.ErrNotLoaded
	}

	if err := wa.Unlock(privPass, nil); err != nil {
		return nil, err
	}
	defer wa.Lock()

	outputs := []*wire.TxOut{}

	for i, address := range addresses {
		script, err := txscript.PayToAddrScript(address)
		if err != nil {
			return nil, err
		}

		// Create a TxOut for the destination address
		output := &wire.TxOut{
			Value:    int64(amounts[i].ToUnit(chainutil.AmountLoki)),
			PkScript: script,
		}

		outputs = append(outputs, output)
	}

	minconf := int32(1) // Minimum confirmations required

	// Create the transaction
	tx, err := wa.SendOutputs(
		outputs,                      // Outputs
		&wa.params.AddressScope,      // Key scope
		wa.account.AccountNumber,     // Account ID
		minconf,                      // Minimum confirmations
		feePerByte*1000,              // Fee rate
		&wallet.RandomCoinSelector{}, // Coin selection strategy &wallet.RandomCoinSelector{}
		time.Now().GoString(),
	)
	if err != nil {
		return nil, err
	}

	return tx, nil
}
