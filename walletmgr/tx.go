package walletmgr

import (
	"sort"
	"time"

	"github.com/flokiorg/go-flokicoin/chainjson"
)

type TransactionServiceResultType int

const (
	TRANSACTION_SENT TransactionServiceResultType = iota
	TRANSACTION_RECEIVED
	TRANSACTION_MINED
)

// TransactionServiceResult represents a simplified view of a transaction.
type TransactionServiceResult struct {
	Timestamp     string
	Amount        float64
	Type          TransactionServiceResultType
	TxID          string
	Address       string
	Confirmations int64
}

// aggregateTransactions groups raw transactions into a simplified view.
func (ws *WalletService) aggregateTransactions(txs []chainjson.ListTransactionsResult) []TransactionServiceResult {
	// Map to group transactions by TxID
	txMap := make(map[string]*TransactionServiceResult)

	for _, tx := range txs {
		// Format timestamp
		timestamp := time.Unix(tx.Time, 0).Format("2006-01-02 15:04:05")

		// Check if the transaction is already in the map
		cleanTx, exists := txMap[tx.TxID]
		if !exists {
			cleanTx = &TransactionServiceResult{
				Timestamp:     timestamp,
				Amount:        0,
				TxID:          tx.TxID,
				Address:       tx.Address,
				Confirmations: tx.Confirmations,
			}
			txMap[tx.TxID] = cleanTx
		}

		// Aggregate the amount (net debit/credit)
		cleanTx.Amount = tx.Amount

		// Set the transaction type and details
		if tx.Generated {
			cleanTx.Type = TRANSACTION_MINED
		} else if tx.Amount < 0 {
			cleanTx.Type = TRANSACTION_SENT
		} else {
			cleanTx.Type = TRANSACTION_RECEIVED
		}
	}

	// Collect the clean transactions into a slice
	cleanHistory := []TransactionServiceResult{}
	for _, tx := range txMap {
		cleanHistory = append(cleanHistory, *tx)
	}

	sort.Slice(cleanHistory, func(i, j int) bool {
		// Parse timestamps to compare
		timeI, _ := time.Parse("2006-01-02 15:04:05", cleanHistory[i].Timestamp)
		timeJ, _ := time.Parse("2006-01-02 15:04:05", cleanHistory[j].Timestamp)
		return timeI.After(timeJ)
	})

	return cleanHistory
}
