package chain

import (
	"container/list"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/flokiorg/go-flokicoin/chaincfg/chainhash"
	"github.com/flokiorg/go-flokicoin/chainjson"
	"github.com/flokiorg/go-flokicoin/chainutil"
	"github.com/flokiorg/go-flokicoin/txscript"
	"github.com/flokiorg/go-flokicoin/wire"
	"github.com/flokiorg/walletd/waddrmgr"
	"github.com/flokiorg/walletd/wtxmgr"
)

var (
	// ErrFlokicoindClientShuttingDown is an error returned when we attempt
	// to receive a notification for a specific item and the flokicoind client
	// is in the middle of shutting down.
	ErrFlokicoindClientShuttingDown = errors.New("client is shutting down")
)

// FlokicoindClient represents a persistent client connection to a flokicoind server
// for information regarding the current best block chain.
type FlokicoindClient struct {
	// notifyBlocks signals whether the client is sending block
	// notifications to the caller. This must be used atomically.
	notifyBlocks uint32

	started int32 // To be used atomically.
	stopped int32 // To be used atomically.

	// birthday is the earliest time for which we should begin scanning the
	// chain.
	birthday time.Time

	// id is the unique ID of this client assigned by the backing flokicoind
	// connection.
	id uint64

	// chainConn is the backing client to our rescan client that contains
	// the RPC and ZMQ connections to a flokicoind node.
	chainConn *FlokicoindConn

	// bestBlock keeps track of the tip of the current best chain.
	bestBlockMtx sync.RWMutex
	bestBlock    waddrmgr.BlockStamp

	// watchedAddresses, watchedOutPoints, and watchedTxs are the set of
	// items we should match transactions against while processing a chain
	// rescan to determine if they are relevant to the client.
	watchMtx         sync.RWMutex
	watchedAddresses map[string]struct{}
	watchedOutPoints map[wire.OutPoint]struct{}
	watchedTxs       map[chainhash.Hash]struct{}

	// mempool keeps track of all relevant transactions that have yet to be
	// confirmed. This is used to shortcut the filtering process of a
	// transaction when a new confirmed transaction notification is
	// received.
	//
	// NOTE: This requires the watchMtx to be held.
	mempool map[chainhash.Hash]struct{}

	// expiredMempool keeps track of a set of confirmed transactions along
	// with the height at which they were included in a block. These
	// transactions will then be removed from the mempool after a period of
	// 288 blocks. This is done to ensure the transactions are safe from a
	// reorg in the chain.
	//
	// NOTE: This requires the watchMtx to be held.
	expiredMempool map[int32]map[chainhash.Hash]struct{}

	// notificationQueue is a concurrent unbounded queue that handles
	// dispatching notifications to the subscriber of this client.
	//
	// TODO: Rather than leaving this as an unbounded queue for all types of
	// notifications, try dropping ones where a later enqueued notification
	// can fully invalidate one waiting to be processed. For example,
	// BlockConnected notifications for greater block heights can remove the
	// need to process earlier notifications still waiting to be processed.
	notificationQueue *ConcurrentQueue

	// txNtfns is a channel through which transaction events will be
	// retrieved from the backing flokicoind connection, either via ZMQ or
	// polling RPC.
	txNtfns chan *wire.MsgTx

	// blockNtfns is a channel through block events will be retrieved from
	// the backing flokicoind connection, either via ZMQ or polling RPC.
	blockNtfns chan *wire.MsgBlock

	quit chan struct{}
	wg   sync.WaitGroup
}

// A compile-time check to ensure that FlokicoindClient satisfies the
// chain.Interface interface.
var _ Interface = (*FlokicoindClient)(nil)

// BackEnd returns the name of the driver.
func (c *FlokicoindClient) BackEnd() string {
	return "flokicoind"
}

// GetBestBlock returns the highest block known to flokicoind.
func (c *FlokicoindClient) GetBestBlock() (*chainhash.Hash, int32, error) {
	bcinfo, err := c.chainConn.client.GetBlockChainInfo()
	if err != nil {
		return nil, 0, err
	}

	hash, err := chainhash.NewHashFromStr(bcinfo.BestBlockHash)
	if err != nil {
		return nil, 0, err
	}

	return hash, bcinfo.Blocks, nil
}

// GetBlockHeight returns the height for the hash, if known, or returns an
// error.
func (c *FlokicoindClient) GetBlockHeight(hash *chainhash.Hash) (int32, error) {
	header, err := c.chainConn.client.GetBlockHeaderVerbose(hash)
	if err != nil {
		return 0, err
	}

	return header.Height, nil
}

// GetBlock returns a block from the hash.
func (c *FlokicoindClient) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	return c.chainConn.GetBlock(hash)
}

// GetBlockVerbose returns a verbose block from the hash.
func (c *FlokicoindClient) GetBlockVerbose(
	hash *chainhash.Hash) (*chainjson.GetBlockVerboseResult, error) {

	return c.chainConn.client.GetBlockVerbose(hash)
}

// GetBlockHash returns a block hash from the height.
func (c *FlokicoindClient) GetBlockHash(height int64) (*chainhash.Hash, error) {
	return c.chainConn.client.GetBlockHash(height)
}

// GetBlockHeader returns a block header from the hash.
func (c *FlokicoindClient) GetBlockHeader(
	hash *chainhash.Hash) (*wire.BlockHeader, error) {

	return c.chainConn.client.GetBlockHeader(hash)
}

// GetBlockHeaderVerbose returns a block header from the hash.
func (c *FlokicoindClient) GetBlockHeaderVerbose(
	hash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {

	return c.chainConn.client.GetBlockHeaderVerbose(hash)
}

// IsCurrent returns whether the chain backend considers its view of the network
// as "current".
func (c *FlokicoindClient) IsCurrent() bool {
	bestHash, _, err := c.GetBestBlock()
	if err != nil {
		return false
	}
	bestHeader, err := c.GetBlockHeader(bestHash)
	if err != nil {
		return false
	}
	return bestHeader.Timestamp.After(time.Now().Add(-isCurrentDelta))
}

// GetRawTransactionVerbose returns a TxRawResult from the tx hash.
func (c *FlokicoindClient) GetRawTransactionVerbose(hash *chainhash.Hash) (*chainjson.TxRawResult, error) {

	return c.chainConn.client.GetRawTransactionVerbose(hash)
}

// GetRawTransaction returns a `chainutil.Tx` from the tx hash.
func (c *FlokicoindClient) GetRawTransaction(
	hash *chainhash.Hash) (*chainutil.Tx, error) {

	return c.chainConn.client.GetRawTransaction(hash)
}

// GetRawMempool returns the raw mempool.
func (c *FlokicoindClient) GetRawMempool() ([]*chainhash.Hash, error) {
	return c.chainConn.client.GetRawMempool()
}

// GetTxOut returns a txout from the outpoint info provided.
func (c *FlokicoindClient) GetTxOut(txHash *chainhash.Hash, index uint32,
	mempool bool) (*chainjson.GetTxOutResult, error) {

	return c.chainConn.client.GetTxOut(txHash, index, mempool)
}

// SendRawTransaction sends a raw transaction via flokicoind.
func (c *FlokicoindClient) SendRawTransaction(tx *wire.MsgTx,
	allowHighFees bool) (*chainhash.Hash, error) {

	txid, err := c.chainConn.client.SendRawTransaction(tx, allowHighFees)
	if err != nil {
		return nil, c.MapRPCErr(err)
	}

	return txid, nil
}

// MapRPCErr takes an error returned from calling RPC methods from various
// chain backends and maps it to an defined error here.
func (c *FlokicoindClient) MapRPCErr(rpcErr error) error {
	// Try to match it against flokicoind's error.
	for i := uint32(0); i < uint32(errSentinel); i++ {
		err := RPCErr(i)
		if matchErrStr(rpcErr, err.Error()) {
			return err
		}
	}

	// Perhaps the backend is a newer version of flokicoind, try to match it
	// against the v28.0 and later errors.
	for flokicoindErr, matchedErr := range Flokicoind28ErrMap {
		// Match it against flokicoind's error.
		if matchErrStr(rpcErr, flokicoindErr) {
			return matchedErr
		}
	}

	// If not matched, return the original error wrapped.
	return fmt.Errorf("%w: %v", ErrUndefined, rpcErr)
}

// TestMempoolAcceptCmd returns result of mempool acceptance tests indicating
// if raw transaction(s) would be accepted by mempool.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) TestMempoolAccept(txns []*wire.MsgTx,
	maxFeeRate float64) ([]*chainjson.TestMempoolAcceptResult, error) {

	return c.chainConn.client.TestMempoolAccept(txns, maxFeeRate)
}

// Notifications returns a channel to retrieve notifications from.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) Notifications() <-chan interface{} {
	return c.notificationQueue.ChanOut()
}

// NotifyReceived allows the chain backend to notify the caller whenever a
// transaction pays to any of the given addresses.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) NotifyReceived(addrs []chainutil.Address) error {
	_ = c.NotifyBlocks()

	c.updateWatchedFilters(addrs)

	return nil
}

// NotifySpent allows the chain backend to notify the caller whenever a
// transaction spends any of the given outpoints.
func (c *FlokicoindClient) NotifySpent(outPoints []*wire.OutPoint) error {

	// Send the outpoints so the client will cache them.
	c.updateWatchedFilters(outPoints)

	_ = c.NotifyBlocks()

	// Now we do a quick check in current mempool to see if we already have
	// txes that spends the given outpoints.
	for _, op := range outPoints {
		op := op

		// Check if the input is seen in mempool.
		txid, found := c.chainConn.events.LookupInputSpend(*op)
		if !found {
			// Nothing found, continue to check the next.
			continue
		}

		// Found the tx that spends the input, now we fetch the raw tx
		// and send notification.
		tx, err := c.GetRawTransaction(&txid)
		if err != nil {
			log.Errorf("Unable to get raw transaction for %v, "+
				"err: %v", txid, err)
			continue
		}

		// Construct a record.
		rec, err := wtxmgr.NewTxRecordFromMsgTx(tx.MsgTx(), time.Now())
		if err != nil {
			log.Errorf("Cannot create transaction record for tx: "+
				"%v, err: %v", tx.Hash(), err)
			continue
		}

		// Send to notification immediately.
		c.onRelevantTx(rec, nil)
	}

	return nil
}

// NotifyTx allows the chain backend to notify the caller whenever any of the
// given transactions confirm within the chain.
func (c *FlokicoindClient) NotifyTx(txids []chainhash.Hash) error {

	c.updateWatchedFilters(txids)

	_ = c.NotifyBlocks()

	return nil
}

// NotifyBlocks allows the chain backend to notify the caller whenever a block
// is connected or disconnected.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) NotifyBlocks() error {
	// We'll guard the goroutine being spawned below by the notifyBlocks
	// variable we'll use atomically. We'll make sure to reset it in case of
	// a failure before spawning the goroutine so that it can be retried.
	if !atomic.CompareAndSwapUint32(&c.notifyBlocks, 0, 1) {
		return nil
	}

	// Re-evaluate our known best block since it's possible that blocks have
	// occurred between now and when the client was created. This ensures we
	// don't detect a new notified block as a potential reorg.
	bestHash, bestHeight, err := c.GetBestBlock()
	if err != nil {
		atomic.StoreUint32(&c.notifyBlocks, 0)
		return fmt.Errorf("unable to retrieve best block: %w", err)
	}
	bestHeader, err := c.GetBlockHeaderVerbose(bestHash)
	if err != nil {
		atomic.StoreUint32(&c.notifyBlocks, 0)
		return fmt.Errorf("unable to retrieve header for best block: "+
			"%w", err)
	}

	c.bestBlockMtx.Lock()
	c.bestBlock.Hash = *bestHash
	c.bestBlock.Height = bestHeight
	c.bestBlock.Timestamp = time.Unix(bestHeader.Time, 0)
	c.bestBlockMtx.Unlock()

	// Include the client in the set of rescan clients of the backing
	// flokicoind connection in order to receive ZMQ event notifications for
	// new blocks and transactions.
	c.chainConn.AddClient(c)

	c.wg.Add(1)
	go c.ntfnHandler()

	return nil
}

// shouldNotifyBlocks determines whether the client should send block
// notifications to the caller.
func (c *FlokicoindClient) shouldNotifyBlocks() bool {
	return atomic.LoadUint32(&c.notifyBlocks) == 1
}

// LoadTxFilter uses the given filters to what we should match transactions
// against to determine if they are relevant to the client. The reset argument
// is used to reset the current filters.
//
// The current filters supported are of the following types:
//
//	[]chainutil.Address
//	[]wire.OutPoint
//	[]*wire.OutPoint
//	map[wire.OutPoint]chainutil.Address
//	[]chainhash.Hash
//	[]*chainhash.Hash
func (c *FlokicoindClient) LoadTxFilter(reset bool, filters ...interface{}) error {
	if reset {
		c.resetWatchedFilters()
	}

	// In order to make this operation atomic, we'll iterate through the
	// filters twice: the first to ensure there aren't any unsupported
	// filter types, and the second to actually update our filters.
	for _, filter := range filters {
		switch filter := filter.(type) {
		case []chainutil.Address, []wire.OutPoint, []*wire.OutPoint,
			map[wire.OutPoint]chainutil.Address, []chainhash.Hash,
			[]*chainhash.Hash:

			// Proceed to check the next filter type.
		default:
			return fmt.Errorf("unsupported filter type %T", filter)
		}
	}

	for _, filter := range filters {
		c.updateWatchedFilters(filter)
	}

	return nil
}

// RescanBlocks rescans any blocks passed, returning only the blocks that
// matched as []chainjson.BlockDetails.
func (c *FlokicoindClient) RescanBlocks(
	blockHashes []chainhash.Hash) ([]chainjson.RescannedBlock, error) {

	rescannedBlocks := make([]chainjson.RescannedBlock, 0, len(blockHashes))
	for _, hash := range blockHashes {
		hash := hash

		header, err := c.GetBlockHeaderVerbose(&hash)
		if err != nil {
			log.Warnf("Unable to get header %s from flokicoind: %s",
				hash, err)
			continue
		}

		// Prevent fetching the block completely if we know we shouldn't
		// filter it.
		if !c.shouldFilterBlock(time.Unix(header.Time, 0)) {
			continue
		}

		block, err := c.GetBlock(&hash)
		if err != nil {
			log.Warnf("Unable to get block %s from flokicoind: %s",
				hash, err)
			continue
		}

		relevantTxs := c.filterBlock(block, header.Height, false)
		if len(relevantTxs) > 0 {
			rescannedBlock := chainjson.RescannedBlock{
				Hash: hash.String(),
			}
			for _, tx := range relevantTxs {
				rescannedBlock.Transactions = append(
					rescannedBlock.Transactions,
					hex.EncodeToString(tx.SerializedTx),
				)
			}

			rescannedBlocks = append(rescannedBlocks, rescannedBlock)
		}
	}

	return rescannedBlocks, nil
}

// Rescan rescans from the block with the given hash until the current block,
// after adding the passed addresses and outpoints to the client's watch list.
func (c *FlokicoindClient) Rescan(blockHash *chainhash.Hash,
	addresses []chainutil.Address, outPoints map[wire.OutPoint]chainutil.Address) error {

	// A block hash is required to use as the starting point of the rescan.
	if blockHash == nil {
		return errors.New("rescan requires a starting block hash")
	}

	// We'll then update our filters with the given outpoints and addresses.
	c.updateWatchedFilters(addresses)
	c.updateWatchedFilters(outPoints)

	// Once the filters have been updated, we can begin the rescan.
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		err := c.rescan(*blockHash)
		if err != nil {
			log.Errorf("Unable to complete chain rescan: %v", err)
		}
	}()

	return nil
}

// Start initializes the flokicoind rescan client using the backing flokicoind
// connection and starts all goroutines necessary in order to process rescans
// and ZMQ notifications.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) Start() error {
	if !atomic.CompareAndSwapInt32(&c.started, 0, 1) {
		return nil
	}

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
	bestHeader, err := c.GetBlockHeaderVerbose(bestHash)
	if err != nil {
		return fmt.Errorf("unable to retrieve header for best block: "+
			"%w", err)
	}

	c.bestBlockMtx.Lock()
	c.bestBlock = waddrmgr.BlockStamp{
		Hash:      *bestHash,
		Height:    bestHeight,
		Timestamp: time.Unix(bestHeader.Time, 0),
	}
	c.bestBlockMtx.Unlock()

	return nil
}

// Stop stops the flokicoind rescan client from processing rescans and ZMQ
// notifications.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) Stop() {
	if !atomic.CompareAndSwapInt32(&c.stopped, 0, 1) {
		return
	}

	close(c.quit)

	// Remove this client's reference from the flokicoind connection to
	// prevent sending notifications to it after it's been stopped.
	c.chainConn.RemoveClient(c.id)

	c.notificationQueue.Stop()
}

// WaitForShutdown blocks until the client has finished disconnecting and all
// handlers have exited.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) WaitForShutdown() {
	c.wg.Wait()
}

// ntfnHandler handles the logic to retrieve ZMQ notifications from the backing
// flokicoind connection.
//
// NOTE: This must be called as a goroutine.
func (c *FlokicoindClient) ntfnHandler() {
	defer c.wg.Done()

	for {
		select {
		case tx := <-c.txNtfns:
			txDetails := chainutil.NewTx(tx)
			_, _, err := c.filterTx(txDetails, nil, true)
			if err != nil {
				log.Errorf("Unable to filter transaction %v: %v",
					txDetails.Hash(), err)
			}

		case newBlock := <-c.blockNtfns:
			// If the new block's previous hash matches the best
			// hash known to us, then the new block is the next
			// successor, so we'll update our best block to reflect
			// this and determine if this new block matches any of
			// our existing filters.
			c.bestBlockMtx.RLock()
			bestBlock := c.bestBlock
			c.bestBlockMtx.RUnlock()
			if newBlock.Header.PrevBlock == bestBlock.Hash {
				newBlockHeight := bestBlock.Height + 1
				_ = c.filterBlock(newBlock, newBlockHeight, true)

				// With the block successfully filtered, we'll
				// make it our new best block.
				bestBlock.Hash = newBlock.BlockHash()
				bestBlock.Height = newBlockHeight
				bestBlock.Timestamp = newBlock.Header.Timestamp

				c.bestBlockMtx.Lock()
				c.bestBlock = bestBlock
				c.bestBlockMtx.Unlock()

				continue
			}

			// Otherwise, we've encountered a reorg.
			if err := c.reorg(bestBlock, newBlock); err != nil {
				log.Errorf("Unable to process chain reorg: %v",
					err)
			}
		case <-c.quit:
			return
		}
	}
}

// SetBirthday sets the birthday of the flokicoind rescan client.
//
// NOTE: This should be done before the client has been started in order for it
// to properly carry its duties.
func (c *FlokicoindClient) SetBirthday(t time.Time) {
	c.birthday = t
}

// BlockStamp returns the latest block notified by the client, or an error
// if the client has been shut down.
func (c *FlokicoindClient) BlockStamp() (*waddrmgr.BlockStamp, error) {
	c.bestBlockMtx.RLock()
	bestBlock := c.bestBlock
	c.bestBlockMtx.RUnlock()

	return &bestBlock, nil
}

// onBlockConnected is a callback that's executed whenever a new block has been
// detected. This will queue a BlockConnected notification to the caller.
func (c *FlokicoindClient) onBlockConnected(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	if c.shouldNotifyBlocks() {
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
}

// onFilteredBlockConnected is an alternative callback that's executed whenever
// a new block has been detected. It serves the same purpose as
// onBlockConnected, but it also includes a list of the relevant transactions
// found within the block being connected. This will queue a
// FilteredBlockConnected notification to the caller.
func (c *FlokicoindClient) onFilteredBlockConnected(height int32,
	header *wire.BlockHeader, relevantTxs []*wtxmgr.TxRecord) {

	if c.shouldNotifyBlocks() {
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
}

// onBlockDisconnected is a callback that's executed whenever a block has been
// disconnected. This will queue a BlockDisconnected notification to the caller
// with the details of the block being disconnected.
func (c *FlokicoindClient) onBlockDisconnected(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	if c.shouldNotifyBlocks() {
		select {
		case c.notificationQueue.ChanIn() <- BlockDisconnected{
			Block: wtxmgr.Block{
				Hash:   *hash,
				Height: height,
			},
			Time: timestamp,
		}:
		case <-c.quit:
		}
	}
}

// onRelevantTx is a callback that's executed whenever a transaction is relevant
// to the caller. This means that the transaction matched a specific item in the
// client's different filters. This will queue a RelevantTx notification to the
// caller.
func (c *FlokicoindClient) onRelevantTx(tx *wtxmgr.TxRecord,
	blockDetails *chainjson.BlockDetails) {

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

// onRescanProgress is a callback that's executed whenever a rescan is in
// progress. This will queue a RescanProgress notification to the caller with
// the current rescan progress details.
func (c *FlokicoindClient) onRescanProgress(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	select {
	case c.notificationQueue.ChanIn() <- &RescanProgress{
		Hash:   *hash,
		Height: height,
		Time:   timestamp,
	}:
	case <-c.quit:
	}
}

// onRescanFinished is a callback that's executed whenever a rescan has
// finished. This will queue a RescanFinished notification to the caller with
// the details of the last block in the range of the rescan.
func (c *FlokicoindClient) onRescanFinished(hash *chainhash.Hash, height int32,
	timestamp time.Time) {

	select {
	case c.notificationQueue.ChanIn() <- &RescanFinished{
		Hash:   hash,
		Height: height,
		Time:   timestamp,
	}:
	case <-c.quit:
	}
}

// reorg processes a reorganization during chain synchronization. This is
// separate from a rescan's handling of a reorg. This will rewind back until it
// finds a common ancestor and notify all the new blocks since then.
func (c *FlokicoindClient) reorg(currentBlock waddrmgr.BlockStamp,
	reorgBlock *wire.MsgBlock) error {

	// Retrieve the best known height based on the block which caused the
	// reorg. This way, we can preserve the chain of blocks we need to
	// retrieve.
	bestHash := reorgBlock.BlockHash()
	bestHeight, err := c.GetBlockHeight(&bestHash)
	if err != nil {
		return fmt.Errorf("unable to get block height for %v: %w",
			bestHash, err)
	}

	log.Debugf("Possible reorg at block: height=%v, hash=%v", bestHeight,
		bestHash)

	if bestHeight < currentBlock.Height {
		log.Debugf("Detected multiple reorgs: best_height=%v below "+
			"current_height=%v", bestHeight, currentBlock.Height)
		return nil
	}

	// We'll now keep track of all the blocks known to the *chain*, starting
	// from the best block known to us until the best block in the chain.
	// This will let us fast-forward despite any future reorgs.
	blocksToNotify := list.New()
	blocksToNotify.PushFront(reorgBlock)
	previousBlock := reorgBlock.Header.PrevBlock
	for i := bestHeight - 1; i >= currentBlock.Height; i-- {
		block, err := c.GetBlock(&previousBlock)
		if err != nil {
			return fmt.Errorf("unable to get block %v: %w",
				previousBlock, err)
		}
		blocksToNotify.PushFront(block)
		previousBlock = block.Header.PrevBlock
	}

	// Rewind back to the last common ancestor block using the previous
	// block hash from each header to avoid any race conditions. If we
	// encounter more reorgs, they'll be queued and we'll repeat the cycle.
	//
	// We'll start by retrieving the header to the best block known to us.
	currentHeader, err := c.GetBlockHeader(&currentBlock.Hash)
	if err != nil {
		return fmt.Errorf("unable to get block header for %v: %w",
			currentBlock.Hash, err)
	}

	// Then, we'll walk backwards in the chain until we find our common
	// ancestor.
	for previousBlock != currentHeader.PrevBlock {
		// Since the previous hashes don't match, the current block has
		// been reorged out of the chain, so we should send a
		// BlockDisconnected notification for it.
		log.Debugf("Disconnecting block: height=%v, hash=%v",
			currentBlock.Height, currentBlock.Hash)

		c.onBlockDisconnected(
			&currentBlock.Hash, currentBlock.Height,
			currentBlock.Timestamp,
		)

		// Our current block should now reflect the previous one to
		// continue the common ancestor search.
		prevBlock := &currentHeader.PrevBlock
		currentHeader, err = c.GetBlockHeader(prevBlock)
		if err != nil {
			return fmt.Errorf("unable to get block header for %v: %w",
				prevBlock, err)
		}

		currentBlock.Height--
		currentBlock.Hash = currentHeader.PrevBlock
		currentBlock.Timestamp = currentHeader.Timestamp

		// Store the correct block in our list in order to notify it
		// once we've found our common ancestor.
		block, err := c.GetBlock(&previousBlock)
		if err != nil {
			return fmt.Errorf("unable to get block %v: %w",
				previousBlock, err)
		}
		blocksToNotify.PushFront(block)
		previousBlock = block.Header.PrevBlock
	}

	// Disconnect the last block from the old chain. Since the previous
	// block remains the same between the old and new chains, the tip will
	// now be the last common ancestor.
	log.Debugf("Disconnecting block: height=%v, hash=%v",
		currentBlock.Height, currentBlock.Hash)

	c.onBlockDisconnected(
		&currentBlock.Hash, currentBlock.Height, currentHeader.Timestamp,
	)

	currentBlock.Height--

	// Now we fast-forward to the new block, notifying along the way.
	for blocksToNotify.Front() != nil {
		nextBlock := blocksToNotify.Front().Value.(*wire.MsgBlock)
		nextHeight := currentBlock.Height + 1
		nextHash := nextBlock.BlockHash()
		nextHeader, err := c.GetBlockHeader(&nextHash)
		if err != nil {
			return fmt.Errorf("unable to get block header for %v: %w",
				nextHash, err)
		}

		_ = c.filterBlock(nextBlock, nextHeight, true)

		currentBlock.Height = nextHeight
		currentBlock.Hash = nextHash
		currentBlock.Timestamp = nextHeader.Timestamp

		blocksToNotify.Remove(blocksToNotify.Front())
	}

	c.bestBlockMtx.Lock()
	c.bestBlock = currentBlock
	c.bestBlockMtx.Unlock()

	return nil
}

// FilterBlocks scans the blocks contained in the FilterBlocksRequest for any
// addresses of interest. Each block will be fetched and filtered sequentially,
// returning a FilterBlocksReponse for the first block containing a matching
// address. If no matches are found in the range of blocks requested, the
// returned response will be nil.
//
// NOTE: This is part of the chain.Interface interface.
func (c *FlokicoindClient) FilterBlocks(
	req *FilterBlocksRequest) (*FilterBlocksResponse, error) {

	blockFilterer := NewBlockFilterer(c.chainConn.cfg.ChainParams, req)

	// Iterate over the requested blocks, fetching each from the rpc client.
	// Each block will scanned using the reverse addresses indexes generated
	// above, breaking out early if any addresses are found.
	for i, block := range req.Blocks {
		// TODO(conner): add prefetching, since we already know we'll be
		// fetching *every* block
		rawBlock, err := c.GetBlock(&block.Hash)
		if err != nil {
			return nil, err
		}

		if !blockFilterer.FilterBlock(rawBlock) {
			continue
		}

		// If any external or internal addresses were detected in this
		// block, we return them to the caller so that the rescan
		// windows can widened with subsequent addresses. The
		// `BatchIndex` is returned so that the caller can compute the
		// *next* block from which to begin again.
		resp := &FilterBlocksResponse{
			BatchIndex:         uint32(i),
			BlockMeta:          block,
			FoundExternalAddrs: blockFilterer.FoundExternal,
			FoundInternalAddrs: blockFilterer.FoundInternal,
			FoundOutPoints:     blockFilterer.FoundOutPoints,
			RelevantTxns:       blockFilterer.RelevantTxns,
		}

		return resp, nil
	}

	// No addresses were found for this range.
	return nil, nil
}

// rescan performs a rescan of the chain using a flokicoind backend, from the
// specified hash to the best known hash, while watching out for reorgs that
// happen during the rescan. It uses the addresses and outputs being tracked by
// the client in the watch list. This is called only within a queue processing
// loop.
func (c *FlokicoindClient) rescan(start chainhash.Hash) error {
	// We start by getting the best already processed block. We only use
	// the height, as the hash can change during a reorganization, which we
	// catch by testing connectivity from known blocks to the previous
	// block.
	bestHash, bestHeight, err := c.GetBestBlock()
	if err != nil {
		return err
	}
	bestHeader, err := c.GetBlockHeaderVerbose(bestHash)
	if err != nil {
		return err
	}
	bestBlock := waddrmgr.BlockStamp{
		Hash:      *bestHash,
		Height:    bestHeight,
		Timestamp: time.Unix(bestHeader.Time, 0),
	}

	// Create a list of headers sorted in forward order. We'll use this in
	// the event that we need to backtrack due to a chain reorg.
	headers := list.New()
	previousHeader, err := c.GetBlockHeaderVerbose(&start)
	if err != nil {
		return err
	}
	previousHash, err := chainhash.NewHashFromStr(previousHeader.Hash)
	if err != nil {
		return err
	}
	headers.PushBack(previousHeader)

	log.Debugf("Rescanning from block height %v to %v",
		previousHeader.Height+1, bestBlock.Height)

	// Cycle through all of the blocks known to flokicoind, being mindful of
	// reorgs.
	for i := previousHeader.Height + 1; i <= bestBlock.Height; i++ {
		hash, err := c.GetBlockHash(int64(i))
		if err != nil {
			return err
		}

		// If the previous header is before the wallet birthday, fetch
		// the current header and construct a dummy block, rather than
		// fetching the whole block itself. This speeds things up as we
		// no longer have to fetch the whole block when we know it won't
		// match any of our filters.
		var block *wire.MsgBlock
		afterBirthday := previousHeader.Time >= c.birthday.Unix()
		if !afterBirthday {
			header, err := c.GetBlockHeader(hash)
			if err != nil {
				return err
			}
			block = &wire.MsgBlock{
				Header: *header,
			}

			afterBirthday = c.birthday.Before(header.Timestamp)
			if afterBirthday {
				c.onRescanProgress(
					previousHash, i,
					block.Header.Timestamp,
				)
			}
		}

		if afterBirthday {
			block, err = c.GetBlock(hash)
			if err != nil {
				return err
			}
		}

		for block.Header.PrevBlock.String() != previousHeader.Hash {
			// If we're in this for loop, it looks like we've been
			// reorganized. We now walk backwards to the common
			// ancestor between the best chain and the known chain.
			//
			// First, we signal a disconnected block to rewind the
			// rescan state.
			c.onBlockDisconnected(
				previousHash, previousHeader.Height,
				time.Unix(previousHeader.Time, 0),
			)

			// Get the previous block of the best chain.
			hash, err := c.GetBlockHash(int64(i - 1))
			if err != nil {
				return err
			}
			block, err = c.GetBlock(hash)
			if err != nil {
				return err
			}

			// Then, we'll the get the header of this previous
			// block.
			if headers.Back() != nil {
				// If it's already in the headers list, we can
				// just get it from there and remove the
				// current hash.
				headers.Remove(headers.Back())
				if headers.Back() != nil {
					previousHeader = headers.Back().
						Value.(*chainjson.GetBlockHeaderVerboseResult)
					previousHash, err = chainhash.NewHashFromStr(
						previousHeader.Hash,
					)
					if err != nil {
						return err
					}
				}
			} else {
				// Otherwise, we get it from flokicoind.
				previousHash, err = chainhash.NewHashFromStr(
					previousHeader.PreviousHash,
				)
				if err != nil {
					return err
				}
				previousHeader, err = c.GetBlockHeaderVerbose(
					previousHash,
				)
				if err != nil {
					return err
				}
			}
		}

		// Now that we've ensured we haven't come across a reorg, we'll
		// add the current block header to our list of headers.
		blockHash := block.BlockHash()
		previousHash = &blockHash
		previousHeader = &chainjson.GetBlockHeaderVerboseResult{
			Hash:         blockHash.String(),
			Height:       i,
			PreviousHash: block.Header.PrevBlock.String(),
			Time:         block.Header.Timestamp.Unix(),
		}
		headers.PushBack(previousHeader)

		// Notify the block and any of its relevant transacations.
		_ = c.filterBlock(block, i, true)

		if i%10000 == 0 {
			c.onRescanProgress(
				previousHash, i, block.Header.Timestamp,
			)
		}

		// If we've reached the previously best known block, check to
		// make sure the underlying node hasn't synchronized additional
		// blocks. If it has, update the best known block and continue
		// to rescan to that point.
		if i == bestBlock.Height {
			bestHash, bestHeight, err = c.GetBestBlock()
			if err != nil {
				return err
			}
			bestHeader, err = c.GetBlockHeaderVerbose(bestHash)
			if err != nil {
				return err
			}

			bestBlock.Hash = *bestHash
			bestBlock.Height = bestHeight
			bestBlock.Timestamp = time.Unix(bestHeader.Time, 0)
		}
	}

	c.onRescanFinished(bestHash, bestHeight, time.Unix(bestHeader.Time, 0))

	return nil
}

// shouldFilterBlock determines whether we should filter a block based on its
// timestamp or our watch list.
func (c *FlokicoindClient) shouldFilterBlock(blockTimestamp time.Time) bool {
	c.watchMtx.RLock()
	hasEmptyFilter := len(c.watchedAddresses) == 0 &&
		len(c.watchedOutPoints) == 0 && len(c.watchedTxs) == 0
	c.watchMtx.RUnlock()

	return !(blockTimestamp.Before(c.birthday) || hasEmptyFilter)
}

// filterBlock filters a block for watched outpoints and addresses, and returns
// any matching transactions, sending notifications along the way.
func (c *FlokicoindClient) filterBlock(block *wire.MsgBlock, height int32,
	notify bool) []*wtxmgr.TxRecord {

	// If this block happened before the client's birthday or we have
	// nothing to filter for, then we'll skip it entirely.
	blockHash := block.BlockHash()
	if !c.shouldFilterBlock(block.Header.Timestamp) {
		if notify {
			c.onFilteredBlockConnected(height, &block.Header, nil)
			c.onBlockConnected(
				&blockHash, height, block.Header.Timestamp,
			)
		}
		return nil
	}

	if c.shouldNotifyBlocks() {
		log.Debugf("Filtering block %d (%s) with %d transactions",
			height, block.BlockHash(), len(block.Transactions))
	}

	// Create a block details template to use for all of the confirmed
	// transactions found within this block.
	blockDetails := &chainjson.BlockDetails{
		Hash:   blockHash.String(),
		Height: height,
		Time:   block.Header.Timestamp.Unix(),
	}

	// Now, we'll through all of the transactions in the block keeping track
	// of any relevant to the caller.
	var relevantTxs []*wtxmgr.TxRecord
	confirmedTxs := make(map[chainhash.Hash]struct{})
	for i, tx := range block.Transactions {
		// Update the index in the block details with the index of this
		// transaction.
		blockDetails.Index = i
		txDetails := chainutil.NewTx(tx)
		isRelevant, rec, err := c.filterTx(
			txDetails, blockDetails, notify,
		)
		if err != nil {
			log.Warnf("Unable to filter transaction %v: %v",
				*txDetails.Hash(), err)
			continue
		}

		if isRelevant {
			relevantTxs = append(relevantTxs, rec)
			confirmedTxs[*txDetails.Hash()] = struct{}{}
		}
	}

	// Update the expiration map by setting the block's confirmed
	// transactions and deleting any in the mempool that were confirmed
	// over 288 blocks ago.
	c.watchMtx.Lock()
	c.expiredMempool[height] = confirmedTxs
	if oldBlock, ok := c.expiredMempool[height-288]; ok {
		for txHash := range oldBlock {
			delete(c.mempool, txHash)
		}
		delete(c.expiredMempool, height-288)
	}
	c.watchMtx.Unlock()

	if notify {
		c.onFilteredBlockConnected(height, &block.Header, relevantTxs)
		c.onBlockConnected(&blockHash, height, block.Header.Timestamp)
	}

	return relevantTxs
}

// filterTx determines whether a transaction is relevant to the client by
// inspecting the client's different filters.
func (c *FlokicoindClient) filterTx(txDetails *chainutil.Tx,
	blockDetails *chainjson.BlockDetails, notify bool) (bool,
	*wtxmgr.TxRecord, error) {

	if blockDetails != nil {
		txDetails.SetIndex(blockDetails.Index)
	}

	rec, err := wtxmgr.NewTxRecordFromMsgTx(txDetails.MsgTx(), time.Now())
	if err != nil {
		log.Errorf("Cannot create transaction record for relevant "+
			"tx: %v", err)
		return false, nil, err
	}
	if blockDetails != nil {
		rec.Received = time.Unix(blockDetails.Time, 0)
	}

	// We'll begin the filtering process by holding the lock to ensure we
	// match exactly against what's currently in the filters.
	c.watchMtx.Lock()
	defer c.watchMtx.Unlock()

	// If we've already seen this transaction and it's now been confirmed,
	// then we'll shortcut the filter process by immediately sending a
	// notification to the caller that the filter matches.
	if _, ok := c.mempool[*txDetails.Hash()]; ok {
		if notify && blockDetails != nil {
			c.onRelevantTx(rec, blockDetails)
			return true, rec, nil
		}
	}

	// Otherwise, this is a new transaction we have yet to see. We'll need
	// to determine if this transaction is somehow relevant to the caller.
	var isRelevant bool

	// We'll start by checking all inputs and determining whether it spends
	// an existing outpoint or a pkScript encoded as an address in our watch
	// list.
	for _, txIn := range txDetails.MsgTx().TxIn {
		// If it matches an outpoint in our watch list, we can exit our
		// loop early.
		if _, ok := c.watchedOutPoints[txIn.PreviousOutPoint]; ok {
			isRelevant = true
			break
		}

		sig := txIn.SignatureScript
		witness := txIn.Witness

		// Otherwise, we'll check whether it matches a pkScript in our
		// watch list encoded as an address. To do so, we'll re-derive
		// the pkScript of the output the input is attempting to spend.
		pkScript, err := txscript.ComputePkScript(sig, witness)
		if err != nil {
			// Non-standard outputs can be safely skipped.
			log.Warnf("Received non-standard input sig=%x, "+
				"witness=%x", sig, witness)

			continue
		}
		addr, err := pkScript.Address(c.chainConn.cfg.ChainParams)
		if err != nil {
			// Non-standard outputs can be safely skipped.
			continue
		}
		if _, ok := c.watchedAddresses[addr.String()]; ok {
			isRelevant = true
			break
		}
	}

	// We'll also cycle through its outputs to determine if it pays to
	// any of the currently watched addresses. If an output matches, we'll
	// add it to our watch list.
	for i, txOut := range txDetails.MsgTx().TxOut {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(
			txOut.PkScript, c.chainConn.cfg.ChainParams,
		)

		if err != nil {
			// Non-standard outputs can be safely skipped.
			log.Warnf("Received non-standard output script=%x",
				txOut.PkScript)

			continue
		}

		for _, addr := range addrs {
			if _, ok := c.watchedAddresses[addr.String()]; ok {
				isRelevant = true
				op := wire.OutPoint{
					Hash:  *txDetails.Hash(),
					Index: uint32(i),
				}
				c.watchedOutPoints[op] = struct{}{}
			}
		}
	}

	// If the transaction didn't pay to any of our watched addresses, we'll
	// check if we're currently watching for the hash of this transaction.
	if !isRelevant {
		if _, ok := c.watchedTxs[*txDetails.Hash()]; ok {
			isRelevant = true
		}
	}

	// If the transaction is not relevant to us, we can simply exit.
	if !isRelevant {
		return false, rec, nil
	}

	log.Tracef("Found relevant tx %v", NewLogClosure(func() string {
		return spew.Sdump(txDetails)
	}))

	// Otherwise, the transaction matched our filters, so we should dispatch
	// a notification for it. If it's still unconfirmed, we'll include it in
	// our mempool so that it can also be notified as part of
	// FilteredBlockConnected once it confirms.
	if blockDetails == nil {
		c.mempool[*txDetails.Hash()] = struct{}{}
	}

	c.onRelevantTx(rec, blockDetails)

	return true, rec, nil
}

// LookupInputMempoolSpend returns the transaction hash and true if the given
// input is found being spent in mempool, otherwise it returns nil and false.
func (c *FlokicoindClient) LookupInputMempoolSpend(op wire.OutPoint) (
	chainhash.Hash, bool) {

	return c.chainConn.events.LookupInputSpend(op)
}

// resetWatchedFilters empties the maps used to track outpoints, addresses, and
// txns.
func (c *FlokicoindClient) resetWatchedFilters() {
	c.watchMtx.Lock()
	defer c.watchMtx.Unlock()

	c.watchedOutPoints = make(map[wire.OutPoint]struct{})
	c.watchedAddresses = make(map[string]struct{})
	c.watchedTxs = make(map[chainhash.Hash]struct{})
}

// updateWatchedFilters is used to update the internal maps that track the
// watched addresses, outpoints, or txns.
func (c *FlokicoindClient) updateWatchedFilters(update any) {
	c.watchMtx.Lock()
	defer c.watchMtx.Unlock()

	switch update := update.(type) {
	// We're adding the addresses to our filter.
	case []chainutil.Address:
		for _, addr := range update {
			c.watchedAddresses[addr.String()] = struct{}{}
		}

	// We're adding the outpoints to our filter.
	case []wire.OutPoint:
		for _, op := range update {
			c.watchedOutPoints[op] = struct{}{}
		}

	case []*wire.OutPoint:
		for _, op := range update {
			c.watchedOutPoints[*op] = struct{}{}
		}

	// We're adding the outpoints that map to the scripts
	// that we should scan for to our filter.
	case map[wire.OutPoint]chainutil.Address:
		for op := range update {
			c.watchedOutPoints[op] = struct{}{}
		}

	// We're adding the transactions to our filter.
	case []chainhash.Hash:
		for _, txid := range update {
			c.watchedTxs[txid] = struct{}{}
		}

	case []*chainhash.Hash:
		for _, txid := range update {
			c.watchedTxs[*txid] = struct{}{}
		}
	}
}
