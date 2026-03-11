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
	DepositWalletAddress string `json:"depositWalletAddress"`
}

type RegisterRequest struct {
	WalletAddress string `json:"walletAddress"`
	Signature     string `json:"signature"`
	DeviceID      string `json:"deviceId"`
	IssuedAt      string `json:"issuedAt"`
}

type RegisterResponse struct {
	WalletAddress         string `json:"walletAddress"`
	DisplayName           string `json:"displayName"`
	DepositDestinationTag int    `json:"depositDestinationTag"`
	Created               bool   `json:"created"`
}

type Outcome struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type MarketListItem struct {
	ID              string     `json:"id"`
	MarketType      string     `json:"marketType"`
	Title           string     `json:"title"`
	Headline        string     `json:"headline"`
	Description     string     `json:"description"`
	Category        string     `json:"category"`
	Status          string     `json:"status"`
	StartsAt        time.Time  `json:"startsAt"`
	EndsAt          time.Time  `json:"endsAt"`
	ResolveBy       *time.Time `json:"resolveBy"`
	ContractAddress string     `json:"contractAddress"`
	ChainID         *int64     `json:"chainId"`
	IsResolved      bool       `json:"isResolved"`
	IsSeries        bool       `json:"isSeries"`
	MetadataURI     string     `json:"metadataUri"`
	ImageURL        string     `json:"imageUrl"`
	InstanceID      string     `json:"instanceId"`
	InstanceDate    *time.Time `json:"instanceDate"`
	SequenceNumber  *int       `json:"sequenceNumber"`
	ReferenceValue  string     `json:"referenceValue"`
	AssetSymbol     string     `json:"assetSymbol"`
	Outcomes        []Outcome  `json:"outcomes"`
}

type MarketsResponse struct {
	Items  []MarketListItem `json:"items"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type MarketDetails struct {
	MarketListItem
	SettlementToken      string   `json:"settlementToken"`
	Creator              string   `json:"creator"`
	OwnerAddress         string   `json:"ownerAddress"`
	ResolvedOutcomeIndex *int     `json:"resolvedOutcomeIndex"`
	ResolutionCriteria   string   `json:"resolutionCriteria"`
	Tags                 []string `json:"tags"`
}

type ProfileSummary struct {
	WalletAddress         string     `json:"walletAddress"`
	DisplayName           string     `json:"displayName"`
	AvatarURL             string     `json:"avatarUrl"`
	DepositDestinationTag *int       `json:"depositDestinationTag"`
	MemberSince           *time.Time `json:"memberSince"`
	LastLoginAt           *time.Time `json:"lastLoginAt"`
	Stats                 struct {
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
	} `json:"stats"`
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

func (c *Client) ListMarkets(ctx context.Context, status string, search string, category string, limit int, offset int) (*MarketsResponse, error) {
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

func (c *Client) ListAllMarkets(ctx context.Context, status string, search string, category string, offset int) (*MarketsResponse, error) {
	const pageSize = 50
	combined := &MarketsResponse{
		Items:  make([]MarketListItem, 0),
		Limit:  0,
		Offset: offset,
	}

	currentOffset := offset
	for {
		page, err := c.ListMarkets(ctx, status, search, category, pageSize, currentOffset)
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

func (c *Client) doJSON(ctx context.Context, method string, requestPath string, body any, out any) error {
	endpoint, err := c.buildURL(requestPath)
	if err != nil {
		return fmt.Errorf("build request url: %w", err)
	}

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
		if strings.Contains(c.baseURL.Host, "localhost") || strings.Contains(c.baseURL.Host, "127.0.0.1") {
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
		if message, ok := formatProtectedDeploymentError(resp.StatusCode, c.baseURL.Host, data); ok {
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
