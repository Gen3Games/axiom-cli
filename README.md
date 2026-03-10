# Axiom CLI

Axiom CLI is a standalone Go command-line client for XRPL EVM users who want to manage wallets locally, discover markets, place predictions, claim winnings, and use Axiom funding flows without relying on a browser session.

## What it does

- Creates and imports XRPL EVM wallets
- Optionally creates and imports native XRPL wallets for bridge submissions
- Registers a wallet with the Axiom backend to obtain a destination tag
- Reads market, profile, funding, and unclaimed-winnings data from the Axiom CLI API
- Places on-chain predictions on XRPL EVM
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

- API base URL: `http://localhost:3000/api/cli`
- XRPL EVM RPC URL: `https://rpc.xrplevm.org`
- XRPL RPC URL: `https://s1.ripple.com:51234`
- Active profile: `default`

You can override configuration per command with:

- `--api-url`
- `--rpc-url`
- `--xrpl-rpc-url`
- `--profile`
- `--json`

Persist configuration with:

```bash
axiom config set --api-url https://your-host.example/api/cli
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

Register it and get a destination tag:

```bash
axiom auth register
```

Inspect wallet details and balances:

```bash
axiom wallet show
axiom wallet balance
```

Read funding instructions:

```bash
axiom funding info
```

Browse markets:

```bash
axiom markets list
axiom markets get <market-id-or-address>
```

Preview and place a prediction:

```bash
axiom predict quote <market-id-or-address> --label "Yes" --amount 10
axiom predict buy <market-id-or-address> --label "Yes" --amount 10
```

Review profile activity:

```bash
axiom profile show
axiom profile positions
axiom profile unclaimed
```

Claim winnings:

```bash
axiom claim market <market-id-or-address>
axiom claim batch
```

## Public HTTP contract

The CLI depends on a backend that exposes a stable HTTP interface for configuration, registration, market discovery, profile reads, and funding metadata. The public contract for those endpoints is documented in `docs/http-api.md`.

The CLI no longer includes support for Vercel deployment-bypass secrets. Public deployments should expose the CLI API directly at a reachable base URL such as `https://your-host.example/api/cli`.

## Command groups

- `axiom config`: Show and update persisted CLI configuration
- `axiom wallet`: Create, import, inspect, balance-check, and reset local wallets
- `axiom auth`: Register the active wallet with the backend
- `axiom markets`: List markets and fetch market details
- `axiom profile`: Read profile summary, positions, and unclaimed winnings
- `axiom funding`: Inspect funding instructions, send direct XRP on XRPL EVM, or prepare XRPL bridge funding
- `axiom predict`: Quote and buy market positions
- `axiom claim`: Claim from one market or batch-claim unclaimed winnings

Use `--json` on any command when you want machine-readable output for scripts or automation.

## Funding modes

The CLI supports two funding paths:

- Direct XRPL EVM funding from the local EVM wallet
- XRPL relay funding using a deposit wallet and destination tag

For relay funding, `axiom funding bridge` can either:

- Print a payment URI and terminal QR code for use with another XRPL wallet app
- Submit the XRPL payment directly if a local XRPL seed is available

## Development notes

- The default API base URL is local development friendly by design: `http://localhost:3000/api/cli`
- XRPL EVM defaults to `https://rpc.xrplevm.org`
- XRPL defaults to `https://s1.ripple.com:51234`
- Run `go mod tidy` before release builds to keep the dependency graph minimal
