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
