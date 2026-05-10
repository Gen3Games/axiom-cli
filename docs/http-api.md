# Axiom CLI HTTP API Contract

This document defines the HTTP surfaces used by the public Axiom CLI.

## Surface split

The CLI talks to three distinct backends.

### App API

Base URL default:

```text
https://axiomprotocol.io/api/cli
```

Used for:

- `config`
- `register`
- `markets`
- `profile`
- `funding`
- `rewards`

### Console API

Base URL default:

```text
https://console.axiomprotocol.io/api/cli
```

Used for:

- canonical contract address reads
- signed metadata upload
- logical CLOB register
- logical CLOB resolve
- logical CLOB update

The CLI resolves console app-root routes relative to the configured base URL. If the base ends in `/api/cli`, the CLI strips that suffix before calling `/api/...` routes.

### Hosted CLOB APIs

Base URL defaults:

```text
Projection: https://clob.axiomprotocol.io
Eventstore: https://clob.axiomprotocol.io/api
```

Used for:

- hosted book reads
- hosted order reads
- hosted fill reads
- hosted order submission
- hosted order cancellation
- hosted book close calls from the console

## Transport conventions

- Content type: `application/json`
- Successful responses return JSON
- Error responses should return a non-2xx status with a JSON body of the form `{ "error": "message" }`
- The CLI sends `Accept: application/json`
- The CLI sends `User-Agent: axiom-cli/1.0`
- When available, the CLI sends a device identifier in `X-Axiom-CLI-Device`

## Signature and auth model

### App API

- `POST /register` is authenticated by a wallet signature generated locally by the CLI
- Profile and rewards mutation routes are authenticated by wallet signatures generated locally by the CLI
- Read endpoints are unauthenticated from the CLI perspective and rely on backend rate limiting and abuse controls

### Console API

- Metadata upload is authenticated by a `personal_sign` signature from the active CLI wallet
- Logical CLOB register is authenticated by a `personal_sign` signature from the active CLI wallet
- Logical CLOB resolve is authenticated by a `personal_sign` signature from the active CLI wallet and bootstrap signer allowlist checks
- Logical CLOB update is authenticated by a `personal_sign` signature from the active CLI wallet, bootstrap signer allowlist checks, and logical `ownerAddress` matching

### Hosted CLOB APIs

- Order placement uses a signed EIP-712 `Order`
- Order cancellation uses a signed EIP-712 `CancelOrder`
- Hosted book creation for logical CLOB registration uses signed EIP-712 `CreateBook` signatures over logical `market` plus `outcome`

Hosted EIP-712 domain defaults:

- name: `Axiom CLOB`
- version: `1`
- chainId: `1440000`
- verifyingContract: `0xa232ACB932b4E745f6ee2aaC1E2707ae0E1055c5`

Hosted backend notes confirmed by live CLI usage:

- hosted read endpoints accept `token_side=yes|no`
- omitting `token_side` effectively defaults to the YES book on the backend
- immediate post-cancel projection reads can briefly lag before converging to `status: cancelled` and `remaining: 0`

## App API endpoints

All paths in this section are relative to the app API base URL.

### `GET /config`

Returns chain and deployment metadata used by the CLI.

Response shape:

```json
{
  "apiVersion": "v1",
  "network": "xrpl-mainnet",
  "chainId": 1440000,
  "nativeSymbol": "XRP",
  "rpcUrl": "https://rpc.xrplevm.org",
  "explorerBaseUrl": "https://explorer.xrplevm.org",
  "axiomUtilityAddress": "0x...",
  "axiomRewardsAddress": "0x...",
  "depositWalletAddress": "r..."
}
```

### `POST /register`

Registers or refreshes the active CLI wallet.

Request body:

```json
{
  "walletAddress": "0x...",
  "signature": "0x...",
  "deviceId": "uuid",
  "issuedAt": "2026-03-10T00:00:00Z",
  "referrerCode": "friend-code"
}
```

`referrerCode` is optional and may be either a referral code or a referrer wallet address.

Response body:

```json
{
  "walletAddress": "0x...",
  "displayName": "default",
  "referralCode": "default-alpha",
  "depositDestinationTag": 123456,
  "created": true
}
```

### `GET /markets`

Lists markets.

Query parameters the CLI may send:

- `status`
- `search`
- `marketImplementation`
- `limit`
- `offset`

`status` accepts `open`, `resolved`, and `all`.

- `open` means unresolved markets whose close time has not passed yet
- `resolved` means finalized markets whose outcome is available
- `all` means the backend should return the full default market set

Older backends may still tolerate legacy values such as `active` or `upcoming`, but the CLI only documents and relies on `open|resolved|all`.

`limit=0` means return all matching markets when the CLI falls back to local pagination.

CLI post-processing notes:

- `axiom markets list --my-positions` filters locally against `GET /profile/{address}/positions`
- the CLI only attaches `currentSpotPrices` when the user passes `--spot-prices`
- the CLI does not currently send `category` to the backend; category filtering is applied locally after fetch
- the CLI may fetch broader result sets and apply category or type filters locally when needed

Response body:

```json
{
  "items": [
    {
      "id": "market-id",
      "marketType": "binary",
      "marketImplementation": "AxiomCTFMarket",
      "title": "Will XRP close above $3.00?",
      "headline": "...",
      "description": "...",
      "category": "crypto",
      "status": "active",
      "startsAt": "2026-03-10T00:00:00Z",
      "endsAt": "2026-03-11T00:00:00Z",
      "resolveBy": "2026-03-12T00:00:00Z",
      "contractAddress": "0x...",
      "chainId": 1440000,
      "isResolved": false,
      "isSeries": false,
      "metadataUri": "ipfs://...",
      "imageUrl": "https://...",
      "logicalMarketAddresses": ["0x..."],
      "ctfOutcomeMarkets": [
        {
          "outcomeId": "market-id-yes",
          "outcomeIndex": 0,
          "label": "Yes",
          "contractAddress": "0x...",
          "conditionalTokens": "0x...",
          "outcomeTokenIds": ["1", "2"],
          "metadataUri": "ipfs://...",
          "deploymentId": "deployment-id",
          "questionId": "0x...",
          "conditionId": "0x..."
        }
      ],
      "instanceId": "instance-id",
      "instanceDate": "2026-03-10T00:00:00Z",
      "sequenceNumber": 1,
      "referenceValue": "3.00",
      "assetSymbol": "XRP",
      "outcomes": [
        {
          "index": 0,
          "label": "Yes",
          "description": "..."
        }
      ],
      "currentSpotPrices": [
        {
          "index": 0,
          "label": "Yes",
          "currentSpotPrice": "51.2%"
        }
      ]
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

### `GET /markets/{identifier}`

Returns full market detail for a market ID or contract address.

Query parameters:

- `instanceDate` optional for recurring markets

For recurring daily and hourly markets, the CLI documents `instanceDate` as `YYYY-MM-DD`.

Additional response fields beyond the list item shape:

```json
{
  "settlementToken": "0x...",
  "creator": "0x...",
  "ownerAddress": "0x...",
  "resolvedOutcomeIndex": 0,
  "resolutionCriteria": "...",
  "tags": ["crypto", "daily"],
  "poolBreakdown": {
    "totalPoolXrp": "100.0",
    "maxTimeBonus": "1.5",
    "outcomes": [
      {
        "index": 0,
        "label": "Yes",
        "poolXrp": "60.0",
        "spotPrice": "56.66%"
      }
    ]
  }
}
```

### `GET /profile/{address}`

Returns profile summary data.

Response body fields include:

- `walletAddress`
- `displayName`
- `avatarUrl`
- `referralCode`
- `depositDestinationTag`
- `memberSince`
- `lastLoginAt`
- `stats.totalPredictions`
- `stats.resolvedMarkets`
- `stats.openMarkets`
- `stats.unclaimedMarkets`
- `stats.unclaimedPayoutUsd`
- `stats.unclaimedPnlUsd`
- `stats.leaderboardRank`
- `stats.pnlUsd`
- `stats.pnlPercent`
- `stats.volumeUsd`
- `stats.winRate`
- `stats.tradeCount`

The CLI also tolerates wrapper payloads such as `{ "profile": ... }` or `{ "data": ... }`.

### `POST /profile/{address}`

Updates a CLI-authenticated profile's display metadata.

Request body fields:

- `walletAddress`
- `signature`
- `deviceId`
- `issuedAt`
- `displayName` optional
- `avatarUrl` optional

At least one of `displayName` or `avatarUrl` must be present.

The response body matches `GET /profile/{address}`.

### `GET /profile/{address}/positions`

Returns open and historical positions.

Query parameters:

- `status`
- `limit`

Response body:

```json
{
  "items": [
    {
      "marketId": "market-id",
      "marketAddress": "0x...",
      "title": "...",
      "status": "active",
      "outcomeIndex": 0,
      "outcomeLabel": "Yes",
      "amountUsd": "25.00",
      "shares": "1000000000000000000",
      "createdAt": "2026-03-10T00:00:00Z",
      "instanceDate": "2026-03-10",
      "category": "crypto"
    }
  ],
  "total": 1
}
```

The CLI also accepts backend variants such as a bare array or `{ "positions": [...] }` and normalizes them back to the `items` and `total` shape above.

### `GET /profile/{address}/unclaimed`

Returns claimable winnings and refunds.

Response body contains:

- `summary.totalUnclaimedPayoutUsd`
- `summary.totalUnclaimedPnlUsd`
- `summary.totalCount`
- `summary.marketCount`
- `summary.seriesCount`
- `items[]` with market identifiers, payout values, resolved outcome, and timestamps

### `GET /profile/{address}/rewards`

Returns the CLI rewards dashboard for a wallet.

Response body contains:

- `walletAddress`
- `summary.currentEpochId`
- `summary.currentEpochEndsAt`
- `summary.currentEpochPoints`
- `summary.estimatedPayoutXrp`
- `summary.referralCode`
- `summary.totalReferrals`
- `dailyTasks.completedCount`
- `dailyTasks.requiredCount`
- `dailyTasks.dailyChestClaimed`
- `streak.currentStreak`
- `streak.daysUntilLottery`
- `lotteryTickets[]`
- `epochRewards[]` with `epochId`, `points`, `amountWei`, `amountXrp`, `proof`, `hasClaimed`, `isExpired`, and `claimable`
- `totalClaimableEpochRewardsXrp`

### `POST /profile/{address}/rewards/daily-chest`

Claims the daily chest reward for a CLI-authenticated wallet.

Request body fields:

- `walletAddress`
- `signature`
- `deviceId`
- `issuedAt`

Response body:

```json
{
  "success": true,
  "prizeAmount": 500,
  "prizeLabel": "500 points"
}
```

### `POST /profile/{address}/rewards/lottery/{ticketId}`

Claims an available weekly chest ticket for a CLI-authenticated wallet.

Request body fields:

- `walletAddress`
- `signature`
- `deviceId`
- `issuedAt`

Response body contains `success`, `prizeType`, `prizeAmount`, `prizeLabel`, `isConsolation`, and `cashConvertedToPoints`.

### `POST /profile/{address}/rewards/epochs/{epochId}`

Marks a weekly epoch reward as claimed after the CLI has already submitted the on-chain `AxiomRewards.claim(...)` transaction.

Request body fields:

- `walletAddress`
- `signature`
- `deviceId`
- `issuedAt`
- `txHash`

Response body contains the synced `claimedReward` entry plus the submitted `txHash`.

### `GET /funding/{address}`

Returns the wallet's funding metadata.

Query parameters:

- `limit`

Response body:

```json
{
  "walletAddress": "0x...",
  "depositDestinationTag": 123456,
  "depositWalletAddress": "r...",
  "notes": [
    "Send XRP on XRPL with the destination tag shown below."
  ],
  "recentHistory": [
    {
      "kind": "bridge",
      "status": "completed",
      "amountXrp": "10",
      "txHash": "0x...",
      "bridgeTxHash": "0x...",
      "squidRequestId": "request-id",
      "createdAt": "2026-03-10T00:00:00Z",
      "updatedAt": "2026-03-10T00:01:00Z"
    }
  ]
}
```

## Console API endpoints

All paths in this section are resolved from the console app root, not appended directly to `/api/cli`.

### `GET /api/markets/contract-addresses`

Returns canonical contract addresses for a network.

Query parameters:

- `network`

Response body:

```json
{
  "success": true,
  "network": "xrpl-mainnet",
  "addresses": {
    "marketFactory": "0x...",
    "protocolConfig": "0x...",
    "vaultRegistry": "0x...",
    "ctfExchange": "0x...",
    "ctfLauncher": "0x...",
    "conditionalTokens": "0x..."
  }
}
```

### `POST /api/markets/upload-metadata`

Uploads AxiomCTF market metadata to IPFS through the console.

Request body:

```json
{
  "network": "xrpl-mainnet",
  "walletAddress": "0x...",
  "metadata": {
    "name": "Will XRP close above $3.00?",
    "headline": "XRP daily close",
    "description": "Binary CTF market description.",
    "category": "crypto",
    "tags": ["xrp", "daily"],
    "outcomes": [
      { "index": 0, "label": "Yes", "description": "..." },
      { "index": 1, "label": "No", "description": "..." }
    ],
    "resolutionCriteria": "Official close source.",
    "evidenceSources": ["https://example.com/source"],
    "image": "ipfs://market-pfp",
    "createdAt": "2026-05-10T00:00:00Z",
    "endsAt": "2026-05-11T00:00:00Z",
    "outcomeCount": 2
  },
  "message": "Axiom CLI CLOB metadata upload\n...",
  "signature": "0x..."
}
```

The signed message must exactly match the metadata payload hash and identifying fields.

Response body:

```json
{
  "success": true,
  "network": "xrpl-mainnet",
  "signerAddress": "0x...",
  "cid": "bafy...",
  "ipfsUri": "ipfs://bafy...",
  "gatewayUrl": "https://gateway.example/ipfs/bafy..."
}
```

### `POST /api/markets/register-clob-market`

Registers one or more existing binary `AxiomCTFMarket` contracts as one logical hosted market.

Important semantics:

- `yes_no` logical markets require exactly one binary address; displayed `No` is implicit as `token_side=no`
- `multiple_choice` logical markets require one binary address per displayed outcome
- hosted books are created per binding times `yes` and `no`
- `metadata.image` is persisted as the logical market PFP and may also fall back from fetched IPFS metadata when omitted

Request body:

```json
{
  "marketId": "market-id",
  "network": "xrpl-mainnet",
  "chainId": 1440000,
  "rpcUrl": "https://rpc.xrplevm.org",
  "addresses": ["0x..."],
  "isVisible": false,
  "allowUnindexed": true,
  "metadata": {
    "name": "Logical market title",
    "headline": "Optional headline",
    "description": "Logical market description",
    "category": "crypto",
    "tags": ["xrp"],
    "marketType": "yes_no",
    "resolutionCriteria": "Resolve from official source.",
    "evidenceSources": ["https://example.com/source"],
    "image": "ipfs://market-pfp",
    "startsAt": "2026-05-10T00:00:00Z",
    "endsAt": "2026-05-11T00:00:00Z",
    "resolveBy": "2026-05-11T00:00:00Z",
    "displayOutcomes": [
      { "key": "yes", "label": "Yes", "description": "..." },
      { "key": "no", "label": "No", "description": "..." }
    ]
  },
  "message": "axiom.register-clob-market:\n...",
  "signature": "0x...",
  "bookSignatures": [
    {
      "address": "0x...",
      "outcomeIndex": 0,
      "signature": "0x..."
    }
  ]
}
```

Response body:

```json
{
  "success": true,
  "marketId": "market-id",
  "signerAddress": "0x...",
  "registeredContracts": [
    {
      "contractAddress": "0x...",
      "outcomeIndex": 0,
      "outcomeLabel": "Yes",
      "outcomeTokenIds": ["1", "2"],
      "conditionId": "0x...",
      "questionId": "0x...",
      "metadataUri": "ipfs://...",
      "deploymentId": "deployment-id",
      "creator": "0x..."
    }
  ],
  "booksCreated": 2,
  "booksTotal": 2,
  "warnings": []
}
```

### `POST /api/markets/resolve-clob-market`

Marks a logical hosted market resolved after the CLI has already resolved the grouped binary markets on-chain.

The console route closes hosted books server-side before updating the logical market row.

Request body:

```json
{
  "marketId": "market-id",
  "network": "xrpl-mainnet",
  "rpcUrl": "https://rpc.xrplevm.org",
  "walletAddress": "0x...",
  "winningOutcomeIndex": 1,
  "resolutionTxHashes": ["0x...", "0x..."],
  "reason": "logical-market-resolved",
  "message": "axiom.resolve-clob-market:\n...",
  "signature": "0x..."
}
```

Response body:

```json
{
  "success": true,
  "marketId": "market-id",
  "signerAddress": "0x...",
  "resolvedOutcomeId": "market-id-outcome-1",
  "resolvedOutcomeLabel": "Lakers",
  "winningOutcomeIndex": 1,
  "booksClosed": 6,
  "booksTotal": 6,
  "alreadyResolved": false,
  "warnings": []
}
```

### `POST /api/markets/update-clob-market`

Updates top-level logical market metadata.

Current mutable fields:

- `name`
- `headline`
- `description`
- `category`
- `imageUrl`
- `tags`

Request body:

```json
{
  "marketId": "market-id",
  "network": "xrpl-mainnet",
  "walletAddress": "0x...",
  "name": "Updated title",
  "headline": "Updated headline",
  "description": "Updated description",
  "category": "sports",
  "imageUrl": "ipfs://updated-pfp",
  "tags": ["sports", "updated"],
  "message": "axiom.update-clob-market:\n...",
  "signature": "0x..."
}
```

Response body:

```json
{
  "success": true,
  "marketId": "market-id",
  "signerAddress": "0x...",
  "updatedFields": ["name", "headline", "description", "category", "imageUrl", "tags"]
}
```

## Hosted CLOB projection API

All paths in this section are relative to the projection base URL.

### `GET /books/{market}/{outcome}`

Fetches one hosted book summary.

Query parameters the CLI may send:

- `token_side=yes|no`

Response shape:

```json
{
  "clob_id": "market-id-0-yes",
  "market_id": "market-id",
  "outcome": 0,
  "creator": "0x...",
  "status": "open",
  "bid_count": 1,
  "ask_count": 2,
  "trade_count": 3,
  "last_price": 5000,
  "volume_24h": 100,
  "event_sequence": 12,
  "created_at": "2026-05-10T00:00:00Z",
  "updated_at": "2026-05-10T00:05:00Z"
}
```

### `GET /books/{market}/{outcome}/depth`

Fetches the hosted depth ladder.

Query parameters the CLI may send:

- `token_side=yes|no`

Response shape:

```json
{
  "bids": [
    {
      "clob_id": "market-id-0-yes",
      "side": "buy",
      "price": 5000,
      "total_qty": 10,
      "order_count": 1
    }
  ],
  "asks": [
    {
      "clob_id": "market-id-0-yes",
      "side": "sell",
      "price": 5200,
      "total_qty": 12,
      "order_count": 2
    }
  ]
}
```

### `GET /orders`

Lists hosted orders.

Query parameters the CLI may send:

- `clob_id`
- `token_side`
- `maker`
- `status`
- `active_only`
- `limit`

Response shape:

```json
[
  {
    "order_id": "uuid",
    "clob_id": "market-id-0-no",
    "maker": "0x...",
    "token_side": "no",
    "side": "buy",
    "order_type": "limit",
    "price": 1800,
    "quantity": 25,
    "remaining": 25,
    "total_filled": 0,
    "matched_pending": 0,
    "onchain_filled": 0,
    "status": "open",
    "event_sequence": 10,
    "created_at": "2026-05-10T00:00:00Z",
    "updated_at": "2026-05-10T00:00:00Z"
  }
]
```

### `GET /orders/{orderId}`

Fetches one hosted order.

Response shape matches an item from `GET /orders`.

### `GET /fills`

Lists hosted fills.

Query parameters the CLI may send:

- `clob_id`
- `token_side`
- `wallet`
- `limit`

Response shape:

```json
[
  {
    "trade_id": "uuid",
    "clob_id": "market-id-0-yes",
    "buy_order_id": "uuid",
    "sell_order_id": "uuid",
    "taker_side": "buy",
    "buyer": "0x...",
    "seller": "0x...",
    "price": 5000,
    "quantity": 2,
    "buyer_fee": 0,
    "seller_fee": 0,
    "settlement_status": "confirmed",
    "tx_hash": "0x...",
    "confirmed_at": "2026-05-10T00:00:05Z",
    "created_at": "2026-05-10T00:00:00Z"
  }
]
```

### `GET /fills/{fillId}`

Fetches one hosted fill.

Response shape matches an item from `GET /fills`.

## Hosted CLOB eventstore API

All paths in this section are relative to the eventstore base URL.

### `POST /orders`

Submits a hosted signed order.

Request body:

```json
{
  "signed_order": {
    "maker": "0x...",
    "taker": "0x0000000000000000000000000000000000000000",
    "collateralToken": "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
    "outcomeToken": "0x43e3fa6De5D87dd7265053FA55601d1972984edA",
    "outcomeTokenId": "123456789",
    "tokenSide": "no",
    "side": 0,
    "makerAmount": "4500000000000000000",
    "takerAmount": "25000000000000000000",
    "expiration": "1760000000",
    "nonce": "1760000000000000",
    "feeRateBps": "0",
    "signature": "0x...",
    "market": "market-id",
    "outcome": 0,
    "orderType": 0
  }
}
```

Response body:

```json
{
  "order_id": "uuid",
  "remaining_quantity": 25,
  "trade_count": 0,
  "was_added_to_book": true
}
```

### `DELETE /orders/{orderId}`

Cancels a hosted resting order.

Request body:

```json
{
  "market": "market-id",
  "outcome": 0,
  "token_side": "no",
  "requester": "0x...",
  "nonce": "1760000000000000000",
  "deadline": "1760000300",
  "signature": "0x...",
  "reason": "user-requested"
}
```

Response body reuses the order-response shape:

```json
{
  "order_id": "uuid",
  "remaining_quantity": 0,
  "trade_count": 0,
  "was_added_to_book": false
}
```

### `POST /books/{market}/{outcome}/close`

Used by the console route to close hosted books server-side.

Query parameters:

- `token_side=yes|no`

Headers:

- `X-Admin-Token: <token>`

Request body:

```json
{
  "requester": "0x...",
  "reason": "logical-market-resolved",
  "token_side": "yes"
}
```

Response body:

```json
{
  "status": "closed",
  "message": "book closed"
}
```

## Status codes

- `200` for successful reads and successful logical lifecycle updates
- `201` or `200` for successful registration where applicable
- `400` for invalid input, malformed signatures, unsupported query parameters, or mismatched payloads
- `401` for invalid signatures or recovered-signer mismatches
- `403` for bootstrap signer failures, creator or owner mismatches, or other access controls
- `404` for unknown markets, profiles, orders, or fills
- `409` for duplicate market IDs or already-registered contracts
- `429` for rate limiting
- `500` for unexpected server errors
