// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2024 The Flokicoin developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package bdb implements an instance of walletdb that uses boltdb for the backing
datastore.

# Usage

This package is only a driver to the walletdb package and provides the database
type of "bdb". The only parameters the Open and Create functions take are the
database path as a string, an option for the database to not sync its freelist
to disk as a bool, a timeout value for opening the database as a
time.Duration and an option to open the database in read-only mode as a bool:

	db, err := walletdb.Open(
		"bdb", "path/to/database.db", true, 60*time.Second, false,
	)
	if err != nil {
		// Handle error
	}

	db, err := walletdb.Create(
		"bdb", "path/to/database.db", true, 60*time.Second, false,
	)
	if err != nil {
		// Handle error
	}
*/
package bdb
