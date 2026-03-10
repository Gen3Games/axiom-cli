package evm

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestQuoteBuyReturnsStructuredQuote(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_710_000_000, 0)
	state := &MarketState{
		Status:           0,
		OutcomeCount:     2,
		MarketOpen:       uint64(now.Add(-1 * time.Hour).Unix()),
		BetsClose:        uint64(now.Add(1 * time.Hour).Unix()),
		TotalVirtualPool: mustBigInt("100000000000000000000"),
		MaxTimeBonus:     mustBigInt("1500000000000000000"),
		TotalPool:        mustBigInt("200000000000000000000"),
		OutcomePools: []*big.Int{
			mustBigInt("100000000000000000000"),
			mustBigInt("100000000000000000000"),
		},
		VirtualSeeds: []*big.Int{
			mustBigInt("50000000000000000000"),
			mustBigInt("50000000000000000000"),
		},
		TotalWeightedShares: []*big.Int{
			mustBigInt("10000000000000000000"),
			mustBigInt("10000000000000000000"),
		},
	}

	quote, err := QuoteBuy(state, mustBigInt("1000000000000000000"), 0, "Will XRP rise?", "Yes", now)
	if err != nil {
		t.Fatalf("QuoteBuy() error = %v", err)
	}
	if quote.MarketTitle != "Will XRP rise?" || quote.OutcomeLabel != "Yes" {
		t.Fatalf("QuoteBuy() metadata = %+v, want market and outcome labels preserved", quote)
	}
	if quote.AmountXRP != "1" {
		t.Fatalf("QuoteBuy() amount = %q, want %q", quote.AmountXRP, "1")
	}
	if !strings.HasSuffix(quote.TimeBonusMultiplier, "x") {
		t.Fatalf("QuoteBuy() TimeBonusMultiplier = %q, want multiplier suffix", quote.TimeBonusMultiplier)
	}
	if !strings.HasSuffix(quote.CurrentSpotPrice, "%") || !strings.HasSuffix(quote.PostTradeSpotPrice, "%") {
		t.Fatalf("QuoteBuy() spot prices = current %q post %q, want percentages", quote.CurrentSpotPrice, quote.PostTradeSpotPrice)
	}
	if quote.SuggestedMinShares == "0" {
		t.Fatalf("QuoteBuy() SuggestedMinShares = %q, want non-zero suggestion", quote.SuggestedMinShares)
	}
}

func TestQuoteBuyValidatesInputs(t *testing.T) {
	t.Parallel()

	baseState := &MarketState{
		Status:              0,
		OutcomeCount:        1,
		TotalVirtualPool:    big.NewInt(1),
		TotalPool:           big.NewInt(1),
		OutcomePools:        []*big.Int{big.NewInt(1)},
		VirtualSeeds:        []*big.Int{big.NewInt(1)},
		TotalWeightedShares: []*big.Int{big.NewInt(1)},
	}

	if _, err := QuoteBuy(baseState, nil, 0, "", "", time.Now()); err == nil || !strings.Contains(err.Error(), "amount must be greater than zero") {
		t.Fatalf("QuoteBuy(nil amount) error = %v, want amount validation", err)
	}

	closedState := *baseState
	closedState.Status = 2
	if _, err := QuoteBuy(&closedState, big.NewInt(1), 0, "", "", time.Now()); err == nil || !strings.Contains(err.Error(), "market is not active") {
		t.Fatalf("QuoteBuy(closed market) error = %v, want active-market validation", err)
	}

	if _, err := QuoteBuy(baseState, big.NewInt(1), 2, "", "", time.Now()); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("QuoteBuy(out of range) error = %v, want outcome validation", err)
	}
}

func mustBigInt(value string) *big.Int {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		panic("invalid big int: " + value)
	}
	return parsed
}
