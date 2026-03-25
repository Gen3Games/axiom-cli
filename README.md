# Axiom CLI

Axiom CLI is a standalone Go command-line client for XRPL EVM users who want to manage wallets locally, discover markets, place predictions, claim winnings, and use Axiom funding flows without relying on a browser session.

## What it does

- Creates and imports XRPL EVM wallets
- Optionally creates and imports native XRPL wallets for bridge submissions
- Registers a wallet with the Axiom backend to obtain a destination tag
- Reads market, profile, funding, and unclaimed-winnings data from the Axiom CLI API
- Places on-chain predictions on XRPL EVM
- Signs and submits hosted CLOB orders for grouped `AxiomCTFMarket` logical markets
- Inspects hosted CLOB books, orders, fills, balances, and approvals
- Claims winnings from single markets or in batches

The CLI intentionally splits its work across two data planes:

- Backend API calls for market discovery, profile reads, funding metadata, and registration
- Direct RPC calls for signing, funding, predictions, and claims

That split avoids depending on public RPC infrastructure for historical and indexed application data.

## Security model

The CLI prefers the OS keychain for secret storage.

- EVM private keys are stored in the OS keychain when available
- XRPL seeds are stored in the OS keychain when available
- When the OS keychain is unavailable, the CLI falls back to an encrypted local secret store
- Non-secret metadata is stored in a local config file

The fallback store is encrypted at rest and written under the user config directory. The passphrase is never written to disk.

For headless or CI environments, set:

- `AXIOM_CLI_SECRET_STORE=file`
- `AXIOM_CLI_SECRET_PASSPHRASE=<strong passphrase>`

The local config stores:

- API base URL
- XRPL EVM RPC URL
- XRPL RPC URL
- Local device identifier used for request hardening and rate limiting
- Active profile name
- Non-secret wallet metadata such as addresses and destination tags

The CLI does not store EVM private keys or XRPL seeds in plaintext config.

## Registration model

`axiom auth register` signs a short-lived, device-bound message with the local EVM wallet. The backend verifies the signature, applies rate limits, and either creates or refreshes the wallet's CLI profile before returning the destination tag used for funding flows.

## Requirements

- Go 1.24+
- Access to an Axiom CLI API deployment
- Access to XRPL EVM RPC and XRPL JSON-RPC endpoints

## Build

From the repository root:

```bash
go mod tidy
go build -o axiom ./cmd/axiom
```

Run locally:

```bash
go run ./cmd/axiom --help
```

## Configuration

By default, the CLI stores config in the user config directory under `axiom-cli/config.json`.

Default values:

- API base URL: `https://axiomprotocol.io/api/cli`
- XRPL EVM RPC URL: `https://rpc.xrplevm.org`
- XRPL RPC URL: `https://s1.ripple.com:51234`
- Active profile: `default`

Current production API:

- `https://axiomprotocol.io/api/cli`

You can override configuration per command with:

- `--api-url`
- `--rpc-url`
- `--xrpl-rpc-url`
- `--profile`
- `--json`

Persist configuration with:

```bash
axiom config set --api-url https://axiomprotocol.io/api/cli
axiom config set --rpc-url https://rpc.xrplevm.org
axiom config set --xrpl-rpc-url https://s1.ripple.com:51234
```

Inspect current config with:

```bash
axiom config show
```

## Quick start

Create a local XRPL EVM wallet:

```bash
axiom wallet create
```

Manage multiple local accounts:

```bash
axiom wallet import --account trader-one --private-key 0x...
axiom wallet import --account trader-two --private-key 0x...
axiom wallet accounts list
axiom wallet accounts use trader-two
axiom --account trader-one wallet balance
```

Register it and get a destination tag:

```bash
axiom auth register
axiom auth register --ref-code friend-code
```

Inspect wallet details and balances:

```bash
axiom wallet show
axiom wallet balance
axiom wallet balance --evm
axiom wallet balance --xrpl
```

Read funding instructions:

```bash
axiom funding info
axiom funding bridge
axiom funding bridge-submit --amount 25
```

Browse markets:

```bash
axiom markets list
axiom markets list --status open
axiom markets list --status resolved
axiom markets list --spot-prices
axiom markets list --my-positions
axiom markets get <market-id-or-address>
```

Preview and place a prediction:

```bash
axiom predict quote <market-id-or-address> --label "Yes" --amount 10
axiom predict quote xrp-hourly --label "Higher" --amount 10 --instance-date 2026-03-11
axiom predict buy <market-id-or-address> --label "Yes" --amount 10
axiom predict buy <market-id-or-address> --label "Yes" --amount 10 --dry-run
```

Trade a hosted CLOB market end to end:

```bash
axiom markets get clob-1
axiom clob wallet status clob-1
axiom clob wallet approve clob-1 --wait
axiom clob order place clob-1 --label "Yes" --side buy --type limit --price 52.5 --quantity 10
axiom clob orders list --mine --active-only
axiom clob fills list --mine
axiom claim market clob-1
```

Run a smoke test against the hosted CLOB stack with imported CLI accounts:

```bash
axiom clob smoke
axiom clob smoke first-city-to-ban-private-cars-2032-1773930455346-1773930455346 --label "Paris"
axiom clob smoke --secondary-account trader-two --live
axiom clob smoke --live --auto-approve --wait
```

`axiom clob smoke` uses the active imported CLI account for signing and can inspect a second imported account via `--secondary-account`. It checks hosted projection reads plus on-chain readiness, builds a signed order locally, and in `--live` mode submits a low-impact order then cleans it up with a cancel unless `--keep-order` is set.

Useful CLOB variants:

```bash
axiom clob order place clob-1 --label "Yes" --displayed-side no --side buy --type limit --price 18 --quantity 25
axiom clob order place clob-1 --label "Yes" --side sell --type market --quantity 5
axiom clob order place clob-1 --label "Yes" --side buy --type ioc --price 51 --quantity 20 --dry-run
axiom clob book depth --market clob-1 --outcome 0
axiom clob order get --order-id <order-id>
axiom clob fills get --fill-id <fill-id>
```

Review profile activity:

```bash
axiom profile show
axiom profile update --display-name agent-zero
axiom profile update --avatar-url https://example.com/avatar.png
axiom profile positions
axiom profile positions --status open
axiom profile unclaimed
```

Track rewards and claim reward payouts:

```bash
axiom rewards show
axiom rewards claim daily
axiom rewards claim weekly
axiom rewards claim weekly 77
axiom rewards claim epoch
axiom rewards claim epoch 12
```

Claim winnings:

```bash
axiom claim market <market-id-or-address>
axiom claim market xrp-hourly --instance-date 2026-03-11
axiom claim batch
```

For grouped CLOB markets, `axiom claim market <market-id-or-address>` now inspects all bound binary `AxiomCTFMarket` contracts under the logical market, detects owned YES and NO conditional-token positions for the active wallet, and calls `redeemWithFees` only on the contracts that hold redeemable balances.

For recurring daily and hourly markets, pass `--instance-date` as `YYYY-MM-DD`.

## Public HTTP contract

The CLI depends on a backend that exposes a stable HTTP interface for configuration, registration, market discovery, profile reads, and funding metadata. The public contract for those endpoints is documented in `docs/http-api.md`.

The CLI no longer includes support for Vercel deployment-bypass secrets. Production CLI traffic should use `https://axiomprotocol.io/api/cli`, and alternate deployments should expose the same CLI API contract at their own reachable base URL.

## Command groups

- `axiom config`: Show and update persisted CLI configuration
- `axiom wallet`: Create, import, inspect, balance-check, and reset local wallets
- `axiom auth`: Register the active wallet with the backend
- `axiom auth register --ref-code <code>`: Optionally apply a referral code or referrer wallet address during CLI registration
- `axiom markets`: List markets and fetch market details
- `axiom markets list --my-positions`: Filter the list to markets where the active wallet has open positions
- `axiom profile`: Read profile summary, update display metadata, inspect positions, and inspect unclaimed winnings
- `axiom rewards`: Track daily tasks, streak tickets, estimated epoch payouts, and claim daily, weekly, and epoch rewards
- `axiom funding`: Inspect funding instructions, send direct XRP on XRPL EVM, preview XRPL bridge funding, or bridge directly from a stored XRPL wallet
- `axiom predict`: Quote and buy market positions, including `predict buy --dry-run` for non-committing simulations
- `axiom clob`: Inspect hosted order books, check wallet readiness, approve exchange access, place signed orders, inspect fills, and fetch hosted order/fill records
- `axiom claim`: Claim from one market or batch-claim unclaimed winnings

Use `--json` on any command when you want machine-readable output for scripts or automation.

Notable JSON conventions:

- `profile positions` returns an object with `items` and `total`
- `profile show` includes the wallet's current `referralCode` when one is assigned
- `markets list` includes all open markets by default; add `--spot-prices` to include `currentSpotPrices`
- `markets get` includes per-outcome pool breakdown and the market-level time-bonus configuration when available
- `markets get` includes grouped CTF bindings for `AxiomCTFMarket` logical markets when present
- `rewards show` includes the current `referralCode`, daily-task progress, weekly chest tickets, and claimable epoch reward proofs
- `claim batch` includes `claimedMarkets`, `totalClaimedPayoutUsd`, and `totalClaimedPnlUsd`
- `clob wallet status` includes collateral balance/allowance, ERC-1155 approval state, and YES/NO token balances per grouped CTF binding
- `clob order place --dry-run` returns the locally signed order preview without submitting it to the eventstore

## Funding modes

The CLI supports two funding paths:

- Direct XRPL EVM funding from the local EVM wallet
- XRPL relay funding using a deposit wallet and destination tag

For relay funding, `axiom funding bridge` can either:

- Print a payment URI and terminal QR code for use with another XRPL wallet app
- Submit the XRPL payment directly if a local XRPL seed is available

If you want the local-XRPL-wallet path to appear as its own funding option, use `axiom funding bridge-submit --amount 25` after `axiom wallet xrpl-create` or `axiom wallet xrpl-import`.

## Development notes

- The default API base URL targets production: `https://axiomprotocol.io/api/cli`
- XRPL EVM defaults to `https://rpc.xrplevm.org`
- XRPL defaults to `https://s1.ripple.com:51234`
- For local backend development, override with `axiom config set --api-url http://localhost:3000/api/cli`
- Run `go mod tidy` before release builds to keep the dependency graph minimal
- GitHub release procedure and packaging conventions are documented in `docs/github-releases.md`
