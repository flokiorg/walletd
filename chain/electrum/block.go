// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package electrum

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"

	"github.com/flokiorg/go-flokicoin/chaincfg/chainhash"
	"github.com/flokiorg/go-flokicoin/wire"
)

var (
	// ErrCheckpointHeight is thrown if the checkpoint height is smaller than the block height.
	ErrCheckpointHeight = errors.New("checkpoint height must be greater than or equal to block height")
)

// GetBlockHeaderResp represents the response to GetBlockHeader().
type GetBlockHeaderResp struct {
	Result *GetBlockHeaderResult `json:"result"`
}

// GetBlockHeaderResult represents the content of the result field in the response to GetBlockHeader().
type GetBlockHeaderResult struct {
	Branch []string `json:"branch"`
	Header string   `json:"header"`
	Root   string   `json:"root"`
}

// GetBlockHeader returns the block header at a specific height.
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-block-header
func (s *Client) GetBlockHeader(ctx context.Context, height uint32) (*GetBlockHeaderResult, error) {

	var resp basicResp

	if err := s.request(ctx, "blockchain.block.header", []interface{}{height}, &resp); err != nil {
		return nil, err
	}

	result := &GetBlockHeaderResult{
		Branch: nil,
		Header: resp.Result,
		Root:   "",
	}

	return result, nil
}

// GetBlockHeadersResp represents the response to GetBlockHeaders().
type GetBlockHeadersResp struct {
	Result *GetBlockHeadersResult `json:"result"`
}

// GetBlockHeadersResult represents the content of the result field in the response to GetBlockHeaders().
type GetBlockHeadersResult struct {
	Count   uint32   `json:"count"`
	Headers string   `json:"hex"`
	Max     uint32   `json:"max"`
	Branch  []string `json:"branch,omitempty"`
	Root    string   `json:"root,omitempty"`
}

// GetBlockHeaders return a concatenated chunk of block headers.
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-block-headers
func (s *Client) GetBlockHeaders(ctx context.Context, startHeight, count uint32) (*GetBlockHeadersResult, error) {

	var resp GetBlockHeadersResp

	if err := s.request(ctx, "blockchain.block.headers", []interface{}{startHeight, count}, &resp); err != nil {
		return nil, err
	}

	return resp.Result, nil
}

type BestBlockhashResp struct {
	Result *BestBlockhash `json:"result"`
}

type BestBlockhash struct {
	Height uint32 `json:"height"`
	Header string `json:"hex"`
}

// GetBestBlock uses getblockchaininfo under the hood to provide height and hash.
func (s *Client) GetBestBlock(ctx context.Context) (*chainhash.Hash, int32, error) {
	var infoResp BestBlockhashResp
	if err := s.request(ctx, "blockchain.headers.subscribe", []interface{}{}, &infoResp); err != nil {
		return nil, -1, err
	}

	bytes, err := hex.DecodeString(infoResp.Result.Header)
	if err != nil {
		return nil, -1, err
	}

	hash := chainhash.DoubleHashH(bytes)

	return &hash, int32(infoResp.Result.Height), nil
}

// GetBlockHash calls "blockchain.block.get_hash" for a given height
// and returns the corresponding *chainhash.Hash.
func (s *Client) GetBlockHash(ctx context.Context, height uint32) (*chainhash.Hash, *wire.BlockHeader, error) {

	result, err := s.GetBlockHeader(ctx, height)
	if err != nil {
		return nil, nil, err
	}

	headerBytes, err := hex.DecodeString(result.Header)
	if err != nil {
		return nil, nil, err
	}

	r := bytes.NewReader(headerBytes)

	var bh wire.BlockHeader
	if err := bh.Deserialize(r); err != nil {
		return nil, nil, err
	}

	hash := bh.BlockHash()

	return &hash, &bh, nil
}
