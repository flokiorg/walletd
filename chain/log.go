// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"github.com/flokiorg/flokicoin-neutrino/query"
	flog "github.com/flokiorg/go-flokicoin/log"
)

// log is a logger that is initialized with no output filters.  This
// means the package will not perform any logging by default until the caller
// requests it.
var log flog.Logger

// The default amount of logging is none.
func init() {
	DisableLog()
}

// DisableLog disables all library log output.  Logging output is disabled
// by default until either UseLogger or SetLogWriter are called.
func DisableLog() {
	log = flog.Disabled
}

// UseLogger uses a specified Logger to output package logging info.
// This should be used in preference to SetLogWriter if the caller is also
// using flog.
func UseLogger(logger flog.Logger) {
	log = logger
	query.UseLogger(logger)
}

// LogClosure is used to provide a closure over expensive logging operations so
// don't have to be performed when the logging level doesn't warrant it.
type LogClosure func() string

// String invokes the underlying function and returns the result.
func (c LogClosure) String() string {
	return c()
}

// NewLogClosure returns a new closure over a function that returns a string
// which itself provides a Stringer interface so that it can be used with the
// logging system.
func NewLogClosure(c func() string) LogClosure {
	return LogClosure(c)
}
