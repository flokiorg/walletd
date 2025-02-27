// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package netparams

import (
	"github.com/flokiorg/go-flokicoin/chaincfg"
	"github.com/flokiorg/go-flokicoin/wire"
)

// Params is used to group parameters for various networks such as the main
// network and test networks.
type Params struct {
	*chaincfg.Params
	RPCClientPort string
	RPCServerPort string
}

// MainNetParams contains parameters specific running walletd and
// flokicoind on the main network (wire.MainNet).
var MainNetParams = Params{
	Params:        &chaincfg.MainNetParams,
	RPCClientPort: "15216",
	RPCServerPort: "15213",
}

// Re contains parameters specific running walletd and
// flokicoind on the test network (version 3) (wire.TestNet3).
var RegressionNetParams = Params{
	Params:        &chaincfg.RegressionNetParams,
	RPCClientPort: "25216",
	RPCServerPort: "25213",
}

// TestNet3Params contains parameters specific running walletd and
// flokicoind on the test network (version 3) (wire.TestNet3).
var TestNet3Params = Params{
	Params:        &chaincfg.TestNet3Params,
	RPCClientPort: "35216",
	RPCServerPort: "35213",
}

// SimNetParams contains parameters specific to the simulation test network
// (wire.SimNet).
var SimNetParams = Params{
	Params:        &chaincfg.SimNetParams,
	RPCClientPort: "45216",
	RPCServerPort: "45213",
}

// SigNetParams contains parameters specific to the signet test network
// (wire.SigNet).
var SigNetParams = Params{
	Params:        &chaincfg.SigNetParams,
	RPCClientPort: "55216",
	RPCServerPort: "55213",
}

// SigNetWire is a helper function that either returns the given chain
// parameter's net value if the parameter represents a signet network or 0 if
// it's not. This is necessary because there can be custom signet networks that
// have a different net value.
func SigNetWire(params *chaincfg.Params) wire.FlokicoinNet {
	if params.Name == chaincfg.SigNetParams.Name {
		return params.Net
	}

	return 0
}
