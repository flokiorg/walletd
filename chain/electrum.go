// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flokiorg/go-flokicoin/chaincfg"
	"github.com/flokiorg/go-flokicoin/chaincfg/chainhash"
	"github.com/flokiorg/go-flokicoin/chainjson"
	"github.com/flokiorg/go-flokicoin/chainutil"
	"github.com/flokiorg/go-flokicoin/chainutil/hdkeychain"
	"github.com/flokiorg/go-flokicoin/txscript"
	"github.com/flokiorg/go-flokicoin/wire"
	"github.com/flokiorg/walletd/chain/electrum"
	"github.com/flokiorg/walletd/waddrmgr"
	"github.com/flokiorg/walletd/wtxmgr"
)

// Compile-time check that ElectrumClient implements chain.Interface.
var _ Interface = (*ElectrumClient)(nil)

// ElectrumClient implements chain.Interface, but uses an ElectrumClient internally.
//
// If you need advanced features, such as subscription-based notifications,
// you can store additional data and channels for pushing new blocks/txs, etc.
type ElectrumClient struct {
	electrum *electrum.Client

	network *chaincfg.Params
	started int32

	quit        chan struct{}
	wg          sync.WaitGroup
	birthday    time.Time
	backendName string

	headerByHash   map[string]*wire.BlockHeader
	blocksByHash   map[string]uint32
	blocksByHeight map[uint32]string
	mu             sync.Mutex

	notificationQueue *ConcurrentQueue

	headerSubscription <-chan *electrum.SubscribeHeadersResult

	bestBlockMu sync.Mutex
	bestBlock   waddrmgr.BlockStamp

	// rescanUpdate is a channel will be sent items that we should match
	// transactions against while processing a chain rescan to determine if
	// they are relevant to the client.
	rescanUpdate chan interface{}

	// watchedAddresses, watchedOutPoints, and watchedTxs are the set of
	// items we should match transactions against while processing a chain
	// rescan to determine if they are relevant to the client.
	watchMtx         sync.RWMutex
	watchedAddresses map[string]struct{}
	watchedOutPoints map[wire.OutPoint]struct{}
	watchedTxs       map[chainhash.Hash]struct{}

	health        chan error
	notifications chan interface{}
}

// NewElectrumClient creates a new ElectrumClient instance for electrum.
func NewElectrumClient(network *chaincfg.Params, client *electrum.Client) Interface {
	return &ElectrumClient{
		network:           network,
		electrum:          client,
		quit:              make(chan struct{}),
		backendName:       "electrum",
		headerByHash:      make(map[string]*wire.BlockHeader),
		blocksByHash:      make(map[string]uint32),
		blocksByHeight:    make(map[uint32]string),
		notificationQueue: NewConcurrentQueue(20),

		rescanUpdate:     make(chan interface{}),
		watchedAddresses: make(map[string]struct{}),
		watchedOutPoints: make(map[wire.OutPoint]struct{}),
		watchedTxs:       make(map[chainhash.Hash]struct{}),

		health:        make(chan error),
		notifications: make(chan interface{}),
	}
}

// Start implements the chain.Interface Start method.
func (c *ElectrumClient) Start() error {
	if !atomic.CompareAndSwapInt32(&c.started, 0, 1) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), electrum.DefaultTimeout)
	defer cancel()
	if err := c.electrum.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping: %v", err)
	}

	headerSubscription, err := c.electrum.SubscribeHeaders(context.Background())
	if err != nil {
		return fmt.Errorf("failed to subscribe: %v", err)
	}
	c.headerSubscription = headerSubscription

	// Start the notification queue and immediately dispatch a
	// ClientConnected notification to the caller. This is needed as some of
	// the callers will require this notification before proceeding.
	c.notificationQueue.Start()
	c.notificationQueue.ChanIn() <- ClientConnected{}

	// Retrieve the best block of the chain.
	bestHash, bestHeight, err := c.GetBestBlock()
	if err != nil {
		return fmt.Errorf("unable to retrieve best block: %w", err)
	}
	bestHeader, err := c.GetBlockHeader(bestHash)
	if err != nil {
		return fmt.Errorf("unable to retrieve header for best block: %w", err)
	}

	c.bestBlockMu.Lock()
	c.bestBlock = waddrmgr.BlockStamp{
		Hash:      *bestHash,
		Height:    bestHeight,
		Timestamp: bestHeader.Timestamp,
	}
	c.bestBlockMu.Unlock()

	// Start any required goroutines for handling incoming notifications,
	// block subscription, etc.
	c.wg.Add(3)
	go c.headerSubscriptionHandler()
	go c.pingPongHandler()
	go c.rescanHandler()

	return nil
}

// Stop implements the chain.Interface Stop method.
func (c *ElectrumClient) Stop() {
	if !atomic.CompareAndSwapInt32(&c.started, 1, 0) {
		return
	}

	close(c.quit)

	c.electrum.Shutdown()
	c.notificationQueue.Stop()
}

// WaitForShutdown implements the chain.Interface WaitForShutdown method.
func (c *ElectrumClient) WaitForShutdown() {
	c.wg.Wait()
}

// GetBestBlock implements the chain.Interface GetBestBlock method.
func (c *ElectrumClient) GetBestBlock() (*chainhash.Hash, int32, error) {

	if c.electrum.IsShutdown() {
		return nil, 0, errors.New("electrum not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), electrum.DefaultTimeout)
	defer cancel()
	blockhash, height, err := c.electrum.GetBestBlock(ctx)
	if err != nil {
		return nil, -1, err
	}

	c.mu.Lock()
	c.blocksByHeight[uint32(height)] = blockhash.String()
	c.blocksByHash[blockhash.String()] = uint32(height)
	c.mu.Unlock()

	return blockhash, height, nil
}

// GetBlock implements the chain.Interface GetBlock method.
func (c *ElectrumClient) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {

	if c.electrum.IsShutdown() {
		return nil, errors.New("electrum not connected")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	header, ok := c.headerByHash[hash.String()]
	if !ok {
		return nil, fmt.Errorf("unknow blockhash=%s", hash.String())
	}

	return wire.NewMsgBlock(header), nil
}

func (c *ElectrumClient) GetBlockHash(height int64) (*chainhash.Hash, error) {

	// Check if the Electrum client is connected
	if c.electrum.IsShutdown() {
		return nil, errors.New("electrum not connected")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	blockhash, exists := c.blocksByHeight[uint32(height)]

	if exists {
		// Return the cached block hash
		hash, err := chainhash.NewHashFromStr(blockhash)
		if err != nil {
			return nil, fmt.Errorf("invalid cached block hash: %v", err)
		}
		return hash, nil
	}

	// Fetch the block hash from the Electrum server
	ctx, cancel := context.WithTimeout(context.Background(), electrum.DefaultTimeout)
	defer cancel()
	hash, header, err := c.electrum.GetBlockHash(ctx, uint32(height))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block for height %d: %v", height, err)
	}

	// Cache the block hash for future requests
	c.blocksByHeight[uint32(height)] = hash.String()
	c.blocksByHash[hash.String()] = uint32(height)
	c.headerByHash[hash.String()] = header

	return hash, nil
}

// GetBlockHeader implements the chain.Interface GetBlockHeader method.
func (c *ElectrumClient) GetBlockHeader(hash *chainhash.Hash) (*wire.BlockHeader, error) {

	if c.electrum.IsShutdown() {
		return nil, errors.New("electrum not connected")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if header, ok := c.headerByHash[hash.String()]; ok {
		return header, nil
	}

	height, ok := c.blocksByHash[hash.String()]
	if !ok {
		return nil, fmt.Errorf("unknown height=%d", height)
	}

	// Fetch the block hash from the Electrum server
	ctx, cancel := context.WithTimeout(context.Background(), electrum.DefaultTimeout)
	defer cancel()
	hash, header, err := c.electrum.GetBlockHash(ctx, uint32(height))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch block for height %d: %v", height, err)
	}

	// Cache the block hash for future requests
	c.blocksByHeight[uint32(height)] = hash.String()
	c.blocksByHash[hash.String()] = uint32(height)
	c.headerByHash[hash.String()] = header

	return header, nil
}

// Typically, you compare your best block height against the known chain tip height
// or other heuristics.
func (c *ElectrumClient) IsCurrent() bool {
	return true
}

// BlockStamp implements the chain.Interface BlockStamp method.
// The returned waddrmgr.BlockStamp typically includes hash, height, and timestamp.
func (c *ElectrumClient) BlockStamp() (*waddrmgr.BlockStamp, error) {
	c.bestBlockMu.Lock()
	bestBlock := c.bestBlock
	c.bestBlockMu.Unlock()

	return &bestBlock, nil
}

// SendRawTransaction implements the chain.Interface SendRawTransaction method.
func (c *ElectrumClient) SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {

	if c.electrum.IsShutdown() {
		return nil, errors.New("electrum not connected")
	}

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("failed to serialize tx: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), electrum.DefaultTimeout)
	defer cancel()
	strBlockhash, err := c.electrum.BroadcastTransaction(ctx, hex.EncodeToString(buf.Bytes()))
	if err != nil {
		return nil, err
	}

	hash, err := chainhash.NewHashFromStr(strBlockhash)
	if err != nil {
		return nil, fmt.Errorf("failed to parse block hash: %s", strBlockhash)
	}

	return hash, nil
}

// wallet's birthday, accounts are created after this time. imported addresses are not concerned.
func (s *ElectrumClient) SetStartTime(startTime time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.birthday = startTime
}

// This is typically used for scanning scripts or addresses in blocks.
// Implementation may vary widely depending on your usage and how you store filters.
func (c *ElectrumClient) FilterBlocks(req *FilterBlocksRequest) (*FilterBlocksResponse, error) {
	// This is a placeholder. You would implement actual filter logic
	// using electrum or your own indexing approach, returning only matching blocks.
	return &FilterBlocksResponse{}, nil
}

// NotifyReceived implements the chain.Interface NotifyReceived method.
// Typically used for receiving updates when addresses receive new transactions.
func (c *ElectrumClient) NotifyReceived(addrs []chainutil.Address) error {
	// If needed, subscribe to address-based notifications from electrum.
	return nil
}

// NotifyBlocks implements the chain.Interface NotifyBlocks method.
// Typically you’d subscribe to new block notifications from electrum and relay them via c.notificationsChan.
func (c *ElectrumClient) NotifyBlocks() error {
	return nil
}

// Notifications implements the chain.Interface Notifications method.
func (c *ElectrumClient) Notifications() <-chan interface{} {
	return c.notificationQueue.ChanOut()
}

func (c *ElectrumClient) Health() <-chan error {
	return c.health
}

// BackEnd implements the chain.Interface BackEnd method.
// Return a string that identifies this chain driver, e.g. "electrum".
func (c *ElectrumClient) BackEnd() string {
	return c.backendName
}

// onBlockConnected is a callback that's executed whenever a new block has been
// detected. This will queue a BlockConnected notification to the caller.
func (c *ElectrumClient) onBlockConnected(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	select {
	case c.notificationQueue.ChanIn() <- BlockConnected{
		Block: wtxmgr.Block{
			Hash:   *hash,
			Height: height,
		},
		Time: timestamp,
	}:
	case <-c.quit:
	}
}

func (c *ElectrumClient) onRescanProgress(hash *chainhash.Hash, height int32, blkTime time.Time) {
	select {
	case c.notificationQueue.ChanIn() <- &RescanProgress{*hash, height, blkTime}:
	case <-c.quit:
	}
}

// onRescanFinished is a callback that's executed whenever a rescan has
// finished. This will queue a RescanFinished notification to the caller with
// the details of the last block in the range of the rescan.
func (c *ElectrumClient) onRescanFinished(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	select {
	case c.notificationQueue.ChanIn() <- &RescanFinished{
		Hash:   hash,
		Height: height,
		Time:   timestamp,
	}:
	case <-c.quit:
	}

	select {
	case c.notifications <- &RescanFinished{
		Hash:   hash,
		Height: height,
		Time:   timestamp,
	}:
	case <-time.After(time.Second * 5):
	case <-c.quit:
	}
}

func (c *ElectrumClient) ClientNotifications() chan interface{} {
	return c.notifications
}

// onFilteredBlockConnected is an alternative callback that's executed whenever
// a new block has been detected. It serves the same purpose as
// onBlockConnected, but it also includes a list of the relevant transactions
// found within the block being connected. This will queue a
// FilteredBlockConnected notification to the caller.
func (c *ElectrumClient) onFilteredBlockConnected(height int32,
	header *wire.BlockHeader, relevantTxs []*wtxmgr.TxRecord) {

	select {
	case c.notificationQueue.ChanIn() <- FilteredBlockConnected{
		Block: &wtxmgr.BlockMeta{
			Block: wtxmgr.Block{
				Hash:   header.BlockHash(),
				Height: height,
			},
			Time: header.Timestamp,
		},
		RelevantTxs: relevantTxs,
	}:
	case <-c.quit:
	}
}

// onRelevantTx is a callback that's executed whenever a transaction is relevant
// to the caller. This means that the transaction matched a specific item in the
// client's different filters. This will queue a RelevantTx notification to the
// caller.
func (c *ElectrumClient) onRelevantTx(tx *wtxmgr.TxRecord, blockDetails *chainjson.BlockDetails) {

	block, err := parseBlock(blockDetails)
	if err != nil {
		log.Errorf("Unable to send onRelevantTx notification, failed "+
			"parse block: %v", err)
		return
	}

	select {
	case c.notificationQueue.ChanIn() <- RelevantTx{
		TxRecord: tx,
		Block:    block,
	}:
	case <-c.quit:
	}
}

// TestMempoolAccept implements the chain.Interface TestMempoolAccept method.
func (c *ElectrumClient) TestMempoolAccept(txs []*wire.MsgTx, maxFeeRate float64) ([]*chainjson.TestMempoolAcceptResult, error) {
	if c.electrum.IsShutdown() {
		return nil, errors.New("electrum not connected")
	}
	return nil, fmt.Errorf("unsupported method TestMempoolAccept")
}

// MapRPCErr implements the chain.Interface MapRPCErr method.
func (c *ElectrumClient) MapRPCErr(err error) error {
	return &chainjson.RPCError{
		Code:    chainjson.ErrRPCInternal.Code,
		Message: err.Error(),
	}
}

// headerSubscriptionHandler is a background goroutine that would wait for blocks
func (c *ElectrumClient) headerSubscriptionHandler() {
	defer c.wg.Done()

loop:
	for {
		select {
		case blockEvent := <-c.headerSubscription:
			blockHex, err := hex.DecodeString(blockEvent.Hex)
			if err != nil {
				continue loop
			}
			block := &wire.MsgBlock{}
			block.Deserialize(bytes.NewReader(blockHex))
			blockhash := block.BlockHash()

			c.mu.Lock()
			c.headerByHash[blockhash.String()] = &block.Header
			c.blocksByHeight[uint32(blockEvent.Height)] = blockhash.String()
			c.blocksByHash[blockhash.String()] = uint32(blockEvent.Height)
			c.mu.Unlock()

			c.bestBlockMu.Lock()
			bestBlock := c.bestBlock
			c.bestBlockMu.Unlock()

			needReorg := block.Header.PrevBlock != bestBlock.Hash

			c.onBlockConnected(&blockhash, blockEvent.Height, block.Header.Timestamp)
			c.onRescanProgress(&blockhash, blockEvent.Height, block.Header.Timestamp)
			if err := c.rescan(bestBlock.Hash, bestBlock.Height, needReorg); err != nil {
				continue loop
			}

			// With the block successfully processed, we'll
			// make it our new best block.
			bestBlock.Hash = block.BlockHash()
			bestBlock.Height = blockEvent.Height
			bestBlock.Timestamp = block.Header.Timestamp

			c.bestBlockMu.Lock()
			c.bestBlock = bestBlock
			c.bestBlockMu.Unlock()

			c.onBlockConnected(&blockhash, blockEvent.Height, block.Header.Timestamp)
			c.onRescanFinished(&blockhash, blockEvent.Height, block.Header.Timestamp)
		case <-c.quit:
			return
		}
	}
}

func (c *ElectrumClient) pingPongHandler() {
	defer c.wg.Done()

	for {
		select {
		case <-c.quit:
			return
		case <-time.After(2 * time.Second): // 10secs
			ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
			err := c.electrum.Ping(ctx)
			if err != nil {
				c.health <- err
			} else {
				c.health <- electrum.NerrHealthPong
			}
		}
	}
}

// rescanHandler handles the logic needed for the caller to trigger a chain
// rescan.
//
// NOTE: This must be called as a goroutine.
func (c *ElectrumClient) rescanHandler() {
	defer c.wg.Done()

	for {
		select {
		case update := <-c.rescanUpdate:
			switch update := update.(type) {

			// We're clearing the filters.
			case struct{}:
				c.watchMtx.Lock()
				c.watchedOutPoints = make(map[wire.OutPoint]struct{})
				c.watchedAddresses = make(map[string]struct{})
				c.watchedTxs = make(map[chainhash.Hash]struct{})
				c.watchMtx.Unlock()

			// We're adding the addresses to our filter.
			case []chainutil.Address:
				c.watchMtx.Lock()
				for _, addr := range update {
					c.watchedAddresses[addr.String()] = struct{}{}
				}
				c.watchMtx.Unlock()

			// We're adding the outpoints to our filter.
			case []wire.OutPoint:
				c.watchMtx.Lock()
				for _, op := range update {
					c.watchedOutPoints[op] = struct{}{}
				}
				c.watchMtx.Unlock()
			case []*wire.OutPoint:
				c.watchMtx.Lock()
				for _, op := range update {
					c.watchedOutPoints[*op] = struct{}{}
				}
				c.watchMtx.Unlock()

			// We're adding the outpoints that map to the scripts
			// that we should scan for to our filter.
			case map[wire.OutPoint]chainutil.Address:
				c.watchMtx.Lock()
				for op := range update {
					c.watchedOutPoints[op] = struct{}{}
				}
				c.watchMtx.Unlock()

			// We're adding the transactions to our filter.
			case []chainhash.Hash:
				c.watchMtx.Lock()
				for _, txid := range update {
					c.watchedTxs[txid] = struct{}{}
				}
				c.watchMtx.Unlock()

			case []*chainhash.Hash:
				c.watchMtx.Lock()
				for _, txid := range update {
					c.watchedTxs[*txid] = struct{}{}
				}
				c.watchMtx.Unlock()

			// We're starting a rescan from the hash.
			case chainhash.Hash:
				if err := c.rescan(update, 0, true); err != nil {
					log.Errorf("Unable to complete chain rescan: %v", err)
				}
			default:
				log.Warnf("Received unexpected filter type %T", update)
			}
		case <-c.quit:
			return
		}
	}
}

// Rescan rescans from the block with the given hash until the current block,
// after adding the passed addresses and outpoints to the client's watch list.
func (c *ElectrumClient) Rescan(blockHash *chainhash.Hash, addresses []chainutil.Address, outPoints map[wire.OutPoint]chainutil.Address) error {

	// A block hash is required to use as the starting point of the rescan.
	if blockHash == nil {
		return errors.New("rescan requires a starting block hash")
	}

	// We'll then update our filters with the given outpoints and addresses.
	select {
	case c.rescanUpdate <- addresses:
	case <-c.quit:
		return ErrFlokicoindClientShuttingDown
	}

	select {
	case c.rescanUpdate <- outPoints:
	case <-c.quit:
		return ErrFlokicoindClientShuttingDown
	}

	// Once the filters have been updated, we can begin the rescan.
	select {
	case c.rescanUpdate <- *blockHash:
	case <-c.quit:
		return ErrFlokicoindClientShuttingDown
	}

	return nil
}

// rescan performs a rescan of the chain
func (c *ElectrumClient) rescan(start chainhash.Hash, startHeight int32, needReorg bool) error {

	for strAddr, _ := range c.watchedAddresses {
		addr, err := chainutil.DecodeAddress(strAddr, c.network)
		if err != nil {
			continue
		}

		records, err := c.fetchHistory(context.Background(), addr, startHeight, needReorg)
		if err != nil {
			return err
		}

		for _, r := range records {
			c.onRelevantTx(r.record, r.block)
		}
	}

	return nil

}

type historyRecord struct {
	record *wtxmgr.TxRecord
	block  *chainjson.BlockDetails
}

func (c *ElectrumClient) fetchHistory(parent context.Context, addr chainutil.Address, startHeight int32, needReorg bool) ([]*historyRecord, error) {

	scriptHashBytes, err := txscript.AddrToScripthash(addr)
	if err != nil {
		return nil, fmt.Errorf("address invalid: %v", err)
	}

	ctx, cancel := context.WithTimeout(parent, electrum.DefaultTimeout)
	defer cancel()
	history, err := c.electrum.GetHistory(ctx, hex.EncodeToString(scriptHashBytes))
	if err != nil {
		return nil, fmt.Errorf("electrum service failed loading history for scripthash=%x", scriptHashBytes)
	}

	records := make([]*historyRecord, 0)

	for _, txHistory := range history {

		if !needReorg && txHistory.Height <= startHeight {
			continue /// skip processing old blocks
		}

		blockhash, err := c.GetBlockHash(int64(txHistory.Height))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch block height=%d: %v", txHistory.Height, err)
		}

		ctx, cancel := context.WithTimeout(parent, electrum.DefaultTimeout)
		txRaw, err := c.electrum.GetRawTransaction(ctx, txHistory.Hash)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tx=%s", txHistory.Hash)
		}
		txHex, err := hex.DecodeString(txRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to read tx raw: %v", err)
		}

		msgTx := &wire.MsgTx{}
		if err := msgTx.Deserialize(bytes.NewReader(txHex)); err != nil {
			return nil, fmt.Errorf("failed to decode tx: %v", err)
		}

		records = append(records, &historyRecord{
			record: &wtxmgr.TxRecord{
				MsgTx:        *msgTx,
				Hash:         msgTx.TxHash(),
				Received:     time.Now(),
				SerializedTx: txHex,
			},
			block: &chainjson.BlockDetails{
				Height: txHistory.Height,
				Hash:   blockhash.String(),
			},
		})
	}

	return records, nil
}

// rescan performs a wallet recovering
func (c *ElectrumClient) Recover(account *waddrmgr.AccountProperties, recoveryWindow uint32) (uint32, uint32, error) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recoverCounter uint32

	externalBranch, err := account.AccountPubKey.Derive(waddrmgr.ExternalBranch)
	if err != nil {
		return 0, 0, err
	}
	externalAddrCount, externalRecords, err := c.recover(ctx, externalBranch, recoveryWindow, recoverCounter)
	if err != nil {
		return 0, 0, err
	}

	recoverCounter += externalAddrCount

	internalBranch, err := account.AccountPubKey.Derive(waddrmgr.InternalBranch)
	if err != nil {
		return 0, 0, err
	}
	internalAddrCount, internalRecords, err := c.recover(ctx, internalBranch, recoveryWindow, recoverCounter)
	if err != nil {
		return 0, 0, err
	}

	records := append(externalRecords, internalRecords...)
	for _, r := range records {
		c.onRelevantTx(
			r.record,
			r.block,
		)
	}

	return externalAddrCount, internalAddrCount, nil

}

func (c *ElectrumClient) recover(ctx context.Context, branch *hdkeychain.ExtendedKey, recoveryWindow uint32, recoverCounter uint32) (uint32, []*historyRecord, error) {

	var gapCount uint32 = 0
	var index uint32 = 0
	var usedAddrIndex uint32 = 0
	releventRecords := make([]*historyRecord, 0)

	for {
		key, err := branch.Derive(index)
		if err != nil {
			return 0, nil, err
		}

		address, err := key.Address(c.network)
		if err != nil {
			return 0, nil, err
		}

		records, err := c.fetchHistory(ctx, address, 0, true)
		if err != nil {
			return 0, nil, err
		}

		if len(records) > 0 {
			recoverCounter++
			c.notificationQueue.ChanIn() <- recoverCounter
			releventRecords = append(releventRecords, records...)
			gapCount = 0
			usedAddrIndex = index + 1
		} else {
			gapCount++
		}

		if gapCount >= recoveryWindow {
			break
		}

		index++ // next address
	}

	return usedAddrIndex, releventRecords, nil
}
