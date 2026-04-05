package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type ProfileStats struct {
	TotalPredictions   int     `json:"totalPredictions"`
	ResolvedMarkets    int     `json:"resolvedMarkets"`
	OpenMarkets        int     `json:"openMarkets"`
	UnclaimedMarkets   int     `json:"unclaimedMarkets"`
	UnclaimedPayoutUSD string  `json:"unclaimedPayoutUsd"`
	UnclaimedPnlUSD    string  `json:"unclaimedPnlUsd"`
	LeaderboardRank    *int    `json:"leaderboardRank"`
	PnlUSD             float64 `json:"pnlUsd"`
	PnlPercent         float64 `json:"pnlPercent"`
	VolumeUSD          float64 `json:"volumeUsd"`
	WinRate            float64 `json:"winRate"`
	TradeCount         int     `json:"tradeCount"`
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	deviceID   string
}

type ConfigResponse struct {
	APIVersion           string `json:"apiVersion"`
	Network              string `json:"network"`
	ChainID              int64  `json:"chainId"`
	NativeSymbol         string `json:"nativeSymbol"`
	RPCURL               string `json:"rpcUrl"`
	ExplorerBaseURL      string `json:"explorerBaseUrl"`
	AxiomUtilityAddress  string `json:"axiomUtilityAddress"`
	AxiomRewardsAddress  string `json:"axiomRewardsAddress"`
	DepositWalletAddress string `json:"depositWalletAddress"`
}

type RegisterRequest struct {
	WalletAddress string `json:"walletAddress"`
	Signature     string `json:"signature"`
	DeviceID      string `json:"deviceId"`
	IssuedAt      string `json:"issuedAt"`
	ReferrerCode  string `json:"referrerCode,omitempty"`
}

type RegisterResponse struct {
	WalletAddress         string `json:"walletAddress"`
	DisplayName           string `json:"displayName"`
	ReferralCode          string `json:"referralCode"`
	DepositDestinationTag int    `json:"depositDestinationTag"`
	Created               bool   `json:"created"`
}

type Outcome struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type CtfOutcomeMarketBinding struct {
	OutcomeID         string   `json:"outcomeId"`
	OutcomeIndex      int      `json:"outcomeIndex"`
	Label             string   `json:"label"`
	ContractAddress   string   `json:"contractAddress"`
	ConditionalTokens string   `json:"conditionalTokens,omitempty"`
	OutcomeTokenIDs   []string `json:"outcomeTokenIds"`
	MetadataURI       string   `json:"metadataUri"`
	DeploymentID      string   `json:"deploymentId"`
	QuestionID        string   `json:"questionId"`
	ConditionID       string   `json:"conditionId"`
}

type OutcomeSpotPrice struct {
	Index            int    `json:"index"`
	Label            string `json:"label"`
	CurrentSpotPrice string `json:"currentSpotPrice"`
}

type OutcomePoolBreakdown struct {
	Index     int    `json:"index"`
	Label     string `json:"label"`
	PoolXRP   string `json:"poolXrp"`
	SpotPrice string `json:"spotPrice"`
}

type MarketPoolBreakdown struct {
	TotalPoolXRP string                 `json:"totalPoolXrp"`
	MaxTimeBonus string                 `json:"maxTimeBonus"`
	Outcomes     []OutcomePoolBreakdown `json:"outcomes"`
}

type MarketListItem struct {
	ID                     string                    `json:"id"`
	MarketType             string                    `json:"marketType"`
	MarketImplementation   string                    `json:"marketImplementation"`
	Title                  string                    `json:"title"`
	Headline               string                    `json:"headline"`
	Description            string                    `json:"description"`
	Category               string                    `json:"category"`
	Status                 string                    `json:"status"`
	StartsAt               time.Time                 `json:"startsAt"`
	EndsAt                 time.Time                 `json:"endsAt"`
	ResolveBy              *time.Time                `json:"resolveBy"`
	ContractAddress        string                    `json:"contractAddress"`
	ChainID                *int64                    `json:"chainId"`
	IsResolved             bool                      `json:"isResolved"`
	IsSeries               bool                      `json:"isSeries"`
	MetadataURI            string                    `json:"metadataUri"`
	ImageURL               string                    `json:"imageUrl"`
	LogicalMarketAddresses []string                  `json:"logicalMarketAddresses"`
	CTFOutcomeMarkets      []CtfOutcomeMarketBinding `json:"ctfOutcomeMarkets"`
	InstanceID             string                    `json:"instanceId"`
	InstanceDate           *time.Time                `json:"instanceDate"`
	SequenceNumber         *int                      `json:"sequenceNumber"`
	ReferenceValue         string                    `json:"referenceValue"`
	AssetSymbol            string                    `json:"assetSymbol"`
	Outcomes               []Outcome                 `json:"outcomes"`
	CurrentSpotPrices      []OutcomeSpotPrice        `json:"currentSpotPrices,omitempty"`
}

type MarketsResponse struct {
	Items  []MarketListItem `json:"items"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type MarketDetails struct {
	MarketListItem
	SettlementToken      string               `json:"settlementToken"`
	Creator              string               `json:"creator"`
	OwnerAddress         string               `json:"ownerAddress"`
	ResolvedOutcomeIndex *int                 `json:"resolvedOutcomeIndex"`
	ResolutionCriteria   string               `json:"resolutionCriteria"`
	Tags                 []string             `json:"tags"`
	PoolBreakdown        *MarketPoolBreakdown `json:"poolBreakdown"`
}

type UpdateProfileRequest struct {
	WalletAddress string  `json:"walletAddress"`
	Signature     string  `json:"signature"`
	DeviceID      string  `json:"deviceId"`
	IssuedAt      string  `json:"issuedAt"`
	DisplayName   *string `json:"displayName,omitempty"`
	AvatarURL     *string `json:"avatarUrl,omitempty"`
}

type ProfileSummary struct {
	WalletAddress         string       `json:"walletAddress"`
	DisplayName           string       `json:"displayName"`
	AvatarURL             string       `json:"avatarUrl"`
	ReferralCode          string       `json:"referralCode"`
	DepositDestinationTag *int         `json:"depositDestinationTag"`
	MemberSince           *time.Time   `json:"memberSince"`
	LastLoginAt           *time.Time   `json:"lastLoginAt"`
	Stats                 ProfileStats `json:"stats"`
}

type PositionItem struct {
	MarketID      string    `json:"marketId"`
	MarketAddress string    `json:"marketAddress"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	OutcomeIndex  int       `json:"outcomeIndex"`
	OutcomeLabel  string    `json:"outcomeLabel"`
	AmountUSD     string    `json:"amountUsd"`
	Shares        string    `json:"shares"`
	CreatedAt     time.Time `json:"createdAt"`
	InstanceDate  string    `json:"instanceDate"`
	Category      string    `json:"category"`
}

type PositionsResponse struct {
	Items []PositionItem `json:"items"`
	Total int            `json:"total"`
}

type ClobBook struct {
	ClobID        string     `json:"clob_id"`
	MarketID      string     `json:"market_id"`
	Outcome       int        `json:"outcome"`
	Creator       string     `json:"creator"`
	Status        string     `json:"status"`
	BidCount      int        `json:"bid_count"`
	AskCount      int        `json:"ask_count"`
	TradeCount    int        `json:"trade_count"`
	LastPrice     *int       `json:"last_price"`
	Volume24h     int        `json:"volume_24h"`
	EventSequence int        `json:"event_sequence"`
	CreatedAt     *time.Time `json:"created_at"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

type ClobDepthLevel struct {
	ClobID     string `json:"clob_id"`
	Side       string `json:"side"`
	Price      int    `json:"price"`
	TotalQty   int    `json:"total_qty"`
	OrderCount int    `json:"order_count"`
}

type ClobDepth struct {
	Bids []ClobDepthLevel `json:"bids"`
	Asks []ClobDepthLevel `json:"asks"`
}

type ClobOrder struct {
	OrderID        string     `json:"order_id"`
	ClobID         string     `json:"clob_id"`
	Maker          string     `json:"maker"`
	Side           string     `json:"side"`
	OrderType      string     `json:"order_type"`
	Price          *int       `json:"price"`
	Quantity       int        `json:"quantity"`
	Remaining      int        `json:"remaining"`
	TotalFilled    int        `json:"total_filled"`
	MatchedPending *int       `json:"matched_pending"`
	OnchainFilled  *int       `json:"onchain_filled"`
	Status         string     `json:"status"`
	EventSequence  int        `json:"event_sequence"`
	CreatedAt      *time.Time `json:"created_at"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

type ClobFill struct {
	TradeID          string     `json:"trade_id"`
	ClobID           string     `json:"clob_id"`
	BuyOrderID       string     `json:"buy_order_id"`
	SellOrderID      string     `json:"sell_order_id"`
	TakerSide        string     `json:"taker_side"`
	Buyer            string     `json:"buyer"`
	Seller           string     `json:"seller"`
	Price            int        `json:"price"`
	Quantity         int        `json:"quantity"`
	BuyerFee         *int       `json:"buyer_fee"`
	SellerFee        *int       `json:"seller_fee"`
	SettlementStatus *string    `json:"settlement_status"`
	TxHash           *string    `json:"tx_hash"`
	ConfirmedAt      *time.Time `json:"confirmed_at"`
	CreatedAt        *time.Time `json:"created_at"`
}

type ClobOrderResponse struct {
	OrderID           string `json:"order_id"`
	RemainingQuantity int    `json:"remaining_quantity"`
	TradeCount        int    `json:"trade_count"`
	WasAddedToBook    bool   `json:"was_added_to_book"`
}

type ClobSignedOrderPayload struct {
	Maker           string `json:"maker"`
	Taker           string `json:"taker"`
	CollateralToken string `json:"collateralToken"`
	OutcomeToken    string `json:"outcomeToken"`
	OutcomeTokenID  string `json:"outcomeTokenId"`
	Side            uint8  `json:"side"`
	MakerAmount     string `json:"makerAmount"`
	TakerAmount     string `json:"takerAmount"`
	Expiration      string `json:"expiration"`
	Nonce           string `json:"nonce"`
	FeeRateBps      string `json:"feeRateBps"`
	Signature       string `json:"signature"`
	Market          string `json:"market"`
	Outcome         int    `json:"outcome"`
	OrderType       uint8  `json:"orderType"`
}

type ClobCancelOrderRequest struct {
	Market    string `json:"market"`
	Outcome   int    `json:"outcome"`
	Requester string `json:"requester"`
	Reason    string `json:"reason,omitempty"`
}

func (p *ProfileSummary) UnmarshalJSON(data []byte) error {
	type profileSummaryAlias struct {
		WalletAddress         string        `json:"walletAddress"`
		Address               string        `json:"address"`
		DisplayName           string        `json:"displayName"`
		Name                  string        `json:"name"`
		AvatarURL             string        `json:"avatarUrl"`
		ReferralCode          string        `json:"referralCode"`
		DepositDestinationTag *int          `json:"depositDestinationTag"`
		DestinationTag        *int          `json:"destinationTag"`
		MemberSince           *time.Time    `json:"memberSince"`
		CreatedAt             *time.Time    `json:"createdAt"`
		LastLoginAt           *time.Time    `json:"lastLoginAt"`
		LastLogin             *time.Time    `json:"lastLogin"`
		Stats                 *ProfileStats `json:"stats"`
		TotalPredictions      *int          `json:"totalPredictions"`
		ResolvedMarkets       *int          `json:"resolvedMarkets"`
		OpenMarkets           *int          `json:"openMarkets"`
		UnclaimedMarkets      *int          `json:"unclaimedMarkets"`
		UnclaimedPayoutUSD    *string       `json:"unclaimedPayoutUsd"`
		UnclaimedPnlUSD       *string       `json:"unclaimedPnlUsd"`
		LeaderboardRank       *int          `json:"leaderboardRank"`
		PnlUSD                *float64      `json:"pnlUsd"`
		PnlPercent            *float64      `json:"pnlPercent"`
		VolumeUSD             *float64      `json:"volumeUsd"`
		WinRate               *float64      `json:"winRate"`
		TradeCount            *int          `json:"tradeCount"`
	}

	type profileWrapper struct {
		Profile json.RawMessage `json:"profile"`
		Data    json.RawMessage `json:"data"`
	}

	payload := data
	var wrapper profileWrapper
	if err := json.Unmarshal(data, &wrapper); err == nil {
		switch {
		case len(wrapper.Profile) > 0:
			payload = wrapper.Profile
		case len(wrapper.Data) > 0:
			payload = wrapper.Data
		}
	}

	var aux profileSummaryAlias
	if err := json.Unmarshal(payload, &aux); err != nil {
		return err
	}

	stats := ProfileStats{}
	if aux.Stats != nil {
		stats = *aux.Stats
	}
	if aux.TotalPredictions != nil {
		stats.TotalPredictions = *aux.TotalPredictions
	}
	if aux.ResolvedMarkets != nil {
		stats.ResolvedMarkets = *aux.ResolvedMarkets
	}
	if aux.OpenMarkets != nil {
		stats.OpenMarkets = *aux.OpenMarkets
	}
	if aux.UnclaimedMarkets != nil {
		stats.UnclaimedMarkets = *aux.UnclaimedMarkets
	}
	if aux.UnclaimedPayoutUSD != nil {
		stats.UnclaimedPayoutUSD = *aux.UnclaimedPayoutUSD
	}
	if aux.UnclaimedPnlUSD != nil {
		stats.UnclaimedPnlUSD = *aux.UnclaimedPnlUSD
	}
	if aux.LeaderboardRank != nil {
		stats.LeaderboardRank = aux.LeaderboardRank
	}
	if aux.PnlUSD != nil {
		stats.PnlUSD = *aux.PnlUSD
	}
	if aux.PnlPercent != nil {
		stats.PnlPercent = *aux.PnlPercent
	}
	if aux.VolumeUSD != nil {
		stats.VolumeUSD = *aux.VolumeUSD
	}
	if aux.WinRate != nil {
		stats.WinRate = *aux.WinRate
	}
	if aux.TradeCount != nil {
		stats.TradeCount = *aux.TradeCount
	}

	p.WalletAddress = firstNonEmptyString(aux.WalletAddress, aux.Address)
	p.DisplayName = firstNonEmptyString(aux.DisplayName, aux.Name)
	p.AvatarURL = aux.AvatarURL
	p.ReferralCode = aux.ReferralCode
	p.DepositDestinationTag = firstNonNilInt(aux.DepositDestinationTag, aux.DestinationTag)
	p.MemberSince = firstNonNilTime(aux.MemberSince, aux.CreatedAt)
	p.LastLoginAt = firstNonNilTime(aux.LastLoginAt, aux.LastLogin)
	p.Stats = stats
	return nil
}

func (p *PositionsResponse) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var items []PositionItem
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		p.Items = items
		p.Total = len(items)
		return nil
	}

	type positionsAlias struct {
		Items     []PositionItem `json:"items"`
		Positions []PositionItem `json:"positions"`
		Data      []PositionItem `json:"data"`
		Total     int            `json:"total"`
		Meta      map[string]any `json:"meta"`
	}

	var aux positionsAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	items := aux.Items
	if len(items) == 0 {
		items = aux.Positions
	}
	if len(items) == 0 {
		items = aux.Data
	}

	p.Items = items
	p.Total = aux.Total
	if p.Total <= 0 {
		p.Total = len(items)
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonNilInt(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type UnclaimedItem struct {
	MarketID        string     `json:"marketId"`
	MarketAddress   string     `json:"marketAddress"`
	Title           string     `json:"title"`
	Type            string     `json:"type"`
	PayoutUSD       string     `json:"payoutUsd"`
	PnlUSD          string     `json:"pnlUsd"`
	TotalStakeUSD   string     `json:"totalStakeUsd"`
	WinningBets     int        `json:"winningBets"`
	TotalBets       int        `json:"totalBets"`
	ResolvedOutcome int        `json:"resolvedOutcome"`
	ResolvedAt      time.Time  `json:"resolvedAt"`
	InstanceDate    *time.Time `json:"instanceDate"`
	Category        string     `json:"category"`
}

type UnclaimedResponse struct {
	Summary struct {
		TotalUnclaimedPayoutUSD string `json:"totalUnclaimedPayoutUsd"`
		TotalUnclaimedPnlUSD    string `json:"totalUnclaimedPnlUsd"`
		TotalCount              int    `json:"totalCount"`
		MarketCount             int    `json:"marketCount"`
		SeriesCount             int    `json:"seriesCount"`
	} `json:"summary"`
	Items []UnclaimedItem `json:"items"`
}

type FundingHistoryItem struct {
	Kind           string    `json:"kind"`
	Status         string    `json:"status"`
	AmountXRP      string    `json:"amountXrp"`
	TxHash         string    `json:"txHash"`
	BridgeTxHash   string    `json:"bridgeTxHash"`
	SquidRequestID string    `json:"squidRequestId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type FundingResponse struct {
	WalletAddress         string               `json:"walletAddress"`
	DepositDestinationTag *int                 `json:"depositDestinationTag"`
	DepositWalletAddress  string               `json:"depositWalletAddress"`
	Notes                 []string             `json:"notes"`
	RecentHistory         []FundingHistoryItem `json:"recentHistory"`
}

type RewardsSummary struct {
	Address             string     `json:"address"`
	ReferralCode        string     `json:"referralCode"`
	TotalReferrals      int        `json:"totalReferrals"`
	CurrentEpochID      *int       `json:"currentEpochId"`
	CurrentEpochEndsAt  *time.Time `json:"currentEpochEndsAt"`
	CurrentEpochPoints  int        `json:"currentEpochPoints"`
	TradingPoints       int        `json:"tradingPoints"`
	ReferralPoints      int        `json:"referralPoints"`
	BonusPoints         int        `json:"bonusPoints"`
	PoolXRP             *float64   `json:"poolXrp"`
	GlobalTotalPoints   *int       `json:"globalTotalPoints"`
	PoolSharePercentage *float64   `json:"poolSharePercentage"`
	EstimatedPayoutXRP  *float64   `json:"estimatedPayoutXrp"`
}

func (r *RewardsSummary) UnmarshalJSON(data []byte) error {
	type rewardsSummaryAlias struct {
		Address             string          `json:"address"`
		ReferralCode        string          `json:"referralCode"`
		TotalReferrals      json.RawMessage `json:"totalReferrals"`
		CurrentEpochID      *int            `json:"currentEpochId"`
		CurrentEpochEndsAt  *time.Time      `json:"currentEpochEndsAt"`
		CurrentEpochPoints  int             `json:"currentEpochPoints"`
		TradingPoints       int             `json:"tradingPoints"`
		ReferralPoints      int             `json:"referralPoints"`
		BonusPoints         int             `json:"bonusPoints"`
		PoolXRP             *float64        `json:"poolXrp"`
		GlobalTotalPoints   *int            `json:"globalTotalPoints"`
		PoolSharePercentage *float64        `json:"poolSharePercentage"`
		EstimatedPayoutXRP  *float64        `json:"estimatedPayoutXrp"`
	}

	var aux rewardsSummaryAlias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	totalReferrals, err := parseFlexibleInt(aux.TotalReferrals)
	if err != nil {
		return fmt.Errorf("parse totalReferrals: %w", err)
	}

	r.Address = aux.Address
	r.ReferralCode = aux.ReferralCode
	r.TotalReferrals = totalReferrals
	r.CurrentEpochID = aux.CurrentEpochID
	r.CurrentEpochEndsAt = aux.CurrentEpochEndsAt
	r.CurrentEpochPoints = aux.CurrentEpochPoints
	r.TradingPoints = aux.TradingPoints
	r.ReferralPoints = aux.ReferralPoints
	r.BonusPoints = aux.BonusPoints
	r.PoolXRP = aux.PoolXRP
	r.GlobalTotalPoints = aux.GlobalTotalPoints
	r.PoolSharePercentage = aux.PoolSharePercentage
	r.EstimatedPayoutXRP = aux.EstimatedPayoutXRP
	return nil
}

func parseFlexibleInt(data json.RawMessage) (int, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return 0, nil
	}

	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		return number, nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		parsed, convErr := strconv.Atoi(strings.TrimSpace(asString))
		if convErr != nil {
			return 0, convErr
		}
		return parsed, nil
	}

	return 0, fmt.Errorf("unsupported integer value %s", trimmed)
}

type DailyTaskStatus struct {
	HasPredictTask          bool `json:"hasPredictTask"`
	HasDailyTwitterPostTask bool `json:"hasDailyTwitterPostTask"`
	HasBigBetTask           bool `json:"hasBigBetTask"`
	HasClaimWinningsTask    bool `json:"hasClaimWinningsTask"`
	HasMultiMarketTask      bool `json:"hasMultiMarketTask"`
	CompletedCount          int  `json:"completedCount"`
	RequiredCount           int  `json:"requiredCount"`
	HasCompletedRequirement bool `json:"hasCompletedRequirement"`
	DailyChestClaimed       bool `json:"dailyChestClaimed"`
}

type RewardsStreak struct {
	CurrentStreak                    int        `json:"currentStreak"`
	LongestStreak                    int        `json:"longestStreak"`
	LastActivityDate                 *time.Time `json:"lastActivityDate"`
	DaysUntilLottery                 int        `json:"daysUntilLottery"`
	HasAvailableLotteryTicket        bool       `json:"hasAvailableLotteryTicket"`
	CompletedDailyTasksCount         int        `json:"completedDailyTasksCount"`
	RequiredDailyTasksCount          int        `json:"requiredDailyTasksCount"`
	HasCompletedDailyTaskRequirement bool       `json:"hasCompletedDailyTaskRequirement"`
	HasCompletedDailyBetTask         bool       `json:"hasCompletedDailyBetTask"`
	HasCompletedDailyTwitterPostTask bool       `json:"hasCompletedDailyTwitterPostTask"`
	HasCompletedBigBetTask           bool       `json:"hasCompletedBigBetTask"`
	HasCompletedClaimWinningsTask    bool       `json:"hasCompletedClaimWinningsTask"`
	HasCompletedMultiMarketTask      bool       `json:"hasCompletedMultiMarketTask"`
}

type LotteryTicketInfo struct {
	ID          int        `json:"id"`
	Status      string     `json:"status"`
	PrizeType   string     `json:"prizeType"`
	PrizeAmount *int       `json:"prizeAmount"`
	PrizeLabel  string     `json:"prizeLabel"`
	EarnedAt    time.Time  `json:"earnedAt"`
	ClaimedAt   *time.Time `json:"claimedAt"`
}

type EpochReward struct {
	EpochID    int       `json:"epochId"`
	Points     int       `json:"points"`
	AmountWei  string    `json:"amountWei"`
	AmountXRP  string    `json:"amountXrp"`
	Proof      []string  `json:"proof"`
	HasClaimed bool      `json:"hasClaimed"`
	DateEnded  time.Time `json:"dateEnded"`
	IsExpired  bool      `json:"isExpired"`
	Claimable  bool      `json:"claimable"`
}

type RewardsResponse struct {
	WalletAddress                 string              `json:"walletAddress"`
	Summary                       *RewardsSummary     `json:"summary"`
	DailyTasks                    *DailyTaskStatus    `json:"dailyTasks"`
	Streak                        *RewardsStreak      `json:"streak"`
	LotteryTickets                []LotteryTicketInfo `json:"lotteryTickets"`
	EpochRewards                  []EpochReward       `json:"epochRewards"`
	TotalClaimableEpochRewardsXRP string              `json:"totalClaimableEpochRewardsXrp"`
}

type RewardsActionRequest struct {
	WalletAddress string `json:"walletAddress"`
	Signature     string `json:"signature"`
	DeviceID      string `json:"deviceId"`
	IssuedAt      string `json:"issuedAt"`
	TxHash        string `json:"txHash,omitempty"`
}

type DailyChestClaimResponse struct {
	Success     bool   `json:"success"`
	PrizeAmount *int   `json:"prizeAmount"`
	PrizeLabel  string `json:"prizeLabel"`
}

type WeeklyChestClaimResponse struct {
	Success               bool   `json:"success"`
	PrizeType             string `json:"prizeType"`
	PrizeAmount           *int   `json:"prizeAmount"`
	PrizeLabel            string `json:"prizeLabel"`
	IsConsolation         bool   `json:"isConsolation"`
	CashConvertedToPoints bool   `json:"cashConvertedToPoints"`
}

type EpochRewardClaimResponse struct {
	Success       bool        `json:"success"`
	WalletAddress string      `json:"walletAddress"`
	EpochID       int         `json:"epochId"`
	TxHash        string      `json:"txHash"`
	ClaimedReward EpochReward `json:"claimedReward"`
}

type apiError struct {
	Error string `json:"error"`
}

func NewClient(baseURL string, deviceID string) (*Client, error) {
	trimmed := strings.TrimRight(baseURL, "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse api url: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	return &Client{
		baseURL:    parsed,
		httpClient: &http.Client{Timeout: 30 * time.Second, Jar: jar},
		deviceID:   strings.TrimSpace(deviceID),
	}, nil
}

func (c *Client) GetConfig(ctx context.Context) (*ConfigResponse, error) {
	var out ConfigResponse
	if err := c.doJSON(ctx, http.MethodGet, "config", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RegisterWallet(ctx context.Context, request RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.doJSON(ctx, http.MethodPost, "register", request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListMarkets(ctx context.Context, status string, search string, category string, implementation string, limit int, offset int) (*MarketsResponse, error) {
	values := url.Values{}
	if status != "" {
		values.Set("status", status)
	}
	if search != "" {
		values.Set("search", search)
	}
	if category != "" {
		values.Set("category", category)
	}
	if implementation != "" {
		values.Set("marketImplementation", implementation)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}

	var out MarketsResponse
	if err := c.doJSON(ctx, http.MethodGet, "markets?"+values.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListAllMarkets(ctx context.Context, status string, search string, category string, implementation string, offset int) (*MarketsResponse, error) {
	const pageSize = 50
	combined := &MarketsResponse{
		Items:  make([]MarketListItem, 0),
		Limit:  0,
		Offset: offset,
	}

	currentOffset := offset
	for {
		page, err := c.ListMarkets(ctx, status, search, category, implementation, pageSize, currentOffset)
		if err != nil {
			return nil, err
		}

		if combined.Total == 0 {
			combined.Total = page.Total
		}
		combined.Items = append(combined.Items, page.Items...)

		if len(page.Items) == 0 || len(combined.Items)+offset >= page.Total {
			combined.Limit = len(combined.Items)
			return combined, nil
		}

		currentOffset += len(page.Items)
	}
}

func (c *Client) GetMarket(ctx context.Context, identifier string, instanceDate string) (*MarketDetails, error) {
	requestPath := path.Join("markets", url.PathEscape(identifier))
	if instanceDate != "" {
		requestPath += "?instanceDate=" + url.QueryEscape(instanceDate)
	}
	var out MarketDetails
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetProfile(ctx context.Context, address string) (*ProfileSummary, error) {
	var out ProfileSummary
	if err := c.doJSON(ctx, http.MethodGet, path.Join("profile", address), nil, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.WalletAddress) == "" {
		out.WalletAddress = address
	}
	return &out, nil
}

func (c *Client) UpdateProfile(ctx context.Context, address string, request UpdateProfileRequest) (*ProfileSummary, error) {
	var out ProfileSummary
	if err := c.doJSON(ctx, http.MethodPost, path.Join("profile", address), request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetPositions(ctx context.Context, address string, status string, limit int) (*PositionsResponse, error) {
	values := url.Values{}
	if status != "" {
		values.Set("status", status)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	requestPath := path.Join("profile", address, "positions")
	if len(values) > 0 {
		requestPath += "?" + values.Encode()
	}
	var out PositionsResponse
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetUnclaimed(ctx context.Context, address string) (*UnclaimedResponse, error) {
	var out UnclaimedResponse
	if err := c.doJSON(ctx, http.MethodGet, path.Join("profile", address, "unclaimed"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetFunding(ctx context.Context, address string, limit int) (*FundingResponse, error) {
	requestPath := path.Join("funding", address)
	if limit > 0 {
		requestPath += "?limit=" + strconv.Itoa(limit)
	}
	var out FundingResponse
	if err := c.doJSON(ctx, http.MethodGet, requestPath, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetClobBook(ctx context.Context, projectionBaseURL string, market string, outcome int) (*ClobBook, error) {
	var out ClobBook
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, path.Join("books", url.PathEscape(market), strconv.Itoa(outcome)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetClobDepth(ctx context.Context, projectionBaseURL string, market string, outcome int) (*ClobDepth, error) {
	var out ClobDepth
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, path.Join("books", url.PathEscape(market), strconv.Itoa(outcome), "depth"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListClobOrders(ctx context.Context, projectionBaseURL string, filters url.Values) ([]ClobOrder, error) {
	requestPath := "orders"
	if len(filters) > 0 {
		requestPath += "?" + filters.Encode()
	}
	var out []ClobOrder
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, requestPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetClobOrder(ctx context.Context, projectionBaseURL string, orderID string) (*ClobOrder, error) {
	var out ClobOrder
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, path.Join("orders", url.PathEscape(orderID)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListClobFills(ctx context.Context, projectionBaseURL string, filters url.Values) ([]ClobFill, error) {
	requestPath := "fills"
	if len(filters) > 0 {
		requestPath += "?" + filters.Encode()
	}
	var out []ClobFill
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, requestPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetClobFill(ctx context.Context, projectionBaseURL string, fillID string) (*ClobFill, error) {
	var out ClobFill
	if err := c.doJSONAgainstBase(ctx, http.MethodGet, projectionBaseURL, path.Join("fills", url.PathEscape(fillID)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SubmitClobOrder(ctx context.Context, eventstoreBaseURL string, signedOrder ClobSignedOrderPayload) (*ClobOrderResponse, error) {
	var out ClobOrderResponse
	payload := map[string]any{"signed_order": signedOrder}
	if err := c.doJSONAgainstBase(ctx, http.MethodPost, eventstoreBaseURL, "orders", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelClobOrder(ctx context.Context, eventstoreBaseURL string, orderID string, request ClobCancelOrderRequest) (*ClobOrderResponse, error) {
	var out ClobOrderResponse
	if err := c.doJSONAgainstBase(ctx, http.MethodDelete, eventstoreBaseURL, path.Join("orders", url.PathEscape(orderID)), request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetRewards(ctx context.Context, address string) (*RewardsResponse, error) {
	var out RewardsResponse
	if err := c.doJSON(ctx, http.MethodGet, path.Join("profile", address, "rewards"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ClaimDailyChest(ctx context.Context, address string, request RewardsActionRequest) (*DailyChestClaimResponse, error) {
	var out DailyChestClaimResponse
	if err := c.doJSON(ctx, http.MethodPost, path.Join("profile", address, "rewards", "daily-chest"), request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ClaimWeeklyChest(ctx context.Context, address string, ticketID int, request RewardsActionRequest) (*WeeklyChestClaimResponse, error) {
	var out WeeklyChestClaimResponse
	if err := c.doJSON(ctx, http.MethodPost, path.Join("profile", address, "rewards", "lottery", strconv.Itoa(ticketID)), request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SyncEpochRewardClaim(ctx context.Context, address string, epochID int, request RewardsActionRequest) (*EpochRewardClaimResponse, error) {
	var out EpochRewardClaimResponse
	if err := c.doJSON(ctx, http.MethodPost, path.Join("profile", address, "rewards", "epochs", strconv.Itoa(epochID)), request, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method string, requestPath string, body any, out any) error {
	endpoint, err := c.buildURL(requestPath)
	if err != nil {
		return fmt.Errorf("build request url: %w", err)
	}
	return c.doJSONEndpoint(ctx, method, endpoint, body, out, c.baseURL.Host)
}

func (c *Client) doJSONAgainstBase(ctx context.Context, method string, baseURL string, requestPath string, body any, out any) error {
	endpoint, host, err := buildURLAgainstBase(baseURL, requestPath)
	if err != nil {
		return fmt.Errorf("build request url: %w", err)
	}
	return c.doJSONEndpoint(ctx, method, endpoint, body, out, host)
}

func (c *Client) doJSONEndpoint(ctx context.Context, method string, endpoint *url.URL, body any, out any, host string) error {

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "axiom-cli/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.deviceID != "" {
		req.Header.Set("X-Axiom-CLI-Device", c.deviceID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1") {
			return fmt.Errorf("request api: %w (local CLI API unreachable; start the webapp or run `axiom config set --api-url https://axiomprotocol.io/api/cli`)", err)
		}
		return fmt.Errorf("request api: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("api error (%d): %s", resp.StatusCode, apiErr.Error)
		}
		if message, ok := formatProtectedDeploymentError(resp.StatusCode, host, data); ok {
			return fmt.Errorf("api error (%d): %s", resp.StatusCode, message)
		}
		return fmt.Errorf("api error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func buildURLAgainstBase(base string, requestPath string) (*url.URL, string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	parsedBase, err := url.Parse(trimmed)
	if err != nil {
		return nil, "", err
	}
	relative, err := url.Parse(requestPath)
	if err != nil {
		return nil, "", err
	}
	endpoint := *parsedBase
	basePath := strings.TrimSuffix(endpoint.Path, "/")
	relPath := strings.TrimPrefix(relative.Path, "/")
	if relPath != "" {
		endpoint.Path = path.Join(basePath, relPath)
	} else {
		endpoint.Path = basePath
	}
	endpoint.RawQuery = relative.RawQuery
	endpoint.Fragment = relative.Fragment
	return &endpoint, parsedBase.Host, nil
}

func formatProtectedDeploymentError(statusCode int, host string, data []byte) (string, bool) {
	body := strings.TrimSpace(string(data))
	if statusCode != http.StatusUnauthorized {
		return "", false
	}
	lowerBody := strings.ToLower(body)
	if !strings.Contains(lowerBody, "vercel authentication") && !strings.Contains(lowerBody, "authentication required") {
		return "", false
	}
	return fmt.Sprintf("received a Vercel Authentication page from %s; your CLI API base URL is pointed at a protected deployment. Run `axiom config set --api-url https://axiomprotocol.io/api/cli` or pass `--api-url https://axiomprotocol.io/api/cli`.", host), true
}

func (c *Client) buildURL(requestPath string) (*url.URL, error) {
	relative, err := url.Parse(requestPath)
	if err != nil {
		return nil, err
	}
	endpoint := *c.baseURL
	basePath := strings.TrimSuffix(endpoint.Path, "/")
	relPath := strings.TrimPrefix(relative.Path, "/")
	if relPath != "" {
		endpoint.Path = path.Join(basePath, relPath)
	} else {
		endpoint.Path = basePath
	}
	endpoint.RawQuery = relative.RawQuery
	endpoint.Fragment = relative.Fragment
	return &endpoint, nil
}
