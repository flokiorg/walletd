module github.com/flokiorg/walletd

go 1.23.4

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/flokiorg/flokicoin-neutrino v0.0.0-00010101000000-000000000000
	github.com/flokiorg/go-flokicoin v0.23.5-0.20230711222809-7faa9b266231
	github.com/gorilla/websocket v1.5.3
	github.com/jessevdk/go-flags v1.6.1
	github.com/jrick/logrotate v1.1.2
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf
	github.com/lightningnetwork/lnd/clock v1.1.1
	github.com/lightningnetwork/lnd/ticker v1.1.1
	github.com/lightningnetwork/lnd/tlv v1.3.0
	github.com/stretchr/testify v1.10.0
	go.etcd.io/bbolt v1.4.0
	golang.org/x/net v0.37.0
	golang.org/x/sync v0.12.0
	golang.org/x/term v0.30.0
	google.golang.org/grpc v1.71.0
	google.golang.org/protobuf v1.36.5
)

require (
	github.com/aead/siphash v1.0.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.4 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0 // indirect
	github.com/decred/dcrd/lru v1.1.3 // indirect
	github.com/flokiorg/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lightningnetwork/lnd/fn/v2 v2.0.8 // indirect
	github.com/lightningnetwork/lnd/queue v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	golang.org/x/exp v0.0.0-20250210185358-939b2ce775ac // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/btcsuite/btcd v0.24.2 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.1.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0
	golang.org/x/crypto v0.36.0
	golang.org/x/sys v0.31.0 // indirect
)

replace github.com/flokiorg/go-flokicoin => ../go-flokicoin

replace github.com/flokiorg/flokicoin-neutrino => ../flokicoin-neutrino
