# Axiom CLI Market-Making Runbook

This document is the dedicated operator runbook for hosted CLOB market making in `axiom-cli`.

It covers the `axiom mm` command group, how it maps to hosted CLOB books, how the CLI stores MM state, what safety checks run before live submission, and the exact manual workflow that was validated against the beta XRP close market.

## Scope

The `axiom mm` surface is intentionally a manual operator layer above the lower-level `axiom clob` commands.

It is designed for:

- selecting one active hosted CLOB logical market
- minting complete-set inventory for that market
- inspecting inventory, one hosted book, open orders, and recent fills
- posting one two-sided quote to one exact hosted book
- bulk-canceling active orders safely, either across a market or on one exact hosted book

It is not currently designed to be a bot runner. There is no long-running `mm run`, no automated repricer, and no built-in strategy engine.

## Command surface

The current market-making commands are:

- `axiom mm market list`
- `axiom mm market use`
- `axiom mm market show`
- `axiom mm market clear`
- `axiom mm mint`
- `axiom mm inventory`
- `axiom mm status`
- `axiom mm book`
- `axiom mm orders`
- `axiom mm fills`
- `axiom mm quote`
- `axiom mm cancel-all`

All `mm` commands use the same hosted CLOB flags as the lower-level `clob` flows:

- `--projection-url`
- `--eventstore-url`
- `--exchange-address`
- `--clob-domain-contract`
- `--clob-chain-id`
- `--outcome-token-address`

Default production values:

- projection: `https://clob.axiomprotocol.io`
- eventstore: `https://clob.axiomprotocol.io/api`
- signing domain contract: `0xa232ACB932b4E745f6ee2aaC1E2707ae0E1055c5`
- chain id: `1440000`
- exchange address: `0xCd9522eeB541ef44722b73a9bf104CED3A2347B2`
- outcome token address: `0x43e3fa6De5D87dd7265053FA55601d1972984edA`

## Architecture

`axiom mm` deliberately spans three surfaces:

- console API for hidden-market-aware market discovery and market detail lookup
- hosted CLOB projection API for book, order, and fill reads
- hosted CLOB eventstore plus XRPL EVM RPC for signed order submission, approvals, and split transactions

This split matters operationally:

- `mm market list` and `mm market use` can see hidden hosted CLOB markets because they read through the console API.
- `mm status`, `mm book`, `mm orders`, `mm fills`, and `mm cancel-all` read current hosted state from projection.
- `mm mint` and live `mm quote` use on-chain transactions and EIP-712 signed hosted order payloads.

## Active market state

The active market-maker market is not stored in `config.json`.

It is stored per CLI profile in:

```text
<user-config-dir>/axiom-cli/mm-state.json
```

The stored fields are:

- `activeMarketId`
- `activeMarketTitle`
- `activeInstanceDate`

This lets you select a market once and then omit the market argument for:

- `mm mint`
- `mm inventory`
- `mm status`
- `mm book`
- `mm orders`
- `mm fills`
- `mm quote`
- `mm cancel-all`

If no market argument is provided and no active MM market is set, the CLI errors and tells you to run `axiom mm market use`.

## Hosted book identity

MM operations target hosted CLOB books, not just logical markets.

Hosted book IDs are built as:

```text
{marketId}-{outcomeIndex}-{tokenSide}
```

Examples:

- `my-market-0-yes`
- `my-market-0-no`
- `my-market-2-yes`

For single-binding binary `yes_no` logical markets:

- the logical displayed `Yes` side resolves to the binding's hosted `yes` book
- the logical displayed `No` side resolves to the binding's hosted `no` book
- if `--displayed-side` is omitted, the CLI infers `yes` for logical outcome index `0` and `no` for logical outcome index `1`

For multi-binding markets:

- each logical outcome has its own binding
- `--displayed-side` defaults to `yes` if omitted
- passing `--displayed-side no` switches to the complementary hosted book for that exact logical outcome

This is why `mm orders`, `mm fills`, `mm book`, and `mm status` always operate on one exact hosted book once the outcome and displayed side are resolved.

## Market discovery and selection

### `axiom mm market list`

Lists hosted CLOB markets for MM workflows.

Supported filters:

- `--search`
- `--status` with default `open`
- `--category`
- `--limit`

Example:

```bash
axiom mm market list --search xrp --status open --limit 20
```

Operational notes:

- This list uses the console API and includes hidden markets.
- The result echoes the current `activeMarket` when one is already selected.
- Long-running fetches show a loading indicator on stderr.

### `axiom mm market use`

Sets the active MM market.

Two usage modes:

```bash
axiom mm market use <market-id-or-address>
```

or interactive selection:

```bash
axiom mm market use --search xrp --status open --limit 20
```

Interactive selection behavior:

- market choices are printed to stderr
- hidden markets are annotated with `[hidden]`
- the prompt is `Select market-maker market`
- the selected market is persisted to `mm-state.json`

Recurring markets:

- use `--instance-date YYYY-MM-DD` when needed
- the chosen instance date is also stored in MM state

### `axiom mm market show`

Shows the currently selected MM market.

### `axiom mm market clear`

Clears the active MM market from `mm-state.json` for the current profile.

## Inventory and minting

### `axiom mm mint`

`mm mint` is the MM-friendly wrapper over the lower-level split flow.

It splits collateral into complete YES and NO sets for one grouped CTF binding.

Example:

```bash
axiom mm mint --label 'Below $1.35' --amount 3.0 --wait
```

Flags:

- `--label` identifies the binding used for the split
- `--amount` is required
- `--wait` waits for the split receipt
- `--dry-run` previews the split without broadcasting
- `--skip-approval` skips the automatic collateral approval to ConditionalTokens
- `--instance-date` selects the recurring market instance

Selection rules:

- if the market has exactly one binding, `--label` can be omitted
- if the market has multiple bindings, `--label` is required

Amount parsing is important:

- if `--amount` contains a decimal point, it is interpreted as XRP and converted to 18-decimal wei
- if `--amount` does not contain a decimal point, it is interpreted as a raw wei integer

Examples:

- `--amount 3.0` means `3 XRP`
- `--amount 3000000000000000000` means the same amount in wei

Live `mm mint` behavior:

- checks collateral balance before splitting
- auto-approves collateral to the ConditionalTokens contract unless `--skip-approval` is set
- waits for the approval receipt before splitting when approval is needed

### `axiom mm inventory`

Summarizes inventory, approvals, balances, and imbalance for the selected MM market.

Example:

```bash
axiom mm inventory
```

Optional flags:

- `--wallet` to inspect a different wallet address
- `--instance-date`

The output includes:

- collateral balance and allowance
- `outcomeApprovalForAll`
- per-binding YES balance
- per-binding NO balance
- per-binding complete-set count
- per-binding imbalance and `inventoryBias`
- total complete sets across the market

Inventory semantics:

- `completeSets` for a binding is `min(yesBalance, noBalance)`
- `inventoryBias` is `yes`, `no`, or `balanced`
- `mergeReady` means the binding has at least one complete set available to merge back to collateral

## Book, order, fill, and status inspection

These commands all support the same targeting pattern:

- optional positional market argument, otherwise the active MM market is used
- `--outcome` or `--label` to select the logical outcome
- `--displayed-side yes|no` to select the exact hosted book when needed

### `axiom mm status`

This is the main one-shot operator view.

Example:

```bash
axiom mm status --label 'Below $1.35' --displayed-side yes
```

It returns:

- `activeMarket`
- wallet address
- logical market metadata
- exact `clobId`
- inventory summary
- hosted book summary
- depth ladder
- active orders on that exact book
- recent fills on that exact book
- `activeOrderCount`
- `recentFillCount`

Useful flags:

- `--wallet`
- `--order-limit`
- `--fill-limit`
- `--instance-date`

### `axiom mm book`

Fetches the hosted book summary and depth for one exact hosted book.

Example:

```bash
axiom mm book --label 'Below $1.35' --displayed-side yes
```

### `axiom mm orders`

Lists active orders for one exact hosted book and wallet.

Example:

```bash
axiom mm orders --label 'Below $1.35' --displayed-side yes
```

Useful flags:

- `--wallet`
- `--limit`

### `axiom mm fills`

Lists recent fills for one exact hosted book and wallet.

Example:

```bash
axiom mm fills --label 'Below $1.35' --displayed-side yes
```

Useful flags:

- `--wallet`
- `--limit`

## Quoting

### `axiom mm quote`

Places a two-sided hosted quote on one exact hosted book.

Example:

```bash
axiom mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3
```

Required flags:

- `--bid-price`
- `--ask-price`
- `--quantity`
- logical outcome selection via `--outcome` or `--label`

Useful flags:

- `--displayed-side yes|no`
- `--expiry` with `1h`, `24h`, `7d`, or `never`
- `--dry-run`
- `--instance-date`

Input rules:

- prices are entered in displayed percent units, for example `45` or `55`
- prices are converted to basis points internally
- valid range is `0.00` through `99.99`
- `--ask-price` must be greater than `--bid-price`
- `--quantity` must be a positive whole number

Live submission behavior:

1. The CLI resolves the exact hosted book.
2. It builds both signed orders locally.
3. It loads wallet status and checks readiness.
4. It auto-submits missing approvals when possible.
5. It submits the bid.
6. It submits the ask.
7. If the ask submission fails after the bid was placed, it attempts to cancel the bid as rollback.

Dry-run behavior:

- does not broadcast anything
- returns the derived signed-order payload details for both sides
- returns `quoteReady`
- returns `blocking` arrays for bid and ask independently

#### Quote readiness checks

Before live submission, the CLI checks the same practical blockers that `clob smoke` uses:

For the bid side:

- sufficient collateral balance
- sufficient collateral allowance to the exchange

For the ask side:

- `ERC1155 setApprovalForAll(true)` on the outcome token for the exchange
- sufficient displayed-side token balance on the selected hosted book side

The CLI also enforces the backend settleability rule locally.

If a quote cannot settle on-chain at the requested quantity and price, the quote is rejected locally before submission.

The error format is explicit, for example:

```text
quote is not ready: bid: order quantity too small for on-chain settlement: quantity 2 at price 4500 bps, minimum is 3 shares
```

#### Live auto-approval

Unlike `clob smoke`, live `mm quote` auto-approves missing prerequisites without requiring a separate `--auto-approve` flag.

It can submit:

- max ERC-20 collateral approval to the exchange for the bid side
- ERC-1155 `setApprovalForAll(true)` for the ask side

`mm quote --dry-run` never broadcasts approvals.

#### Settleability and minimum size

Hosted CLOB orders must be settleable on-chain at the selected quantity and price.

That means the smallest valid size can depend on the quoted price. The CLI computes this locally from the final signed payload, so the minimum can differ between the bid and ask side even for the same quote width.

Operational takeaway:

- do not assume `quantity 1` is always valid
- use `mm quote --dry-run` first when testing a new market shape or spread
- when the dry-run reports a settleability blocker, raise quantity or change price before going live

## Bulk cancellation

### `axiom mm cancel-all`

Cancels active orders for the active MM wallet.

This command can operate in four practical modes:

1. active-market scope, if an active MM market is selected and `--market` is omitted
2. explicit market scope, if `--market` is provided
3. exact hosted-book scope, if market scope is combined with `--label` or `--outcome` plus side selection
4. all-wallet scope across markets, only if no active MM market is selected and `--market` is omitted

Examples:

Cancel only one exact hosted book:

```bash
axiom mm cancel-all --label 'Below $1.35' --displayed-side yes
```

Preview the exact targeted orders without canceling:

```bash
axiom mm cancel-all --label 'Below $1.35' --displayed-side yes --dry-run
```

Flags:

- `--market`
- `--outcome`
- `--label`
- `--token-side`
- `--displayed-side`
- `--reason`
- `--limit`
- `--dry-run`
- `--instance-date`

Important scoping rules:

- `--displayed-side` is an alias for `--token-side`
- if both are provided they must match
- if you pass outcome, label, or side filters without a market, the CLI errors
- if an active MM market is selected, running `axiom mm cancel-all` with no `--market` uses that active market automatically
- all-wallet cancel-across-markets behavior only happens when there is no active MM market and no explicit `--market`
- when scoping to a single-binding binary market with no outcome or side filters, both hosted books are targeted
- when scoping to a multi-binding market with no outcome or side filters, the current implementation targets the `yes` book for each binding
- when scoping to a market with only `--token-side` or `--displayed-side`, that side is targeted across all bindings in the market

Exact-book safety:

- `mm cancel-all` filters open orders by exact `clob_id`
- when you pass `--label` plus `--displayed-side`, only that exact hosted book is targeted
- unrelated books and unrelated markets are left alone

## Recommended operator workflow

This is the intended manual flow for one hosted market:

```bash
axiom mm market list --search xrp
axiom mm market use <market-id>
axiom mm market show

axiom mm inventory
axiom mm status --label 'Below $1.35' --displayed-side yes
axiom mm book --label 'Below $1.35' --displayed-side yes

axiom mm mint --label 'Below $1.35' --amount 3.0 --wait
axiom mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3 --dry-run
axiom mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3

axiom mm orders --label 'Below $1.35' --displayed-side yes
axiom mm fills --label 'Below $1.35' --displayed-side yes

axiom mm cancel-all --label 'Below $1.35' --displayed-side yes
```

Recommended habits:

- select the active market once with `mm market use`
- use single quotes around labels containing `$`
- use `mm quote --dry-run` before first live quoting on a new market or spread
- use `mm status` as the primary all-in-one operator check
- use `mm cancel-all --dry-run` when you want to confirm exact cancellation scope first

## Shell quoting notes

If a label contains `$`, use single quotes so the shell does not interpolate it.

Correct:

```bash
axiom mm status --label 'Below $1.35' --displayed-side yes
```

Incorrect:

```bash
axiom mm status --label "Below $1.35" --displayed-side yes
```

## Troubleshooting

### No active MM market is set

Error example:

```text
no market provided and no active market-maker market is set; run `axiom mm market use`
```

Fix:

```bash
axiom mm market use <market-id>
```

### Multiple bindings found; use `--label`

This happens when a market has multiple grouped outcome bindings and the CLI cannot infer which binding to split for `mm mint`.

Fix:

```bash
axiom mm mint --label 'Above $1.50' --amount 3.0 --wait
```

### Quote is not ready

This usually means one of:

- insufficient collateral balance for the bid
- insufficient collateral allowance for the bid
- missing ERC-1155 approval-for-all for the ask
- insufficient displayed-side token balance for the ask
- order quantity too small for on-chain settlement

First step:

```bash
axiom --json mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3 --dry-run
```

Inspect `bid.blocking` and `ask.blocking` separately.

### Immediate post-cancel reads still show an open order

Projection reads can lag immediately after a cancel. A just-canceled order can briefly still show as open before converging to:

- `status: "cancelled"`
- `remaining: 0`

If needed, re-run the read after a short pause.

### Hosted projection timeout or lag

Under load, hosted projection reads can time out or lag. One observed transient read failure was:

```text
request api: Get "https://clob.axiomprotocol.io/books/beta-test-where-will-xrp-close-on-sunday-may-17-2026-at-21-00-utc-1778403914962/0/depth?token_side=yes": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
```

Operationally, retry the read and avoid assuming the first immediate post-write projection response is final.

## Detailed beta validation

The MM workflow above was validated end to end against the hidden beta market:

```text
beta-test-where-will-xrp-close-on-sunday-may-17-2026-at-21-00-utc-1778403914962
```

The local binary used for validation was `./axiom` built from the local repo.

### Validated market books

The logical outcomes were:

- `Below $1.35`
- `$1.35 to $1.50 inclusive`
- `Above $1.50`

Each outcome was quoted on both displayed sides, for six hosted books total:

- `Below $1.35` yes
- `Below $1.35` no
- `$1.35 to $1.50 inclusive` yes
- `$1.35 to $1.50 inclusive` no
- `Above $1.50` yes
- `Above $1.50` no

### Market selection

```bash
./axiom mm market use beta-test-where-will-xrp-close-on-sunday-may-17-2026-at-21-00-utc-1778403914962
```

### Inventory minting

Three complete sets were minted on each logical outcome:

```bash
./axiom mm mint --label 'Below $1.35' --amount 3.0 --wait
./axiom mm mint --label '$1.35 to $1.50 inclusive' --amount 3.0 --wait
./axiom mm mint --label 'Above $1.50' --amount 3.0 --wait
```

### Minimum settleable-size findings

The tested quote width was:

- bid `45`
- ask `55`

Observed dry-run checks:

```bash
./axiom --json mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 1 --dry-run
./axiom --json mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 2 --dry-run
```

Observed blockers:

- `quantity 1` failed both sides
- at `4500 bps`, the bid required a minimum of `3` shares
- at `5500 bps`, the ask required a minimum of `2` shares
- `quantity 2` still failed the `45` bid side
- `quantity 3` was the smallest passing size for the tested spread

### Full six-book live sweep

All six books were quoted live at `45/55 x 3` and then cleaned up with exact-book `mm cancel-all`.

```bash
./axiom mm quote --label 'Below $1.35' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label 'Below $1.35' --displayed-side yes

./axiom mm quote --label 'Below $1.35' --displayed-side no --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label 'Below $1.35' --displayed-side no

./axiom mm quote --label '$1.35 to $1.50 inclusive' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label '$1.35 to $1.50 inclusive' --displayed-side yes

./axiom mm quote --label '$1.35 to $1.50 inclusive' --displayed-side no --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label '$1.35 to $1.50 inclusive' --displayed-side no

./axiom mm quote --label 'Above $1.50' --displayed-side yes --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label 'Above $1.50' --displayed-side yes

./axiom mm quote --label 'Above $1.50' --displayed-side no --bid-price 45 --ask-price 55 --quantity 3
./axiom mm cancel-all --label 'Above $1.50' --displayed-side no
```

Observed live results:

- all six hosted books accepted live two-sided quotes
- every cleanup canceled only the two targeted orders on the exact book that had just been quoted
- no unrelated beta books were touched

### Final verification

Final verification commands:

```bash
./axiom --json --projection-url 'https://clob.axiomprotocol.io' clob orders list --mine --active-only
./axiom --json mm status --label 'Above $1.50' --displayed-side no
```

Observed final state:

- the only remaining active order globally was the unrelated order `16a758d5-8533-40f1-b17e-c4d9bb97935c`
- that order lived on `smoke-hidden-20260509-neonfix-01-0-yes`
- `mm status` for `Above $1.50` on displayed side `no` returned `activeOrderCount: 0`
- the exact hosted book checked was `beta-test-where-will-xrp-close-on-sunday-may-17-2026-at-21-00-utc-1778403914962-2-no`

## Relationship to lower-level `clob` commands

`axiom mm` is the operator convenience layer. `axiom clob` remains the lower-level primitive layer.

Use `axiom mm` when you want:

- active-market persistence
- higher-level MM workflows
- two-sided quote placement
- exact-book MM status views

Use `axiom clob` when you want:

- low-level hosted book inspection without MM state
- one-off order placement or cancelation primitives
- smoke testing the hosted stack
- split and merge primitives directly

Relevant lower-level commands:

- `axiom clob wallet status`
- `axiom clob wallet approve`
- `axiom clob book depth`
- `axiom clob order place`
- `axiom clob orders list`
- `axiom clob fills list`
- `axiom clob smoke`
- `axiom clob split`
- `axiom clob merge`

## Summary

For the current product scope, `axiom mm` is feature complete as a manual hosted CLOB operator tool.

It supports:

- hidden-market discovery
- active market selection
- complete-set minting
- inventory inspection
- exact-book status, book, order, and fill inspection
- two-sided quote placement with local readiness and settleability checks
- exact-book bulk cancellation

What it does not yet include:

- automated quoting loops
- strategy logic
- inventory-aware repricing
- background daemons
- unattended operations
