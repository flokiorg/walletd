// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package electrum

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/flokiorg/go-flokicoin/chaincfg"
	"github.com/flokiorg/go-flokicoin/chainutil"
	"github.com/flokiorg/go-flokicoin/txscript"
)

// AddressToElectrumScriptHash converts valid flokicoin address to electrum scriptHash sha256 encoded, reversed and encoded in hex
// https://electrumx.readthedocs.io/en/latest/protocol-basics.html#script-hashes
func AddressToElectrumScriptHash(addressStr string, network *chaincfg.Params) (string, error) {
	address, err := chainutil.DecodeAddress(addressStr, network)
	if err != nil {
		return "", err
	}
	script, err := txscript.PayToAddrScript(address)
	if err != nil {
		return "", err
	}

	hashSum := sha256.Sum256(script)

	for i, j := 0, len(hashSum)-1; i < j; i, j = i+1, j-1 {
		hashSum[i], hashSum[j] = hashSum[j], hashSum[i]
	}

	return hex.EncodeToString(hashSum[:]), nil
}
