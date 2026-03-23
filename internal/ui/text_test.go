package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/app"
	"github.com/Gen3Games/axiom-cli/internal/evm"
)

func TestRenderKnownTypes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 10, 15, 4, 0, 0, time.UTC)
	tag := 4242
	rank := 7
	memberSince := now.Add(-24 * time.Hour)

	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{
			name: "config",
			value: app.Config{
				APIBaseURL:    "https://api.example",
				EVMRPCURL:     "https://rpc.example",
				XRPLRPCURL:    "https://xrpl.example",
				ActiveProfile: "default",
				OutputFormat:  "text",
				Profiles: map[string]app.Profile{
					"default": {Name: "default", EVMAddress: "0xabc", XRPLAddress: "rABC", DepositDestinationTag: 4242},
				},
			},
			want: []string{"CLI Configuration", "API Base URL", "Profiles", "default", "0xabc", "rABC", "4242"},
		},
		{
			name:  "register",
			value: api.RegisterResponse{WalletAddress: "0xabc", DisplayName: "default", ReferralCode: "default-alpha", DepositDestinationTag: 4242, Created: true},
			want:  []string{"CLI Registration", "Wallet", "Referral Code", "default-alpha", "Destination Tag", "created"},
		},
		{
			name:  "markets",
			value: api.MarketsResponse{Items: []api.MarketListItem{{ID: "m1", Title: "Will XRP rise?", Status: "active", Category: "crypto", EndsAt: now, ContractAddress: "0xmarket", Outcomes: []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}}, CurrentSpotPrices: []api.OutcomeSpotPrice{{Index: 0, Label: "Yes", CurrentSpotPrice: "50%"}, {Index: 1, Label: "No", CurrentSpotPrice: "50%"}}}}, Total: 1},
			want:  []string{"Markets (1 total)", "Will XRP rise?", "crypto", "Yes, No", "Yes 50%, No 50%", "0xmarket"},
		},
		{
			name:  "market details",
			value: api.MarketDetails{MarketListItem: api.MarketListItem{ID: "m1", Title: "Will XRP rise?", Headline: "XRP headline", Description: "Long form details", Status: "active", Category: "crypto", MarketType: "binary", StartsAt: memberSince, EndsAt: now, Outcomes: []api.Outcome{{Index: 0, Label: "Yes", Description: "Above"}}}, SettlementToken: "0x0", Creator: "0xcreator"},
			want:  []string{"Will XRP rise?", "Identifier", "binary", "XRP headline", "Long form details", "Outcomes", "Yes"},
		},
		{
			name: "market details disabled max time bonus",
			value: api.MarketDetails{
				MarketListItem: api.MarketListItem{
					ID:         "m2",
					Title:      "Liverpool vs Brighton",
					Status:     "active",
					Category:   "sports",
					MarketType: "standalone",
					StartsAt:   memberSince,
					EndsAt:     now,
					Outcomes:   []api.Outcome{{Index: 0, Label: "Liverpool", Description: "Home team wins"}},
				},
				PoolBreakdown: &api.MarketPoolBreakdown{
					TotalPoolXRP: "4.25",
					MaxTimeBonus: "0",
					Outcomes:     []api.OutcomePoolBreakdown{{Index: 0, Label: "Liverpool", PoolXRP: "3.49", SpotPrice: "44.75%"}},
				},
			},
			want: []string{"Pool Breakdown", "Max Time Bonus", "Disabled"},
		},
		{
			name: "profile",
			value: api.ProfileSummary{
				WalletAddress:         "0xabc",
				DisplayName:           "default",
				ReferralCode:          "default-alpha",
				DepositDestinationTag: &tag,
				MemberSince:           &memberSince,
				LastLoginAt:           &now,
				Stats:                 api.ProfileStats{TotalPredictions: 12, ResolvedMarkets: 5, OpenMarkets: 7, UnclaimedPayoutUSD: "12.34", UnclaimedPnlUSD: "1.23", LeaderboardRank: &rank, PnlUSD: 4.56, PnlPercent: 7.89, VolumeUSD: 123.45, WinRate: 66.6},
			},
			want: []string{"Profile", "Referral Code", "default-alpha", "Performance", "Win Rate", "66.60%", "$123.45", "$4.56", "4242"},
		},
		{
			name:  "positions",
			value: api.PositionsResponse{Items: []api.PositionItem{{Title: "Will XRP rise?", Status: "active", OutcomeIndex: 0, OutcomeLabel: "Yes", AmountUSD: "25.00", Shares: "100", CreatedAt: now}}, Total: 1},
			want:  []string{"Positions (1 total)", "Will XRP rise?", "$25.00", "0 · Yes", "100"},
		},
		{
			name: "unclaimed",
			value: api.UnclaimedResponse{Summary: struct {
				TotalUnclaimedPayoutUSD string `json:"totalUnclaimedPayoutUsd"`
				TotalUnclaimedPnlUSD    string `json:"totalUnclaimedPnlUsd"`
				TotalCount              int    `json:"totalCount"`
				MarketCount             int    `json:"marketCount"`
				SeriesCount             int    `json:"seriesCount"`
			}{TotalUnclaimedPayoutUSD: "22.00", TotalUnclaimedPnlUSD: "3.00", TotalCount: 1, MarketCount: 1, SeriesCount: 0}, Items: []api.UnclaimedItem{{Title: "Will XRP rise?", PayoutUSD: "22.00", PnlUSD: "3.00", ResolvedOutcome: 0, ResolvedAt: now}}},
			want: []string{"Unclaimed Winnings", "Total Payout", "$22.00", "Will XRP rise?", "$3.00"},
		},
		{
			name:  "funding",
			value: api.FundingResponse{WalletAddress: "0xabc", DepositWalletAddress: "rDeposit", DepositDestinationTag: &tag, Notes: []string{"Send XRP on XRPL."}, RecentHistory: []api.FundingHistoryItem{{Kind: "bridge", Status: "completed", AmountXRP: "10", TxHash: "0xtx", CreatedAt: now}}},
			want:  []string{"Funding", "Deposit Wallet", "Notes", "Send XRP on XRPL.", "Recent Bridge Activity", "bridge", "completed"},
		},
		{
			name:  "buy quote",
			value: evm.BuyQuote{MarketTitle: "Will XRP rise?", OutcomeIndex: 0, OutcomeLabel: "Yes", AmountXRP: "10", TimeBonusMultiplier: "1.5x", CurrentSpotPrice: "50%", AverageEntryPrice: "48%", PostTradeSpotPrice: "52%", BaseShares: "20", WeightedShares: "30", SuggestedMinShares: "29", GrossPayoutIfWinNowXRP: "12", ProfitIfWinNowXRP: "2"},
			want:  []string{"Predict Quote", "Time Bonus", "1.5x", "Est. Profit If Wins Now", "2 XRP"},
		},
		{
			name:  "generic map",
			value: map[string]any{"evmAddress": "0xabc", "storedInKeychain": false, "nextStep": "Run axiom auth register."},
			want:  []string{"EVM Address", "0xabc", "Stored In Keychain", "no", "Next Step"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := Render(test.value)
			for _, want := range test.want {
				if !strings.Contains(output, want) {
					t.Fatalf("Render() output missing %q\noutput:\n%s", want, output)
				}
			}
		})
	}
}

func TestRenderEmptyCollections(t *testing.T) {
	t.Parallel()

	if got := Render(api.MarketsResponse{}); !strings.Contains(got, "No markets found.") {
		t.Fatalf("Render(markets empty) = %q, want no-markets message", got)
	}
	if got := Render(api.PositionsResponse{}); !strings.Contains(got, "No positions found.") {
		t.Fatalf("Render(positions empty) = %q, want no-positions message", got)
	}
	if got := Render(api.UnclaimedResponse{}); !strings.Contains(got, "No unclaimed items found.") {
		t.Fatalf("Render(unclaimed empty) = %q, want no-unclaimed message", got)
	}
}
