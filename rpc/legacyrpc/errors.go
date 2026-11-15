// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package legacyrpc

import (
	"errors"

	"github.com/flokiorg/go-flokicoin/chainjson"
)

// TODO(jrick): There are several error paths which 'replace' various errors
// with a more appropriate error from the chainjson package.  Create a map of
// these replacements so they can be handled once after an RPC handler has
// returned and before the error is marshaled.

// Error types to simplify the reporting of specific categories of
// errors, and their *chainjson.RPCError creation.
type (
	// DeserializationError describes a failed deserializaion due to bad
	// user input.  It corresponds to chainjson.ErrRPCDeserialization.
	DeserializationError struct {
		error
	}

	// InvalidParameterError describes an invalid parameter passed by
	// the user.  It corresponds to chainjson.ErrRPCInvalidParameter.
	InvalidParameterError struct {
		error
	}

	// ParseError describes a failed parse due to bad user input.  It
	// corresponds to chainjson.ErrRPCParse.
	ParseError struct {
		error
	}
)

// Errors variables that are defined once here to avoid duplication below.
var (
	ErrNeedPositiveAmount = InvalidParameterError{
		errors.New("amount must be positive"),
	}

	ErrNeedPositiveMinconf = InvalidParameterError{
		errors.New("minconf must be positive"),
	}

	ErrAddressNotInWallet = chainjson.RPCError{
		Code:    chainjson.ErrRPCWallet,
		Message: "address not found in wallet",
	}

	ErrAddressTypeUnknown = chainjson.RPCError{
		Code:    chainjson.ErrRPCWalletInvalidAddressType,
		Message: "unknown address type",
	}

	ErrAccountNameNotFound = chainjson.RPCError{
		Code:    chainjson.ErrRPCWalletInvalidAccountName,
		Message: "account name not found",
	}

	ErrUnloadedWallet = chainjson.RPCError{
		Code:    chainjson.ErrRPCWallet,
		Message: "Request requires a wallet but wallet has not loaded yet",
	}

	ErrWalletUnlockNeeded = chainjson.RPCError{
		Code:    chainjson.ErrRPCWalletUnlockNeeded,
		Message: "Enter the wallet passphrase with walletpassphrase first",
	}

	ErrNotImportedAccount = chainjson.RPCError{
		Code:    chainjson.ErrRPCWallet,
		Message: "imported addresses must belong to the imported account",
	}

	ErrNoTransactionInfo = chainjson.RPCError{
		Code:    chainjson.ErrRPCNoTxInfo,
		Message: "No information for transaction",
	}

	ErrReservedAccountName = chainjson.RPCError{
		Code:    chainjson.ErrRPCInvalidParameter,
		Message: "Account name is reserved by RPC server",
	}
)
