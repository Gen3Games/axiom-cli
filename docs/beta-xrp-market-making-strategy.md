# Beta XRP Market-Making Strategy Notes

This document explains the logic behind the hosted CLOB market-making setup used for the hidden beta market `beta-test-where-will-xrp-close-on-sunday-may-17-2026-at-21-00-utc-1778403914962`.

It covers:

- how complete-set inventory works
- why inventory and free XRP are both required
- how the quoted prices were chosen
- how the strategy can make money
- the main risks and operational constraints

## Market Structure

This logical market has three mutually exclusive outcomes:

- `Below $1.35`
- `$1.35 to $1.50 inclusive`
- `Above $1.50`

Operationally, each outcome is its own grouped binary CTF binding with two hosted books:

- displayed side `yes`
- displayed side `no`

That means the logical three-outcome market is quoted across six hosted books total.

For one binary binding:

- `1 YES + 1 NO = 1 XRP` at resolution

That paired `YES + NO` position is the complete set. A complete set is inventory-neutral because the combined payout is fixed at `1 XRP` regardless of which side wins.

## Why We Mint Complete Sets

`axiom mm mint` splits collateral into complete-set inventory for one outcome binding.

If we mint `50 XRP` on one outcome, we receive:

- `50 YES`
- `50 NO`

We minted `50` complete sets on each of the three outcomes, so the wallet now holds:

- `50 YES` and `50 NO` for `Below $1.35`
- `50 YES` and `50 NO` for `$1.35 to $1.50 inclusive`
- `50 YES` and `50 NO` for `Above $1.50`

The purpose of that inventory is to support ask orders.

## Why Free XRP Still Matters

`axiom mm quote` posts two-sided liquidity on one exact hosted book:

- a bid, which requires free XRP collateral
- an ask, which requires token inventory on that displayed side

So there are two separate resources:

- complete-set inventory backs the sell side
- unsplit XRP backs the buy side

This is why minting too aggressively can become a problem. If too much XRP is split into complete sets, there is not enough unsplit XRP left to support the bid side of the ladder.

## Important Hosted CLOB Behavior

In this hosted CLOB setup, resting orders do not reduce the wallet's visible on-chain balances when they are posted.

Instead, the operator still needs enough collateral and token inventory for each order to be settleable if it executes. The CLI checks those practical blockers before submission.

That is why:

- the wallet balance still shows free XRP after posting orders
- the token balances still show the full inventory after posting asks
- the orders still must be sized so they can actually settle if matched

## Why We Did Not Quote Flat `45/55` Everywhere

A flat `45/55` quote on every book would be incorrect for a three-outcome market.

If all three `YES` books were quoted around `50`, the implied probability mass would sum to around `150`, which is impossible because the three outcomes are mutually exclusive and must sum to `100`.

The quote shape has to respect the relative probability of each outcome.

## Probability View Used For This Setup

The quote levels were based on:

- spot XRP around `1.4668`
- roughly four days until resolution
- recent realized XRP volatility from daily and 4-hour data
- empirical rolling 4-day XRP moves
- near-term news and catalyst risk around XRP and the `1.50` technical level

The working fair-value estimate used for quoting was:

- `Below $1.35` YES: about `6`
- `$1.35 to $1.50 inclusive` YES: about `62`
- `Above $1.50` YES: about `32`

The complementary `NO` books were therefore:

- `Below $1.35` NO: about `94`
- `$1.35 to $1.50 inclusive` NO: about `38`
- `Above $1.50` NO: about `68`

This keeps the market coherent:

- `YES` probabilities sum to roughly `100`
- each `NO` book is the complement of its `YES` book

## Ladder Design

The strategy used two quote levels on each book:

- Level 1 closer to fair value for tighter displayed liquidity
- Level 2 wider from fair value for more edge when filled

This creates a ladder rather than a single flat quote.

## Ladder That Was Placed

### `Below $1.35`

| Book | Level 1 | Level 2 |
| --- | --- | --- |
| `yes` | `5 / 8 x 20` | `4 / 10 x 25` |
| `no` | `92 / 96 x 10` | `90 / 98 x 15` |

### `$1.35 to $1.50 inclusive`

| Book | Level 1 | Level 2 |
| --- | --- | --- |
| `yes` | `59 / 65 x 10` | `55 / 69 x 15` |
| `no` | `35 / 41 x 10` | `31 / 45 x 15` |

### `Above $1.50`

| Book | Level 1 | Level 2 |
| --- | --- | --- |
| `yes` | `29 / 35 x 10` | `25 / 39 x 15` |
| `no` | `65 / 71 x 10` | `61 / 75 x 15` |

Prices are shown as `bid / ask`, with quantity after `x`.

## Why `Below $1.35` YES Needed Different Sizing

Very low-priced hosted orders can fail the venue's on-chain settleability rule if the quantity is too small.

That happened on the initial dry-run for the `Below $1.35` `yes` book:

- the small `4%` and `2%` bid ideas were too small to settle on-chain at the original quantities

The solution was not to change the directional view, but to increase size to a settleable amount. That is why the final ladder on that book used:

- `5 / 8 x 20`
- `4 / 10 x 25`

## How Inventory Relates To The Orders

The minted inventory supports the ask side.

Examples:

- `Below yes` asks total `45` shares, and the wallet holds `50 YES`
- `Below no` asks total `25` shares, and the wallet holds `50 NO`
- `Middle yes` asks total `25` shares, and the wallet holds `50 YES`
- `Middle no` asks total `25` shares, and the wallet holds `50 NO`
- `Above yes` asks total `25` shares, and the wallet holds `50 YES`
- `Above no` asks total `25` shares, and the wallet holds `50 NO`

That leaves headroom instead of using the full inventory on one side.

The bid side is funded by free XRP. For the posted ladder, the approximate worst-case bid exposure is about `69.3 XRP`, which fit under the free collateral that was available after the final top-up.

## How The Strategy Makes Money

### 1. Spread capture

The basic market-making edge is buying below fair value and selling above fair value.

Example on `Middle yes`:

- estimated fair value: about `62`
- bid: `59`
- ask: `65`

If someone sells into the `59` bid, the desk buys below estimated fair value.

If someone lifts the `65` ask, the desk sells above estimated fair value.

### 2. Complete-set edge across complementary books

Because `YES + NO = 100` for each binary binding, quoting both books can create a complete-set edge.

Example on the `Middle` level 1 quotes:

- `YES bid 59`
- `NO bid 35`
- combined cost = `94`

If both bids fill, the desk has effectively bought a complete set for `0.94 XRP`. That complete set can be merged back into `1.00 XRP`, producing gross edge.

On the ask side:

- `YES ask 65`
- `NO ask 41`
- combined sale = `106`

If both asks fill, the desk has effectively sold a complete set for `1.06 XRP` even though creating that set costs `1.00 XRP`.

This complementary structure is a major reason to quote both `YES` and `NO` books instead of only one side.

### 3. Two-way flow and inventory recycling

If the market trades on both sides over time, inventory can recycle naturally:

- asks reduce token inventory and increase XRP
- bids consume XRP and increase token inventory

When both sides of one binding refill in a balanced way, complete sets can be reconstructed and merged.

## Why The Strategy Can Still Lose Money

This is not free money. The edge depends on the probabilities being roughly right and the quotes being updated when the market moves.

Main risks:

- fair value estimate is wrong
- XRP headlines or policy news shift the true odds quickly
- the market trades only one side, leaving the desk imbalanced
- the most likely winning side is sold too cheaply before repricing
- a likely losing side is bought too aggressively before repricing
- settlement-size constraints force wider or larger quotes at low prices

## Practical Monitoring Logic

The desk should monitor two things continuously:

- directional XRP moves versus the `1.35` and `1.50` thresholds
- news that changes short-horizon conviction

Examples:

- if XRP cleanly breaks and holds above `1.50`, some probability should likely move from `Middle` into `Above`
- if XRP sells off hard and threatens `1.35`, some probability should likely move from `Middle` into `Below`

When that happens, the right response is to cancel stale quotes and reprice the ladder.

## Simple Mental Model

- complete sets are neutral inventory
- minted tokens back the ask side
- free XRP backs the bid side
- quote levels are anchored to estimated probabilities
- quoting both `YES` and `NO` creates spread edge plus complete-set edge
- the main failure mode is stale or wrong probability estimates under fast-moving XRP conditions

## Session Notes

- The wallet was funded, then inventory was minted to `50` complete sets per outcome.
- A dry-run was used before live submission on every book.
- One book required a settleability adjustment before going live: `Below $1.35` `yes`.
- The full ladder was then submitted with `axiom mm quote` across the six hosted books.
