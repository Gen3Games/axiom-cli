# CLOB Settlement Fix: Amount Scaling Mismatch

## Problem

On-chain settlement was always failing when placing orders through the Axiom CLI, while the same operations worked perfectly through the `axiom` server's `e2e_clob_orders` script.

## Root Cause

**File**: `cmd/axiom/clob_support.go` -- `buildClobOrderAmounts()`

The CLI was computing `makerAmount` and `takerAmount` using **1e18 (wei) scaling**, while the server (`axiom/clob/internal/domain/types/signature.go` -- `AmountsFromPriceQty`) uses **BpsScale (10000) scaling**.

### Before (broken)

```go
shareWei := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1e18
quantityWei := new(big.Int).Mul(big.NewInt(int64(quantity)), shareWei)
costWei := new(big.Int).Mul(big.NewInt(int64(quantity)*priceValue), new(big.Int).Div(shareWei, big.NewInt(10000)))
```

Example: SELL qty=100000000000 price=5000 bps
- makerAmount = 100000000000 * 1e18 = 1e29
- takerAmount = 100000000000 * 5000 * 1e14 = 5e28

### After (fixed)

```go
q := big.NewInt(int64(quantity))
p := big.NewInt(int64(priceBps))
scale := big.NewInt(10000) // BpsScale
// SELL: makerAmount = qty * BpsScale, takerAmount = qty * price
```

Example: SELL qty=100000000000 price=5000 bps
- makerAmount = 100000000000 * 10000 = 1000000000000000
- takerAmount = 100000000000 * 5000  = 500000000000000

These values match the server's `AmountsFromPriceQty` exactly and match the output from the working `e2e_clob_orders` script.

## Why Settlement Failed

1. The CLI signed orders with amounts ~1e14x too large
2. The server accepted the orders (signature was valid, price ratio was correct)
3. The server derived correct prices but wildly incorrect quantities from the oversized amounts
4. On-chain `matchOrders` tried to transfer enormous token amounts that exceeded wallet balances
5. The transaction reverted every time

## What Changed

| File | Change |
|------|--------|
| `cmd/axiom/clob_support.go` | Replaced 1e18 scaling with BpsScale (10000) in `buildClobOrderAmounts()` |
| `cmd/axiom/clob_support_test.go` | Added 3 tests verifying amounts match the server for sell, buy, and market orders |

## How to Test

### Prerequisites

- Two wallets with XRP on XRPL EVM mainnet (chain ID 1440000)
- Seller wallet must have outcome tokens (from a prior `splitPosition`)
- Both wallets must have ERC-20/ERC-1155 approvals for the exchange contract

### Test Flow: Sell Limit + Buy Market with Settlement

1. **Place a sell limit order** (wallet A):
```bash
axiom clob smoke <market-id> \
  --side sell \
  --type limit \
  --price 50 \
  --quantity 100000000000 \
  --expiry 24h \
  --live \
  --keep-order
```

2. **Place a buy market order** (wallet B, use `--profile` to switch accounts):
```bash
axiom clob smoke <market-id> \
  --side buy \
  --type market \
  --quantity 100000000000 \
  --live
```

3. **Check fills**:
```bash
# Poll until settlement_status = "confirmed"
axiom clob fills --market <market-id> --wallet <wallet-address>
```

### What to Verify

- The sell order rests on the book (`was_added_to_book: true`)
- The buy market order matches immediately (`trade_count: 1`)
- Settlement transitions: `submitted` -> `confirmed` (not `failed`)
- The `makerAmount` and `takerAmount` in the order JSON are small BpsScale-based values, not huge 1e18-based values

### Quick Sanity Check (dry-run, no on-chain tx)

```bash
axiom clob smoke <market-id> \
  --side sell \
  --type limit \
  --price 50 \
  --quantity 100000000000
```

Check the `order.makerAmount` in the JSON output. It should be `"1000000000000000"` (qty * 10000), not `"100000000000000000000000000000"` (qty * 1e18).
