package chain

import (
	"encoding/json"
	"fmt"

	"github.com/flokiorg/go-flokicoin/chaincfg/chainhash"
	"github.com/flokiorg/go-flokicoin/rpcclient"
	"github.com/flokiorg/go-flokicoin/wire"
)

// FlokicoindEvents is the interface that must be satisfied by any type that
// serves flokicoind block and transactions events.
type FlokicoindEvents interface {
	// TxNotifications will return a channel which will deliver new
	// transactions.
	TxNotifications() <-chan *wire.MsgTx

	// BlockNotifications will return a channel which will deliver new
	// blocks.
	BlockNotifications() <-chan *wire.MsgBlock

	// LookupInputSpend will return the transaction found in mempool that
	// spends the given input.
	LookupInputSpend(op wire.OutPoint) (chainhash.Hash, bool)

	// Start will kick off any goroutines required for operation.
	Start() error

	// Stop will clean up any resources and goroutines.
	Stop() error
}

// Ensure rpcclient.Client implements the rpcClient interface at compile time.
var _ batchClient = (*rpcclient.Client)(nil)

// NewFlokicoindEventSubscriber initialises a new FlokicoinEvents object impl
// depending on the config passed.
func NewFlokicoindEventSubscriber(cfg *FlokicoindConfig, client *rpcclient.Client,
	bClient batchClient) (FlokicoindEvents, error) {

	if cfg.PollingConfig != nil && cfg.ZMQConfig != nil {
		return nil, fmt.Errorf("either PollingConfig or ZMQConfig " +
			"should be specified, not both")
	}

	// Check if the flokicoind node is on a version that has the
	// gettxspendingprevout RPC. If it does, then we don't need to maintain
	// a mempool for ZMQ clients and can maintain a smaller mempool for RPC
	// clients.
	hasRPC, err := hasSpendingPrevoutRPC(client)
	if err != nil {
		return nil, err
	}

	if cfg.PollingConfig != nil {
		if client == nil {
			return nil, fmt.Errorf("rpc client must be given " +
				"if rpc polling is to be used for event " +
				"subscriptions")
		}

		pollingEvents := newFlokicoindRPCPollingEvents(
			cfg.PollingConfig, client, bClient, hasRPC,
		)

		return pollingEvents, nil
	}

	if cfg.ZMQConfig == nil {
		return nil, fmt.Errorf("ZMQConfig must be specified if " +
			"rpcpolling is disabled")
	}

	return newFlokicoindZMQEvents(cfg.ZMQConfig, client, bClient, hasRPC)
}

// hasSpendingPrevoutRPC returns whether or not the flokicoind has the newer
// gettxspendingprevout RPC.
func hasSpendingPrevoutRPC(client *rpcclient.Client) (bool, error) {
	// Fetch the flokicoind version.
	resp, err := client.RawRequest("getnetworkinfo", nil)
	if err != nil {
		return false, err
	}

	info := struct {
		Version int64 `json:"version"`
	}{}

	if err := json.Unmarshal(resp, &info); err != nil {
		return false, err
	}

	// Flokicoind returns a single value representing the semantic version:
	// 10000 * CLIENT_VERSION_MAJOR + 100 * CLIENT_VERSION_MINOR
	// + 1 * CLIENT_VERSION_BUILD
	//
	// The gettxspendingprevout call was added in version 24.0.0, so we
	// return for versions >= 240000.
	return info.Version >= 240000, nil
}
