// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package electrum

// TODO

// import (
// 	"context"
// 	"encoding/hex"
// 	"log"
// 	"testing"

// 	"github.com/flokiorg/go-flokicoin/chainutil"
// 	"github.com/flokiorg/go-flokicoin/chaincfg"
// 	"github.com/flokiorg/go-flokicoin/txscript"
// )

// var (
// 	defaultBackendEndpoint = "example.com:50001"
// 	defaultTransactionID   = "a7663ad6841aa36d3736b6a660246efcac6c0f8148bec1d695656bc206b85b9e"
// 	// defaultStrAddress    = "mny5T4UyGkrBV7yiap7ksXEEjn1P294x91"
// 	defaultStrAddress = "mx5RmqJYYEjtXhiZ7Sa6stEWouiQXU7k5M"
// 	defaultScripthash string
// 	defaultAddress    chainutil.Address
// )

// func init() {
// 	addrDecoded, err := chainutil.DecodeAddress(defaultStrAddress, &chaincfg.TestNet3Params)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defaultAddress = addrDecoded

// 	scripthash, err := txscript.AddrToScripthash(defaultAddress)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defaultScripthash = hex.EncodeToString(scripthash)
// }

// func createConnection(t *testing.T) *Client {
// 	client := NewClient(defaultBackendEndpoint, nil)
// 	if err := client.Start(context.Background()); err != nil {
// 		t.Fatal(err)
// 	}

// 	return client
// }

// func TestBlock(t *testing.T) {

// 	client := createConnection(t)

// 	header, err := client.GetBlockHeader(context.Background(), 0)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	t.Logf("header=%v", header)

// 	headers, err := client.GetBlockHeaders(context.Background(), 0, 2)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("headers=%v", headers)

// 	bestBlock, height, err := client.GetBestBlock(context.Background())
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("bestBlock=%v height=%d", bestBlock, height)

// 	blockhash, _, err := client.GetBlockHash(context.Background(), 6)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("blockhash=%v", blockhash)

// }

// func TestTransaction(t *testing.T) {

// 	client := createConnection(t)

// 	transaction, err := client.GetTransaction(context.Background(), defaultTransactionID)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("transaction=%v", transaction)

// 	rawTransaction, err := client.GetRawTransaction(context.Background(), defaultTransactionID)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("rawTransaction=%v", rawTransaction)

// 	txHash, err := client.GetHashFromPosition(context.Background(), 1, 0)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("txHash=%v", txHash)

// }

// func TestProof(t *testing.T) {

// 	client := createConnection(t)

// 	proof, err := client.GetMerkleProof(context.Background(), defaultTransactionID, 1)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("proof=%v", proof)

// 	proofPos, err := client.GetMerkleProofFromPosition(context.Background(), 1, 0)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("proofPos=%v", proofPos)
// }

// func TestFee(t *testing.T) {

// 	client := createConnection(t)

// 	fee, err := client.GetFee(context.Background(), 2)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("fee=%v", fee)

// 	relayFee, err := client.GetRelayFee(context.Background())
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("relayFee=%v", relayFee)

// 	feeHistogram, err := client.GetFeeHistogram(context.Background())
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("feeHistogram=%v", feeHistogram)
// }

// func TestScriptHash(t *testing.T) {

// 	client := createConnection(t)

// 	balance, err := client.GetBalance(context.Background(), defaultScripthash)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("balance=%v", balance)

// 	history, err := client.GetHistory(context.Background(), defaultScripthash)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("history=%v", len(history))

// 	// GetMempool => deprecated

// 	unspent, err := client.ListUnspent(context.Background(), defaultScripthash)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	t.Logf("unspent=%v", len(unspent))

// }

// func TestSubscribeHeader(t *testing.T) {

// 	client := createConnection(t)

// 	headers, err := client.SubscribeHeaders(context.Background())
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	for header := range headers {
// 		t.Logf("new header: %v", header)
// 	}

// }

// func TestSubscribeScriptHash(t *testing.T) {

// 	client := createConnection(t)

// 	subscription, changes := client.SubscribeScripthash()

// 	subscription.Add(context.Background(), defaultScripthash)

// 	for sh := range changes {
// 		t.Logf("scripthash: %v", sh)
// 	}

// }
