package main

import (
	"math/big"
	"testing"
)

func TestSummarizeClobSplitStatusUsesAllowanceAndOwnedMergeBalance(t *testing.T) {
	summary := summarizeClobSplitStatus(
		big.NewInt(125),
		big.NewInt(40),
		big.NewInt(9),
		big.NewInt(4),
	)

	if summary.MaxSplitWei.String() != "40" {
		t.Fatalf("MaxSplitWei = %s, want 40", summary.MaxSplitWei.String())
	}
	if !summary.SplitApproved {
		t.Fatal("SplitApproved = false, want true")
	}
	if !summary.SplitReady {
		t.Fatal("SplitReady = false, want true")
	}
	if summary.MaxMergeableWei.String() != "4" {
		t.Fatalf("MaxMergeableWei = %s, want 4", summary.MaxMergeableWei.String())
	}
	if !summary.MergeReady {
		t.Fatal("MergeReady = false, want true")
	}
	if summary.MergeApprovalRequired {
		t.Fatal("MergeApprovalRequired = true, want false")
	}
}

func TestBuildClobOrderAmountsSellMatchesServer(t *testing.T) {
	// Must match axiom/clob AmountsFromPriceQty(SideSell, 5000, 100_000_000_000)
	maker, taker := buildClobOrderAmounts("sell", 5000, 100_000_000_000)
	if maker.String() != "1000000000000000" {
		t.Fatalf("sell makerAmount = %s, want 1000000000000000", maker.String())
	}
	if taker.String() != "500000000000000" {
		t.Fatalf("sell takerAmount = %s, want 500000000000000", taker.String())
	}
}

func TestBuildClobOrderAmountsBuyMatchesServer(t *testing.T) {
	// Must match axiom/clob AmountsFromPriceQty(SideBuy, 5000, 100_000_000_000)
	maker, taker := buildClobOrderAmounts("buy", 5000, 100_000_000_000)
	if maker.String() != "500000000000000" {
		t.Fatalf("buy makerAmount = %s, want 500000000000000", maker.String())
	}
	if taker.String() != "1000000000000000" {
		t.Fatalf("buy takerAmount = %s, want 1000000000000000", taker.String())
	}
}

func TestBuildClobOrderAmountsMarketOrder(t *testing.T) {
	// price=0 → use BpsScale (10000) so amounts are non-zero
	maker, taker := buildClobOrderAmounts("buy", 0, 100)
	if maker.String() != "1000000" {
		t.Fatalf("market buy makerAmount = %s, want 1000000", maker.String())
	}
	if taker.String() != "1000000" {
		t.Fatalf("market buy takerAmount = %s, want 1000000", taker.String())
	}
}

func TestParseClobAmountDecimalXRP(t *testing.T) {
	amount, err := parseClobAmount("0.01")
	if err != nil {
		t.Fatalf("parseClobAmount(0.01) error = %v", err)
	}
	expected := "10000000000000000" // 0.01 * 1e18
	if amount.String() != expected {
		t.Fatalf("parseClobAmount(0.01) = %s, want %s", amount.String(), expected)
	}
}

func TestParseClobAmountWei(t *testing.T) {
	amount, err := parseClobAmount("10000000000000000")
	if err != nil {
		t.Fatalf("parseClobAmount(wei) error = %v", err)
	}
	if amount.String() != "10000000000000000" {
		t.Fatalf("parseClobAmount(wei) = %s, want 10000000000000000", amount.String())
	}
}

func TestParseClobAmountRejectsEmpty(t *testing.T) {
	_, err := parseClobAmount("")
	if err == nil {
		t.Fatal("parseClobAmount(\"\") should return an error")
	}
}

func TestParseClobAmountRejectsNegative(t *testing.T) {
	_, err := parseClobAmount("-1")
	if err == nil {
		t.Fatal("parseClobAmount(-1) should return an error")
	}
}

func TestParseClobAmountRejectsTooManyDecimals(t *testing.T) {
	_, err := parseClobAmount("0.1234567890123456789")
	if err == nil {
		t.Fatal("parseClobAmount with >18 decimals should return an error")
	}
}
