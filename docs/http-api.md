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
  "issuedAt": "2026-03-10T00:00:00Z"
}
```

Response body:

```json
{
  "walletAddress": "0x...",
  "displayName": "default",
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

Additional response fields beyond the list item shape:

```json
{
  "settlementToken": "0x...",
  "creator": "0x...",
  "ownerAddress": "0x...",
  "resolvedOutcomeIndex": 0,
  "resolutionCriteria": "...",
  "tags": ["crypto", "daily"]
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

### `GET /profile/{address}/unclaimed`

Returns claimable winnings and refunds.

Response body contains:

- `summary.totalUnclaimedPayoutUsd`
- `summary.totalUnclaimedPnlUsd`
- `summary.totalCount`
- `summary.marketCount`
- `summary.seriesCount`
- `items[]` with market identifiers, payout values, resolved outcome, and timestamps

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