# Changelog

## [0.2.3]

### Fixed

- **GO-2026-6443**, a remotely triggerable server panic in
  `google.golang.org/grpc` (missing `:authority`/`Host` header), was reachable
  here. grpc is pinned to **v1.83.2**, which the advisory does not cover.

  Earlier notes in this org described this finding as unfixable, because the
  only fix the advisory lists is an unreleased v1.85.0 development build. That
  was wrong: the affected ranges are `[0, 1.82.2)`, `[1.83.0, 1.83.2)` and
  `[1.84.0-dev, 1.85.0-dev...)`, so **v1.83.2 is not affected**. 0.2.2 required
  v1.84.0, which is.

  `govulncheck ./...` now reports no reachable vulnerabilities at all.

## [0.2.2]

### Fixed

- `walletd` reported 0.1.6-beta, well behind the 0.2.x releases it was shipped
  as, because its version came from hand-edited constants that were never
  bumped. It is now taken from the release tag at build time, so there is
  nothing left to drift.

### Changed

- Built with Go 1.26.8, up from 1.26.5. That closes four reachable stdlib
  vulnerabilities reported by govulncheck -- GO-2026-6218 (net/url),
  GO-2026-6090 (crypto/tls), GO-2026-5972 (encoding/asn1) and GO-2026-5026
  (net/http) -- all fixed in 1.26.6. (#3)
- Updated `go-flokicoin` to
  [v0.26.2](https://github.com/flokiorg/go-flokicoin/releases/tag/v0.26.2) from
  v0.25.13-alpha, and `flokicoin-neutrino` to
  [v0.17.2](https://github.com/flokiorg/flokicoin-neutrino/releases/tag/v0.17.2)
  from v0.17.0-beta.
- Updated `google.golang.org/grpc` from v1.79.3 to v1.84.0, closing
  GO-2026-6348 and GO-2026-6061.

### Known issue

- `GO-2026-6443`, a server panic in `google.golang.org/grpc` reachable from
  `startRPCServers` via missing `:authority` or `Host` headers, is **not fixed
  in any stable grpc release** -- upstream's fix currently exists only in a
  v1.85.0 development build. Pinning a pre-release dependency into a release is
  the worse trade, so this is accepted and monitored rather than forced.

## [0.2.1-beta]

walletd v0.2.1-beta adds CI and fixes a real standalone build break plus several `go vet` findings.

### Fixes

- **Standalone build break**: `flokicoin-neutrino` was pinned to `v0.16.3-beta`, which predates the context-aware `ChainService.Start(ctx context.Context) error` signature that walletd's own `NeutrinoChainService` interface already expects. `chain/chainservice.go` and `chain/neutrino.go` failed to compile against the pinned version outside the org's shared `go.work`. Bumped to `v0.17.0-beta`, which has the matching signature.
- **`chain/electrum.go`**: `pingPongHandler`'s `WithTimeout` call is inside a `for`/`select` loop, so a deferred cancel wouldn't run until the goroutine itself exited (accumulating pending cancels). Now calls `cancel()` explicitly after each use.
- **`walletmgr/service.go`**: `RelayFee`/`EstimateFee` discarded `context.WithTimeout`'s cancel function.
- **`chain/lokid_conn.go`**: a log call used `%w` (an `fmt.Errorf`-only wrapping verb) outside of error construction; switched to `%v`.
- **`chain/mempool.go`**: a non-formatting `log.Error` call was passed a format string and an argument; switched to `log.Errorf`.

### CI

- Added `.github/workflows/ci.yaml`: runs `go build`, `go vet`, and `go test` on push to `main` and on pull requests.

Commit range: `0.2.0-beta..0.2.1-beta` (4 commits + 1 merge).

## [0.2.0-beta]

walletd v0.2.0-beta ports the upstream btcwallet sync and fixes several real correctness bugs surfaced along the way.

### API Change

- `chain.Interface.Start` now takes a `context.Context` argument. All in-tree backends (Electrum, lokid RPC, Neutrino) and their mocks were updated to match.

### Fixes

- **`GetTransaction` confirmations**: previously returned the raw block height as the confirmation count and always populated `Timestamp`, even for unconfirmed transactions. Now only populated for confirmed transactions, with `Confirmations` computed against the current best block.
- **`FetchOutpointInfo` ownership check**: now returns `ErrNotMine` when the outpoint has no matching credit record, instead of assuming any outpoint on a known transaction belongs to the wallet.
- **Duplicate UTXO guard**: `txToOutputs` and `txFee` now reject a caller-selected UTXO set containing duplicate entries instead of silently double-counting an input.
- **Nil-result guard**: `signRawTransaction` no longer panics with a nil-pointer dereference when the referenced UTXO has already been spent.
- **Imported-account default info**: the imported-address scoped key manager was missing its default account info on watch-only wallets; it's now created consistently.
- Corrected ZMQ listener log calls that were passing the remote address as a bare `Info` argument instead of interpolating it.

### Internal

- Renamed `confirmed`/`confirms` to `hasMinConfs`/`calcConf` for clarity (same behavior), matching upstream btcwallet naming.
- `go.mod` tidy: `lnd/fn/v2` moved from indirect to direct now that `createtx.go` imports it directly.

### Dependency Security

- Bumped `golang.org/x/crypto` from `v0.45.0` to `v0.52.0` (7 `ssh`/`ssh/agent` advisories; not directly imported here either — only `pbkdf2`/`scrypt`/etc. are used).
- Bumped `google.golang.org/grpc` from `v1.76.0` to `v1.79.3`, closing GHSA-p77j-4mvh-x3m3 (CVSS 9.1, gRPC `:path` authorization-bypass). walletd's RPC server doesn't run a path-based authorization interceptor today, so this wasn't exploitable here, but it's the transport walletd's wallet RPC runs on.

Commit range: `0.1.8-beta..0.2.0-beta` (10 commits).

## [0.1.8-beta]

### Dependency Updates

- **go-flokicoin**: Updated to `v0.25.13-alpha` for MuSig2 support and TestNet4 port fixes.
- Routine `go mod tidy` cleanup.

## [0.1.7-beta]

### Bug Fixes

#### RPC Port Corrections

Corrected hardcoded Bitcoin RPC port numbers in help text to their Flokicoin equivalents:

| Network | Before | After |
|---|---|---|
| MainNet | `8332` | `15213` |
| TestNet | `18332` | `35213` |
| SimNet | `18554` | `45213` |
| RegTest | `18332` | `25213` |

### Dependency Updates

- Routine `go mod tidy` cleanup.

## [0.1.6-beta]

- Updated chain error types and standardized backend names
- Added new wallet error types and introduced a wallet interface with accessor methods

## [0.1.5-beta]

- Swapped the chain backend to lokid and removed the legacy flokicoind client.
- Aligned RPC auth/config names and docs with the lokid wording.
- Bumped go-flokicoin and flokicoin-neutrino to the latest betas.

## [0.1.4-beta]

- This is a **pre-release** for testing and feedback.
- Developers and early adopters are encouraged to **report issues**.

## [0.1.3-beta]

### Changes

#### Dependencies
- Updated `flokicoin-neutrino` → **v0.16.2-beta**
- Updated `go-flokicoin` → **v0.25.7-beta**

#### Core
- Bumped `VERSION` from **0.1.2-alpha** → **0.1.3-beta**

### Notes
- Repo aligned with latest upstream library versions
- Backward compatibility should be preserved

## [0.1.2-alpha]

#### Changed
- Upgraded dependencies

## [0.1.1-alpha]

- This is a **pre-release** for testing and feedback.
- Developers and early adopters are encouraged to **report issues**.

## [0.1.0-beta]

- This is a **pre-release** for testing and feedback.
- Developers and early adopters are encouraged to **report issues**.
