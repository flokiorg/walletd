// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package legacyrpc

import flog "github.com/flokiorg/go-flokicoin/log"

var log = flog.Disabled

// UseLogger sets the package-wide logger.  Any calls to this function must be
// made before a server is created and used (it is not concurrent safe).
func UseLogger(logger flog.Logger) {
	log = logger
}
