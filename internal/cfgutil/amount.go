// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"strconv"
	"strings"

	"github.com/flokiorg/go-flokicoin/chainutil"
)

// AmountFlag embeds a chainutil.Amount and implements the flags.Marshaler and
// Unmarshaler interfaces so it can be used as a config struct field.
type AmountFlag struct {
	chainutil.Amount
}

// NewAmountFlag creates an AmountFlag with a default chainutil.Amount.
func NewAmountFlag(defaultValue chainutil.Amount) *AmountFlag {
	return &AmountFlag{defaultValue}
}

// MarshalFlag satisfies the flags.Marshaler interface.
func (a *AmountFlag) MarshalFlag() (string, error) {
	return a.Amount.String(), nil
}

// UnmarshalFlag satisfies the flags.Unmarshaler interface.
func (a *AmountFlag) UnmarshalFlag(value string) error {
	value = strings.TrimSuffix(value, " FLC")
	valueF64, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	amount, err := chainutil.NewAmount(valueF64)
	if err != nil {
		return err
	}
	a.Amount = amount
	return nil
}
