# Axiom CLI HTTP API Contract

This document defines the backend HTTP interface expected by the public Axiom CLI.

## Base URL

The CLI expects a base URL like:

```text
https://axiomprotocol.io/api/cli
```

All paths below are relative to that base URL.

## Transport conventions

- Content type: `application/json`
- Successful responses return JSON
- Error responses should return a non-2xx status with a JSON body of the form `{ "error": "message" }`
- The CLI sends `Accept: application/json`
- The CLI sends `User-Agent: axiom-cli/1.0`
- When available, the CLI sends a device identifier in `X-Axiom-CLI-Device`

## Auth and request hardening

- `POST /register` is authenticated by a wallet signature generated locally by the CLI
- Read endpoints are unauthenticated from the CLI perspective and rely on the backend's own rate limiting and abuse controls
- Implementations should rate-limit by IP, device header, and wallet address where appropriate

## Endpoints

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

Query parameters:

- `status`
- `search`
- `category`
- `limit`
- `offset`

`status` accepts `open`, `resolved`, and `all`.

- `open` means unresolved markets whose close time has not passed yet
- `resolved` means finalized markets whose outcome is available
- `all` means the backend should return the full default market set

Older backends may still tolerate legacy values such as `active` or `upcoming`, but the CLI only documents and relies on `open|resolved|all`.

`limit=0` means "return all matching markets".

CLI post-processing:

- `axiom markets list --my-positions` filters the result locally against `GET /profile/{address}/positions`
- the CLI only attaches `currentSpotPrices` when the user passes `--spot-prices`

Response body:

```json
{
  "items": [
    {
      "id": "market-id",
      "marketType": "binary",
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

## Status codes

- `200` for successful reads
- `201` or `200` for successful registration
- `400` for invalid input or unsupported query parameters
- `401` or `403` if a deployment adds access controls
- `404` for unknown markets or profiles
- `429` for rate limiting
- `500` for unexpected server errors