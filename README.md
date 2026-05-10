# Axiom CLI

Axiom CLI is a standalone Go command-line client for XRPL EVM users who want to manage wallets locally, discover markets, place predictions, claim winnings, author and operate hosted CLOB markets, and use Axiom funding flows without relying on a browser session.

## What it does

- Creates and imports XRPL EVM wallets
- Optionally creates and imports native XRPL wallets for bridge submissions
- Registers a wallet with the Axiom backend to obtain a destination tag
- Reads market, profile, funding, and unclaimed-winnings data from the Axiom CLI API
- Places on-chain predictions on XRPL EVM
- Signs and submits hosted CLOB orders for grouped `AxiomCTFMarket` logical markets
- Creates, registers, resolves, and updates logical hosted CLOB markets
- Deploys and resolves low-level binary `AxiomCTFMarket` contracts
- Inspects hosted CLOB books, orders, fills, balances, approvals, and split or merge readiness
- Splits collateral into YES and NO conditional tokens and merges complete sets back into collateral
- Claims winnings from single markets or in batches

The CLI intentionally splits its work across indexed API surfaces and direct RPC calls:

- App, console, and hosted CLOB APIs for market discovery, profile reads, metadata, registration, and hosted orderbook state
- Direct RPC calls for signing, funding, predictions, claims, binary market operations, and CTF split or merge transactions

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

- App API base URL
- Console API base URL
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
- Access to an Axiom CLI app API deployment
- For hosted CLOB commands, access to an Axiom console API deployment plus hosted CLOB projection and eventstore deployments
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

Persisted defaults:

- App API base URL: `https://axiomprotocol.io/api/cli`
- Console API base URL: `https://console.axiomprotocol.io/api/cli`
- XRPL EVM RPC URL: `https://rpc.xrplevm.org`
- XRPL RPC URL: `https://s1.ripple.com:51234`
- Active profile: `default`

You can override configuration per command with:

- `--api-url`
- `--console-api-url`
- `--rpc-url`
- `--xrpl-rpc-url`
- `--profile`
- `--json`

Persist configuration with:

```bash
axiom config set --api-url https://axiomprotocol.io/api/cli
axiom config set --console-api-url https://console.axiomprotocol.io/api/cli
axiom config set --rpc-url https://rpc.xrplevm.org
axiom config set --xrpl-rpc-url https://s1.ripple.com:51234
```

Inspect current config with:

```bash
axiom config show
```

Hosted CLOB endpoints are command-scoped rather than persisted in `config.json`:

- `--projection-url`: `https://clob.axiomprotocol.io`
- `--eventstore-url`: `https://clob.axiomprotocol.io/api`
- `--clob-domain-contract`: `0xa232ACB932b4E745f6ee2aaC1E2707ae0E1055c5`
- `--clob-chain-id`: `1440000`
- `--exchange-address`: `0xCd9522eeB541ef44722b73a9bf104CED3A2347B2`
- `--outcome-token-address`: `0x43e3fa6De5D87dd7265053FA55601d1972984edA`
- `--factory-address`: optional; when omitted the CLI loads the canonical `MarketFactory` from the console API

The hosted CLOB flags can also be supplied with:

- `AXIOM_CLOB_PROJECTION_URL` or `CLOB_PROJECTION_URL`
- `AXIOM_CLOB_EVENTSTORE_URL` or `CLOB_EVENTSTORE_URL`
- `AXIOM_CLOB_DOMAIN_CONTRACT`
- `AXIOM_CLOB_CHAIN_ID`

## Hosted CLOB model

The CLI intentionally splits hosted CLOB flows across three HTTP surfaces:

- App API: market discovery, profile reads, funding metadata, and general CLI data
- Console API: canonical contract addresses, signed metadata uploads, and logical CLOB lifecycle routes
- Hosted CLOB APIs: projection reads plus eventstore writes for books, orders, fills, and cancellations

Logical `clob create` is hybrid by design:

- upload per-binding metadata through the console API
- launch grouped binary markets on-chain via `AxiomCTFMarketLauncher`
- register the logical market and hosted books off-chain through the console API

Hosted market identity is side-aware:

- A `yes_no` logical market binds one binary `AxiomCTFMarket`; displayed `No` is the same binding with `token_side=no`
- A `multiple_choice` logical market binds one binary `AxiomCTFMarket` per displayed outcome; each binding has hosted `yes` and `no` books
- Hosted book and order identity uses logical `marketId` plus `outcome` plus `token_side`; hosted `clob_id` values follow `{marketId}-{outcome}-yes|no`

Hosted order, cancel, and `CreateBook` signatures use this EIP-712 domain:

- Name: `Axiom CLOB`
- Version: `1`
- Chain ID: `1440000`
- Verifying contract: `0xa232ACB932b4E745f6ee2aaC1E2707ae0E1055c5`

The hosted signing domain is separate from the on-chain exchange contract used for approvals and settlement prep. Do not point `--clob-domain-contract` at `0xCd9522eeB541ef44722b73a9bf104CED3A2347B2`.

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

## Hosted CLOB workflows

Prepare a wallet for hosted trading:

```bash
axiom markets get clob-1
axiom clob wallet status clob-1
axiom clob wallet approve clob-1 --wait
```

Inspect hosted books and history:

```bash
axiom clob book depth --market clob-1 --label "Yes"
axiom clob book depth --market clob-1 --label "No"
axiom clob orders list --market clob-1 --label "Yes" --active-only
axiom clob fills list --market clob-1 --label "Yes"
axiom clob orders list --mine --active-only
axiom clob fills list --mine
```

Place, inspect, and cancel orders:

```bash
axiom clob order place clob-1 --label "Yes" --side buy --type limit --price 52.5 --quantity 10
axiom clob order place clob-1 --label "Warriors" --displayed-side no --side buy --type limit --price 18 --quantity 25
axiom clob order place clob-1 --label "Yes" --side sell --type market --quantity 5
axiom clob order place clob-1 --label "Yes" --side buy --type ioc --price 51 --quantity 20 --dry-run
axiom clob order get --order-id <order-id>
axiom clob order cancel --order-id <order-id> --market clob-1 --label "Yes"
axiom clob fills get --fill-id <fill-id>
```

Read and write commands that target hosted books are token-side aware:

- `clob order place` uses `--displayed-side`
- `clob book depth`, `clob orders list`, `clob fills list`, and `clob order cancel` use `--token-side`
- For single-binding `yes_no` logical markets, selecting the displayed `No` outcome infers the hosted `no` book automatically
- For multi-binding markets, omitting `--token-side` defaults to the `yes` book for the selected displayed outcome

Manage complete sets:

```bash
axiom clob split-status clob-1 --label "Yes"
axiom clob split clob-1 --label "Yes" --amount 5 --wait
axiom clob merge clob-1 --label "Yes" --max --wait
axiom claim market clob-1
```

`axiom claim market` on grouped CLOB markets inspects all bound binary contracts under the logical market and only calls `redeemWithFees` on contracts that actually hold redeemable YES or NO balances for the active wallet.

Create a logical hidden `yes_no` market:

```bash
axiom clob logical create \
  --hidden \
  --market-type yes_no \
  --name "Will XRP close above $3.00 on 2026-05-10?" \
  --headline "XRP daily close" \
  --description "Resolves Yes if XRP closes strictly above $3.00 on 2026-05-10 UTC." \
  --category crypto \
  --resolution-criteria "Resolve using the official XRPL EVM daily close source." \
  --starts-at 2026-05-10T00:00:00Z \
  --ends-at 2026-05-11T00:00:00Z \
  --image ipfs://xrp-daily-pfp \
  --tag xrp \
  --tag daily
```

Create a logical hidden `multiple_choice` market with additive `--outcomes-json` overrides:

```bash
axiom clob logical create \
  --hidden \
  --market-type multiple_choice \
  --name "Who wins the series?" \
  --description "Resolves to the team that wins the best-of-seven series." \
  --category sports \
  --resolution-criteria "Resolve using the official final series result." \
  --starts-at 2026-05-10T00:00:00Z \
  --ends-at 2026-05-20T00:00:00Z \
  --image ipfs://series-pfp \
  --outcomes-json '[
    {"key":"warriors","label":"Warriors","metadataUri":"ipfs://warriors-meta","questionId":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
    {"key":"lakers","label":"Lakers","questionId":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
    {"key":"celtics","label":"Celtics"}
  ]'
```

`--outcomes-json` is additive and preserves the simple flags for existing scripts. Each outcome object can provide:

- `key`
- `label`
- `description`
- `metadataUri`
- `questionId`

When `metadataUri` is supplied for a launched outcome, the CLI skips metadata upload for that binding and passes the URI through unchanged.

Register existing binary markets as one logical market:

```bash
axiom clob logical register \
  --hidden \
  --market-id xrp-close-2026-05-10 \
  --market-type yes_no \
  --name "Will XRP close above $3.00 on 2026-05-10?" \
  --description "Resolves Yes if XRP closes strictly above $3.00 on 2026-05-10 UTC." \
  --category crypto \
  --resolution-criteria "Resolve using the official XRPL EVM daily close source." \
  --starts-at 2026-05-10T00:00:00Z \
  --ends-at 2026-05-11T00:00:00Z \
  --address 0x1234567890abcdef1234567890abcdef12345678
```

Resolve and update logical markets:

```bash
axiom clob logical resolve --market xrp-close-2026-05-10 --outcome 1 --wait

axiom clob logical update \
  --market xrp-close-2026-05-10 \
  --name "Will XRP close above $3.00 on 2026-05-10? Updated" \
  --headline "Updated headline" \
  --description "Updated logical market copy." \
  --category crypto \
  --image ipfs://xrp-daily-pfp-v2 \
  --tag xrp \
  --tag updated
```

`clob logical resolve` performs the lifecycle in this order:

- Resolve the grouped binary markets on-chain
- Close the hosted books for each binding and token side
- Mark the logical market row resolved in the console

`clob logical update` currently supports top-level logical metadata only:

- `name`
- `headline`
- `description`
- `category`
- `image`
- `tag`

The update route is creator-signed and verifies bootstrap signer access plus logical `ownerAddress` before persisting changes.

`--hidden` and `--visible` are mutually exclusive on logical create and register. `--image` sets the logical market PFP URL and is also included in uploaded binary metadata.

Use the low-level binary primitives when you only want raw on-chain contracts:

```bash
axiom clob market create \
  --name "Will XRP close above $3.00?" \
  --description "Binary CTF market." \
  --category crypto \
  --resolution-criteria "Resolve from the official close source." \
  --question-id 0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
  --trading-open 1778361600 \
  --trading-close 1778448000

axiom clob market resolve \
  --market 0x1234567890abcdef1234567890abcdef12345678 \
  --payouts 1,0 \
  --wait
```

`clob market create` and `clob market resolve` intentionally stay low-level. Use `clob logical register` when you want those binary contracts surfaced as one logical hosted market.

`axiom clob smoke` remains the fastest end-to-end validation path for imported CLI accounts:

```bash
axiom clob smoke
axiom clob smoke first-city-to-ban-private-cars-2032-1773930455346-1773930455346 --label "Paris"
axiom clob smoke --secondary-account trader-two --live
axiom clob smoke --live --auto-approve --wait
```

Immediately after a live cancel, the first `clob order get` can briefly return the pre-cancel projection row. Subsequent reads converge to `status: "cancelled"` and `remaining: 0`.

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

The CLI depends on three stable HTTP surfaces:

- the app API for configuration, registration, market discovery, profile reads, funding metadata, and rewards
- the console API for canonical addresses, signed metadata uploads, and logical CLOB lifecycle routes
- the hosted CLOB projection and eventstore APIs for books, orders, fills, and cancellations

The current contract for all three surfaces is documented in `docs/http-api.md`.

The CLI no longer includes support for Vercel deployment-bypass secrets. Production CLI traffic should use `https://axiomprotocol.io/api/cli`, and alternate deployments should expose the same CLI API contract at their own reachable base URL.

## Command groups

- `axiom config`: Show and update persisted CLI configuration
- `axiom wallet`: Create, import, inspect, balance-check, reset, and switch local wallets
- `axiom auth register --ref-code <code>`: Register the active wallet with the backend and optionally apply a referral code or referrer wallet address
- `axiom markets`: List markets and fetch market details
- `axiom profile`: Read profile summary, update display metadata, inspect positions, and inspect unclaimed winnings
- `axiom rewards`: Track daily tasks, streak tickets, estimated epoch payouts, and claim daily, weekly, and epoch rewards
- `axiom funding`: Inspect funding instructions, send direct XRP on XRPL EVM, preview XRPL bridge funding, or bridge directly from a stored XRPL wallet
- `axiom predict`: Quote and buy TieredParimutuel market positions, including `predict buy --dry-run`
- `axiom claim`: Claim from one market or batch-claim unclaimed winnings
- `axiom clob logical`: Atomically create, register, resolve, and update logical `AxiomCTFMarket` hosted markets
- `axiom clob market`: Deploy and resolve low-level binary `AxiomCTFMarket` contracts
- `axiom clob wallet`: Inspect hosted CLOB readiness and submit approvals
- `axiom clob book`: Inspect token-side hosted books and depth
- `axiom clob order`: Place, inspect, and cancel hosted orders
- `axiom clob orders`: List hosted order history
- `axiom clob fills`: List or fetch hosted fills
- `axiom clob smoke`: Run dry-run or live smoke tests against the hosted stack
- `axiom clob split`, `merge`, and `split-status`: Tokenize collateral into complete sets, merge matched sets back to collateral, and inspect readiness

Use `--json` on any command when you want machine-readable output for scripts or automation.

Notable JSON conventions:

- `profile positions` returns an object with `items` and `total`
- `profile show` includes the wallet's current `referralCode` when one is assigned
- `markets list` includes all open markets by default; add `--spot-prices` to include `currentSpotPrices`
- `markets list` and `markets get` include `marketImplementation` when the backend exposes it
- `markets get` includes per-outcome pool breakdown, market-level time-bonus configuration, `imageUrl`, `logicalMarketAddresses`, and grouped `ctfOutcomeMarkets` bindings when available
- `rewards show` includes the current `referralCode`, daily-task progress, weekly chest tickets, and claimable epoch reward proofs
- `claim batch` includes `claimedMarkets`, `totalClaimedPayoutUsd`, and `totalClaimedPnlUsd`
- `clob wallet status` includes collateral balance/allowance, ERC-1155 approval state, and YES/NO token balances per grouped CTF binding
- `clob book depth`, `clob orders list`, and `clob fills list` echo the resolved logical outcome plus `tokenSide` when market context is provided
- `clob order place --dry-run` returns the locally signed order preview, including token addresses, token ID, maker and taker amounts, expiration, and nonce
- `clob logical create --dry-run` returns launch parameters and logical registration metadata without submitting transactions
- `clob logical resolve --dry-run` returns the per-binding payout plan plus the expected hosted book closure count
- `clob logical update --dry-run` returns the exact signed logical-update payload preview

## Funding modes

The CLI supports two funding paths:

- Direct XRPL EVM funding from the local EVM wallet
- XRPL relay funding using a deposit wallet and destination tag

For relay funding, `axiom funding bridge` can either:

- Print a payment URI and terminal QR code for use with another XRPL wallet app
- Submit the XRPL payment directly if a local XRPL seed is available

If you want the local-XRPL-wallet path to appear as its own funding option, use `axiom funding bridge-submit --amount 25` after `axiom wallet xrpl-create` or `axiom wallet xrpl-import`.

## Development notes

- The default app API base URL targets production: `https://axiomprotocol.io/api/cli`
- The default console API base URL targets production: `https://console.axiomprotocol.io/api/cli`
- XRPL EVM defaults to `https://rpc.xrplevm.org`
- XRPL defaults to `https://s1.ripple.com:51234`
- Hosted CLOB reads default to `https://clob.axiomprotocol.io`
- Hosted CLOB writes default to `https://clob.axiomprotocol.io/api`
- For local backend development, override with `axiom config set --api-url http://localhost:<app-port>/api/cli`
- For local console development, override with `axiom config set --console-api-url http://localhost:<console-port>/api/cli`
- Use the hosted CLOB flags or environment variables when you need non-default projection, eventstore, or signing-domain settings
- Run `go mod tidy` before release builds to keep the dependency graph minimal
- GitHub release procedure and packaging conventions are documented in `docs/github-releases.md`
