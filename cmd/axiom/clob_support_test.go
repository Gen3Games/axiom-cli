package main

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
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
	if maker.String() != "100000000000000000000000000000" {
		t.Fatalf("sell makerAmount = %s, want 100000000000000000000000000000", maker.String())
	}
	if taker.String() != "50000000000000000000000000000" {
		t.Fatalf("sell takerAmount = %s, want 50000000000000000000000000000", taker.String())
	}
}

func TestBuildClobOrderAmountsBuyMatchesServer(t *testing.T) {
	// Must match axiom/clob AmountsFromPriceQty(SideBuy, 5000, 100_000_000_000)
	maker, taker := buildClobOrderAmounts("buy", 5000, 100_000_000_000)
	if maker.String() != "50000000000000000000000000000" {
		t.Fatalf("buy makerAmount = %s, want 50000000000000000000000000000", maker.String())
	}
	if taker.String() != "100000000000000000000000000000" {
		t.Fatalf("buy takerAmount = %s, want 100000000000000000000000000000", taker.String())
	}
}

func TestBuildClobOrderAmountsMarketOrder(t *testing.T) {
	// price=0 → use BpsScale (10000) so amounts are non-zero
	maker, taker := buildClobOrderAmounts("buy", 0, 100)
	if maker.String() != "100000000000000000000" {
		t.Fatalf("market buy makerAmount = %s, want 100000000000000000000", maker.String())
	}
	if taker.String() != "100000000000000000000" {
		t.Fatalf("market buy takerAmount = %s, want 100000000000000000000", taker.String())
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

func TestResolveClobSigningDomainUsesHostedDefaults(t *testing.T) {
	cmd := newClobCommand()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	domain, err := resolveClobSigningDomain(cmd)
	if err != nil {
		t.Fatalf("resolveClobSigningDomain() error = %v", err)
	}
	if domain.ChainID == nil || domain.ChainID.Int64() != evm.DefaultClobChainID {
		t.Fatalf("domain chainID = %v, want %d", domain.ChainID, evm.DefaultClobChainID)
	}
	if domain.VerifyingContract != common.HexToAddress(evm.DefaultClobDomainContract) {
		t.Fatalf("verifying contract = %s, want %s", domain.VerifyingContract.Hex(), evm.DefaultClobDomainContract)
	}
}

func TestBuildSignedClobCancelIncludesSignedFields(t *testing.T) {
	wallet, err := evm.WalletFromPrivateKeyHex("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatalf("WalletFromPrivateKeyHex() error = %v", err)
	}
	domain := clobSigningDomain{
		ChainID:           big.NewInt(evm.DefaultClobChainID),
		VerifyingContract: common.HexToAddress(evm.DefaultClobDomainContract),
	}
	request, err := buildSignedClobCancel(wallet, domain, "order-123", "market-123", 2, "no", wallet.Address().Hex(), "cleanup")
	if err != nil {
		t.Fatalf("buildSignedClobCancel() error = %v", err)
	}
	if request.Market != "market-123" || request.Outcome != 2 || request.TokenSide != "no" {
		t.Fatalf("request = %+v, want market/outcome/token_side preserved", request)
	}
	if request.Requester != wallet.Address().Hex() {
		t.Fatalf("requester = %q, want %q", request.Requester, wallet.Address().Hex())
	}
	if request.Nonce == "" || request.Deadline == "" || request.Signature == "" {
		t.Fatalf("request = %+v, want nonce/deadline/signature populated", request)
	}
	if request.Reason != "cleanup" {
		t.Fatalf("reason = %q, want cleanup", request.Reason)
	}
	if !strings.HasPrefix(request.Signature, "0x") {
		t.Fatalf("signature = %q, want 0x-prefixed hex", request.Signature)
	}
	if len(request.Signature) != 132 {
		t.Fatalf("signature length = %d, want 132", len(request.Signature))
	}
}

func TestResolveClobSelectionSingleBindingInfersNoDisplayedSide(t *testing.T) {
	market := &api.MarketDetails{
		MarketListItem: api.MarketListItem{
			ID:                   "clob-yes-no-single",
			MarketImplementation: "AxiomCTFMarket",
			ContractAddress:      "0x00000000000000000000000000000000000000C1",
			Outcomes:             []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
			CTFOutcomeMarkets: []api.CtfOutcomeMarketBinding{{
				OutcomeID:       "outcome-yes",
				OutcomeIndex:    0,
				Label:           "Yes",
				ContractAddress: "0x00000000000000000000000000000000000000C1",
				OutcomeTokenIDs: []string{"101", "102"},
			}},
		},
		SettlementToken: "0x0000000000000000000000000000000000000000",
	}

	selection, err := resolveClobSelection(market, "1", "", "", "", "")
	if err != nil {
		t.Fatalf("resolveClobSelection() error = %v", err)
	}
	if selection.Binding.OutcomeIndex != 0 {
		t.Fatalf("binding outcome index = %d, want 0", selection.Binding.OutcomeIndex)
	}
	if selection.LogicalOutcome.Index != 1 {
		t.Fatalf("logical outcome index = %d, want 1", selection.LogicalOutcome.Index)
	}
	if selection.DisplayedSide != "no" {
		t.Fatalf("displayed side = %q, want no", selection.DisplayedSide)
	}
	if selection.DisplayedTokenIDRaw != "102" {
		t.Fatalf("displayed token id = %q, want 102", selection.DisplayedTokenIDRaw)
	}
}

func mustFindCommand(t *testing.T, root *cobra.Command, args ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := root.Find(args)
	if err != nil {
		t.Fatalf("Find(%v) error = %v", args, err)
	}
	return cmd
}

func TestBuildLogicalMarketPlanYesNoUsesSingleLaunchOutcome(t *testing.T) {
	cmd := mustFindCommand(t, newClobCommand(), "logical", "create")
	args := []string{
		"--market-id", "logical-plan-yes-no",
		"--name", "Will XRP close above $3?",
		"--description", "Binary logical market",
		"--category", "crypto",
		"--resolution-criteria", "Close above $3.",
		"--starts-at", "2026-01-01T00:00:00Z",
		"--ends-at", "2026-01-02T00:00:00Z",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	plan, err := buildLogicalMarketPlan(cmd)
	if err != nil {
		t.Fatalf("buildLogicalMarketPlan() error = %v", err)
	}
	if plan.MarketType != "yes_no" {
		t.Fatalf("marketType = %q, want yes_no", plan.MarketType)
	}
	if len(plan.DisplayOutcomes) != 2 {
		t.Fatalf("displayOutcomes = %+v, want two display outcomes", plan.DisplayOutcomes)
	}
	if len(plan.LaunchOutcomes) != 1 {
		t.Fatalf("launchOutcomes = %+v, want one binary launch outcome for yes_no", plan.LaunchOutcomes)
	}
	if plan.LaunchOutcomes[0].Key != "yes" {
		t.Fatalf("launch outcome key = %q, want yes", plan.LaunchOutcomes[0].Key)
	}
	if plan.LogicalMarketIDHash == (common.Hash{}) {
		t.Fatal("logicalMarketIdHash = zero, want derived grouping hash")
	}
	if !plan.ResolveBy.Equal(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("resolveBy = %s, want endsAt default", plan.ResolveBy)
	}
}

func TestBuildLogicalMarketPlanMultipleChoiceUsesAllLaunchOutcomes(t *testing.T) {
	cmd := mustFindCommand(t, newClobCommand(), "logical", "create")
	args := []string{
		"--market-id", "logical-plan-multi",
		"--market-type", "multiple_choice",
		"--name", "Who wins?",
		"--description", "Multiple-choice logical market",
		"--category", "sports",
		"--resolution-criteria", "Winner is official final result.",
		"--starts-at", "2026-01-01T00:00:00Z",
		"--ends-at", "2026-01-02T00:00:00Z",
		"--outcome-label", "Warriors",
		"--outcome-label", "Lakers",
		"--outcome-label", "Draw",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	plan, err := buildLogicalMarketPlan(cmd)
	if err != nil {
		t.Fatalf("buildLogicalMarketPlan() error = %v", err)
	}
	if plan.MarketType != "multiple_choice" {
		t.Fatalf("marketType = %q, want multiple_choice", plan.MarketType)
	}
	if len(plan.DisplayOutcomes) != 3 || len(plan.LaunchOutcomes) != 3 {
		t.Fatalf("display/launch outcomes = %d/%d, want 3/3", len(plan.DisplayOutcomes), len(plan.LaunchOutcomes))
	}
	if plan.LaunchOutcomes[0].QuestionID == (common.Hash{}) || plan.LaunchOutcomes[1].QuestionID == plan.LaunchOutcomes[0].QuestionID {
		t.Fatalf("question IDs = %+v, want unique non-zero hashes", plan.LaunchOutcomes)
	}
	if plan.DisplayOutcomes[2].Key != "draw" {
		t.Fatalf("display outcome key = %q, want draw", plan.DisplayOutcomes[2].Key)
	}
}
