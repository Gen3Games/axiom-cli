package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRegisterWalletSendsExpectedHeadersAndBody(t *testing.T) {
	t.Parallel()

	var gotHeader string
	var gotUserAgent string
	var gotContentType string
	var gotRequest RegisterRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/register" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/register")
		}
		gotHeader = r.Header.Get("X-Axiom-CLI-Device")
		gotUserAgent = r.Header.Get("User-Agent")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			WalletAddress:         gotRequest.WalletAddress,
			DisplayName:           "default",
			DepositDestinationTag: 4242,
			Created:               true,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.RegisterWallet(context.Background(), RegisterRequest{
		WalletAddress: "0xabc",
		Signature:     "0xsig",
		DeviceID:      "device-123",
		IssuedAt:      "2026-03-10T00:00:00.000Z",
		ReferrerCode:  "friend-code",
	})
	if err != nil {
		t.Fatalf("RegisterWallet() error = %v", err)
	}
	if response.DepositDestinationTag != 4242 {
		t.Fatalf("RegisterWallet() tag = %d, want 4242", response.DepositDestinationTag)
	}
	if gotHeader != "device-123" {
		t.Fatalf("X-Axiom-CLI-Device = %q, want %q", gotHeader, "device-123")
	}
	if gotUserAgent != "axiom-cli/1.0" {
		t.Fatalf("User-Agent = %q, want %q", gotUserAgent, "axiom-cli/1.0")
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if gotRequest.WalletAddress != "0xabc" || gotRequest.Signature != "0xsig" {
		t.Fatalf("request = %+v, want wallet/signature preserved", gotRequest)
	}
	if gotRequest.ReferrerCode != "friend-code" {
		t.Fatalf("referrerCode = %q, want %q", gotRequest.ReferrerCode, "friend-code")
	}
}

func TestListAllMarketsAggregatesPages(t *testing.T) {
	t.Parallel()

	items := []MarketListItem{
		{ID: "m1", Title: "One"},
		{ID: "m2", Title: "Two"},
		{ID: "m3", Title: "Three"},
	}
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/api/cli/markets" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/markets")
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit == 0 {
			limit = 50
		}
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MarketsResponse{
			Items:  items[offset:end],
			Total:  len(items),
			Limit:  limit,
			Offset: offset,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.ListAllMarkets(context.Background(), "active", "", "", 0)
	if err != nil {
		t.Fatalf("ListAllMarkets() error = %v", err)
	}
	if len(response.Items) != len(items) {
		t.Fatalf("ListAllMarkets() returned %d items, want %d", len(response.Items), len(items))
	}
	if requestCount != 1 {
		t.Fatalf("ListAllMarkets() requests = %d, want 1 for a small result set", requestCount)
	}
}

func TestDoJSONParsesAPIErrorMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"bad filter"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetConfig(context.Background())
	if err == nil {
		t.Fatal("GetConfig() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "api error (400): bad filter") {
		t.Fatalf("GetConfig() error = %q, want parsed API error message", err)
	}
}

func TestGetConfigLocalhostErrorIncludesHint(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://127.0.0.1:1/api/cli", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetConfig(ctx)
	if err == nil {
		t.Fatal("GetConfig() error = nil, want connection failure")
	}
	if !strings.Contains(err.Error(), "local CLI API unreachable") {
		t.Fatalf("GetConfig() error = %q, want localhost hint", err)
	}
}

func TestDoJSONFormatsVercelAuthPageError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<!doctype html><html><title>Authentication Required</title><body>Vercel Authentication</body></html>`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetConfig(context.Background())
	if err == nil {
		t.Fatal("GetConfig() error = nil, want protected deployment error")
	}
	if !strings.Contains(err.Error(), "Vercel Authentication page") {
		t.Fatalf("GetConfig() error = %q, want protected deployment hint", err)
	}
	if !strings.Contains(err.Error(), "axiomprotocol.io/api/cli") {
		t.Fatalf("GetConfig() error = %q, want production API guidance", err)
	}
}

func TestBuildURLPreservesBasePathAndQuery(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://example.com/api/cli/", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	endpoint, err := client.buildURL("markets?limit=10&offset=20")
	if err != nil {
		t.Fatalf("buildURL() error = %v", err)
	}

	want, _ := url.Parse("https://example.com/api/cli/markets?limit=10&offset=20")
	if endpoint.String() != want.String() {
		t.Fatalf("buildURL() = %q, want %q", endpoint.String(), want.String())
	}
}

func TestGetProfileAcceptsVariantResponseShapes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/profile/0xabc" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"profile": map[string]any{
				"address":               "0xabc",
				"displayName":           "agent",
				"referralCode":          "agent-alpha",
				"memberSince":           "2026-03-01T00:00:00Z",
				"lastLoginAt":           "2026-03-11T00:00:00Z",
				"depositDestinationTag": 4242,
				"pnlUsd":                12.5,
				"winRate":               66.6,
				"volumeUsd":             100.0,
				"tradeCount":            4,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	profile, err := client.GetProfile(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if profile.WalletAddress != "0xabc" {
		t.Fatalf("WalletAddress = %q, want %q", profile.WalletAddress, "0xabc")
	}
	if profile.Stats.PnlUSD != 12.5 || profile.Stats.WinRate != 66.6 {
		t.Fatalf("Stats = %+v, want top-level stats fields mapped", profile.Stats)
	}
	if profile.DisplayName != "agent" {
		t.Fatalf("DisplayName = %q, want %q", profile.DisplayName, "agent")
	}
	if profile.ReferralCode != "agent-alpha" {
		t.Fatalf("ReferralCode = %q, want %q", profile.ReferralCode, "agent-alpha")
	}
}

func TestUpdateProfileSendsExpectedBody(t *testing.T) {
	t.Parallel()

	var gotRequest UpdateProfileRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/profile/0xabc" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc")
		}
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ProfileSummary{
			WalletAddress: "0xabc",
			DisplayName:   "agent-zero",
			AvatarURL:     "https://example.com/avatar.png",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	displayName := "agent-zero"
	avatarURL := "https://example.com/avatar.png"
	profile, err := client.UpdateProfile(context.Background(), "0xabc", UpdateProfileRequest{
		WalletAddress: "0xabc",
		Signature:     "0xsig",
		DeviceID:      "device-123",
		IssuedAt:      "2026-03-10T00:00:00.000Z",
		DisplayName:   &displayName,
		AvatarURL:     &avatarURL,
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if gotRequest.Signature != "0xsig" {
		t.Fatalf("signature = %q, want %q", gotRequest.Signature, "0xsig")
	}
	if gotRequest.DisplayName == nil || *gotRequest.DisplayName != displayName {
		t.Fatalf("displayName = %#v, want %q", gotRequest.DisplayName, displayName)
	}
	if profile.AvatarURL != avatarURL {
		t.Fatalf("AvatarURL = %q, want %q", profile.AvatarURL, avatarURL)
	}
}

func TestGetRewardsUsesRewardsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/profile/0xabc/rewards" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc/rewards")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RewardsResponse{
			WalletAddress: "0xabc",
			DailyTasks:    &DailyTaskStatus{CompletedCount: 3, RequiredCount: 3, DailyChestClaimed: false},
			EpochRewards:  []EpochReward{{EpochID: 12, AmountXRP: "1", Claimable: true}},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	rewards, err := client.GetRewards(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("GetRewards() error = %v", err)
	}
	if rewards.WalletAddress != "0xabc" || len(rewards.EpochRewards) != 1 {
		t.Fatalf("rewards = %+v, want wallet and one epoch reward", rewards)
	}
}

func TestGetRewardsAcceptsStringTotalReferrals(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/profile/0xabc/rewards" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc/rewards")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"walletAddress": "0xabc",
			"summary": map[string]any{
				"address":            "0xabc",
				"referralCode":       "agent-alpha",
				"totalReferrals":     "0",
				"currentEpochPoints": 0,
				"tradingPoints":      0,
				"referralPoints":     0,
				"bonusPoints":        0,
			},
			"dailyTasks": map[string]any{
				"completedCount":          0,
				"requiredCount":           3,
				"dailyChestClaimed":       false,
				"hasCompletedRequirement": false,
			},
			"streak": map[string]any{
				"currentStreak":                    0,
				"longestStreak":                    0,
				"daysUntilLottery":                 7,
				"hasAvailableLotteryTicket":        false,
				"completedDailyTasksCount":         0,
				"requiredDailyTasksCount":          3,
				"hasCompletedDailyTaskRequirement": false,
			},
			"lotteryTickets":                []any{},
			"epochRewards":                  []any{},
			"totalClaimableEpochRewardsXrp": "0",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	rewards, err := client.GetRewards(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("GetRewards() error = %v", err)
	}
	if rewards.Summary == nil {
		t.Fatal("GetRewards() summary = nil, want decoded summary")
	}
	if rewards.Summary.TotalReferrals != 0 {
		t.Fatalf("TotalReferrals = %d, want 0", rewards.Summary.TotalReferrals)
	}
	if rewards.Summary.ReferralCode != "agent-alpha" {
		t.Fatalf("ReferralCode = %q, want %q", rewards.Summary.ReferralCode, "agent-alpha")
	}
}

func TestSyncEpochRewardClaimSendsExpectedBody(t *testing.T) {
	t.Parallel()

	var gotRequest RewardsActionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/profile/0xabc/rewards/epochs/12" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc/rewards/epochs/12")
		}
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %q, want %q", r.Method, http.MethodPost)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EpochRewardClaimResponse{
			Success:       true,
			WalletAddress: "0xabc",
			EpochID:       12,
			TxHash:        gotRequest.TxHash,
			ClaimedReward: EpochReward{EpochID: 12, AmountXRP: "1", HasClaimed: true},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.SyncEpochRewardClaim(context.Background(), "0xabc", 12, RewardsActionRequest{
		WalletAddress: "0xabc",
		Signature:     "0xsig",
		DeviceID:      "device-123",
		IssuedAt:      "2026-03-10T00:00:00.000Z",
		TxHash:        "0x1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err != nil {
		t.Fatalf("SyncEpochRewardClaim() error = %v", err)
	}
	if gotRequest.TxHash == "" || response.TxHash != gotRequest.TxHash {
		t.Fatalf("tx hash = %q, want preserved sync request tx hash", response.TxHash)
	}
}

func TestGetPositionsAcceptsWrappedAndBareArrays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		response  string
		wantTotal int
	}{
		{name: "positions wrapper", response: `{"positions":[{"marketId":"m1","status":"active"}],"total":1}`, wantTotal: 1},
		{name: "bare array", response: `[{"marketId":"m1","status":"active"},{"marketId":"m2","status":"won"}]`, wantTotal: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/cli/profile/0xabc/positions" {
					t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/profile/0xabc/positions")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL+"/api/cli", "")
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			positions, err := client.GetPositions(context.Background(), "0xabc", "all", 0)
			if err != nil {
				t.Fatalf("GetPositions() error = %v", err)
			}
			if positions.Total != test.wantTotal || len(positions.Items) != test.wantTotal {
				t.Fatalf("positions = %+v, want total/items %d", positions, test.wantTotal)
			}
		})
	}
}
