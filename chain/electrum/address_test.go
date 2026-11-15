// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package electrum

import (
	"testing"

	"github.com/flokiorg/go-flokicoin/chaincfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddressToElectrumScriptHash(t *testing.T) {
	tests := []struct {
		address        string
		wantScriptHash string
	}{
		{
			address:        "FBLGqgP3UHPNYKSHHH3Y9QR1XoBJZCnECm",
			wantScriptHash: "61a5836b0b62b7c54b9bc8650866ba7e2ca1faf48713a6eb54fadc8e40f6a5ca",
		},
	}

	for _, tc := range tests {
		scriptHash, err := AddressToElectrumScriptHash(tc.address, &chaincfg.MainNetParams)
		require.NoError(t, err)
		assert.Equal(t, scriptHash, tc.wantScriptHash)
	}
}
