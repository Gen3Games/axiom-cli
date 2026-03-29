package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	axrpl "github.com/Gen3Games/axiom-cli/internal/xrpl"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestCommandHelpSmoke(t *testing.T) {
	setCLIEnv(t)

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"--help"}, want: "Available Commands:"},
		{args: []string{"config", "--help"}, want: "Inspect or update local CLI configuration"},
		{args: []string{"config", "show", "--help"}, want: "Show the current local configuration"},
		{args: []string{"config", "set", "--help"}, want: "Update the CLI API or RPC URLs"},
		{args: []string{"wallet", "--help"}, want: "Create, import, inspect, and fund local wallets"},
		{args: []string{"wallet", "create", "--help"}, want: "Create a new XRPL EVM wallet"},
		{args: []string{"wallet", "import", "--help"}, want: "Import an existing XRPL EVM private key"},
		{args: []string{"wallet", "accounts", "list", "--help"}, want: "List all local wallet accounts"},
		{args: []string{"wallet", "accounts", "use", "--help"}, want: "Set the active local wallet account"},
		{args: []string{"wallet", "xrpl-create", "--help"}, want: "Create a native XRPL wallet"},
		{args: []string{"wallet", "xrpl-import", "--help"}, want: "Import an XRPL seed"},
		{args: []string{"wallet", "show", "--help"}, want: "Show local wallet addresses"},
		{args: []string{"wallet", "reset", "--help"}, want: "Clear local wallet addresses"},
		{args: []string{"wallet", "balance", "--help"}, want: "Show EVM and XRPL balances"},
		{args: []string{"auth", "register", "--help"}, want: "Register or refresh the active wallet"},
		{args: []string{"markets", "--help"}, want: "Discover Axiom markets"},
		{args: []string{"markets", "list", "--help"}, want: "List markets from the Axiom backend"},
		{args: []string{"markets", "get", "--help"}, want: "Get detailed metadata for a single market"},
		{args: []string{"profile", "--help"}, want: "Read profile stats"},
		{args: []string{"profile", "show", "--help"}, want: "Show an Axiom profile summary"},
		{args: []string{"profile", "positions", "--help"}, want: "List recent positions"},
		{args: []string{"profile", "unclaimed", "--help"}, want: "Show unclaimed winnings"},
		{args: []string{"rewards", "--help"}, want: "Track rewards progress"},
		{args: []string{"rewards", "show", "--help"}, want: "Show rewards progress"},
		{args: []string{"rewards", "claim", "--help"}, want: "Claim daily chest"},
		{args: []string{"rewards", "claim", "daily", "--help"}, want: "Claim the daily chest reward"},
		{args: []string{"rewards", "claim", "weekly", "--help"}, want: "Claim an available weekly chest ticket"},
		{args: []string{"rewards", "claim", "epoch", "--help"}, want: "Claim the current claimable epoch reward"},
		{args: []string{"funding", "--help"}, want: "Handle direct XRP funding"},
		{args: []string{"funding", "info", "--help"}, want: "Show funding instructions"},
		{args: []string{"funding", "direct", "--help"}, want: "Send native XRP on XRPL EVM directly"},
		{args: []string{"funding", "bridge", "--help"}, want: "Prepare XRPL relay funding for your Axiom wallet."},
		{args: []string{"funding", "bridge-submit", "--help"}, want: "Send an XRPL payment from the active local XRPL wallet"},
		{args: []string{"predict", "--help"}, want: "Place predictions on Axiom markets"},
		{args: []string{"predict", "quote", "--help"}, want: "Preview weighted shares"},
		{args: []string{"predict", "buy", "--help"}, want: "Buy into an Axiom market outcome"},
		{args: []string{"clob", "--help"}, want: "Inspect and manage hosted CLOB books, orders, fills, and cancellations"},
		{args: []string{"clob", "book", "depth", "--help"}, want: "Fetch the hosted depth ladder and book summary for a logical CLOB proposition"},
		{args: []string{"clob", "smoke", "--help"}, want: "Run a hosted CLOB smoke test using imported CLI accounts"},
		{args: []string{"clob", "wallet", "status", "--help"}, want: "Show collateral balances, allowances, approvals, and per-outcome token balances for a CLOB market"},
		{args: []string{"clob", "wallet", "approve", "--help"}, want: "Approve collateral and outcome-token spending for the hosted CLOB exchange"},
		{args: []string{"clob", "orders", "list", "--help"}, want: "List hosted CLOB orders for a logical proposition or wallet"},
		{args: []string{"clob", "fills", "list", "--help"}, want: "List hosted CLOB fills for a logical proposition or wallet"},
		{args: []string{"clob", "order", "place", "--help"}, want: "Sign and submit a hosted CLOB order for a logical CTF market"},
		{args: []string{"clob", "order", "get", "--help"}, want: "Fetch a single hosted CLOB order by ID"},
		{args: []string{"clob", "order", "cancel", "--help"}, want: "Cancel a hosted resting CLOB order using the requester wallet address"},
		{args: []string{"clob", "fills", "get", "--help"}, want: "Fetch a single hosted CLOB fill by ID"},
		{args: []string{"claim", "--help"}, want: "Claim winnings"},
		{args: []string{"claim", "market", "--help"}, want: "Claim winnings or refunds"},
		{args: []string{"claim", "batch", "--help"}, want: "Claim all currently unclaimed"},
	}

	for _, test := range tests {
		stdout, stderr, err := executeCLI(t, test.args...)
		if err != nil {
			t.Fatalf("executeCLI(%v) error = %v\nstderr:\n%s", test.args, err, stderr)
		}
		if !strings.Contains(stdout, test.want) {
			t.Fatalf("executeCLI(%v) output missing %q\nstdout:\n%s", test.args, test.want, stdout)
		}
	}
}

func TestConfigSetHonorsJSONOutput(t *testing.T) {
	setCLIEnv(t)

	stdout, stderr, err := executeCLI(t, "--json", "config", "set", "--api-url", "https://api.example", "--rpc-url", "https://rpc.example", "--xrpl-rpc-url", "https://xrpl.example")
	if err != nil {
		t.Fatalf("config set error = %v\nstderr:\n%s", err, stderr)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	if payload["message"] != "Configuration updated." {
		t.Fatalf("message = %#v, want %q", payload["message"], "Configuration updated.")
	}
	configMap, ok := payload["config"].(map[string]any)
	if !ok {
		t.Fatalf("config payload = %#v, want object", payload["config"])
	}
	if configMap["apiBaseUrl"] != "https://api.example" {
		t.Fatalf("apiBaseUrl = %#v, want %q", configMap["apiBaseUrl"], "https://api.example")
	}
}

func TestWalletResetRequiresConfirmation(t *testing.T) {
	setCLIEnv(t)

	_, stderr, err := executeCLI(t, "wallet", "reset")
	if err == nil {
		t.Fatal("wallet reset error = nil, want confirmation failure")
	}
	if !strings.Contains(err.Error(), "confirmation required") {
		t.Fatalf("wallet reset error = %q, want confirmation guidance", err)
	}
	if !strings.Contains(stderr, "irreversible reset") {
		t.Fatalf("wallet reset stderr missing irreversible warning\nstderr:\n%s", stderr)
	}
}

func TestRuntimeErrorsDoNotPrintUsage(t *testing.T) {
	setCLIEnv(t)

	_, stderr, err := executeCLI(t, "funding", "bridge")
	if err == nil {
		t.Fatal("funding bridge error = nil, want missing wallet error")
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("funding bridge stderr unexpectedly included usage\nstderr:\n%s", stderr)
	}
	if !strings.Contains(err.Error(), "no EVM wallet is configured") {
		t.Fatalf("funding bridge error = %q, want missing wallet guidance", err)
	}
}

func TestWalletAuthAndReadCommandsWithMockAPI(t *testing.T) {
	setCLIEnv(t)
	server, state := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	wallet, err := evm.WalletFromPrivateKeyHex(privateKey)
	if err != nil {
		t.Fatalf("WalletFromPrivateKeyHex() error = %v", err)
	}

	stdout, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey)
	if err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, wallet.Address().Hex()) {
		t.Fatalf("wallet import stdout missing address %q\nstdout:\n%s", wallet.Address().Hex(), stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "auth", "register", "--ref-code", "friend-code")
	if err != nil {
		t.Fatalf("auth register error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "4242") || !strings.Contains(stdout, "default-alpha") {
		t.Fatalf("auth register stdout missing destination tag\nstdout:\n%s", stdout)
	}
	if state.lastRegister.WalletAddress != wallet.Address().Hex() {
		t.Fatalf("register wallet = %q, want %q", state.lastRegister.WalletAddress, wallet.Address().Hex())
	}
	if state.lastRegister.ReferrerCode != "friend-code" {
		t.Fatalf("register referrerCode = %q, want %q", state.lastRegister.ReferrerCode, "friend-code")
	}
	if state.lastRegister.DeviceID == "" || state.lastDeviceHeader == "" {
		t.Fatalf("register device ID/body header should be non-empty, got body=%q header=%q", state.lastRegister.DeviceID, state.lastDeviceHeader)
	}

	stdout, stderr, err = executeCLI(t, "--json", "wallet", "show")
	if err != nil {
		t.Fatalf("wallet show error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "4242") || !strings.Contains(stdout, wallet.Address().Hex()) {
		t.Fatalf("wallet show stdout missing registered state\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "markets", "list", "--category", "crypto", "--limit", "1", "--offset", "1")
	if err != nil {
		t.Fatalf("markets list error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "Will XRP close above $3.00?") {
		t.Fatalf("markets list stdout missing filtered market\nstdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "Will BTC reach $120k?") {
		t.Fatalf("markets list stdout included filtered-out market\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "markets", "get", "market-1")
	if err != nil {
		t.Fatalf("markets get error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "resolutionCriteria") {
		t.Fatalf("markets get stdout missing detail fields\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "profile", "show")
	if err != nil {
		t.Fatalf("profile show error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, wallet.Address().Hex()) || !strings.Contains(stdout, "default") || !strings.Contains(stdout, "default-alpha") {
		t.Fatalf("profile show stdout missing profile data\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "profile", "show", wallet.Address().Hex())
	if err != nil {
		t.Fatalf("profile show by address error = %v\nstderr:\n%s", err, stderr)
	}
	var profilePayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &profilePayload); err != nil {
		t.Fatalf("json.Unmarshal(profile show by address stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	if profilePayload["walletAddress"] != wallet.Address().Hex() {
		t.Fatalf("walletAddress = %#v, want %q", profilePayload["walletAddress"], wallet.Address().Hex())
	}
	if profilePayload["referralCode"] != "default-alpha" {
		t.Fatalf("referralCode = %#v, want %q", profilePayload["referralCode"], "default-alpha")
	}
	statsPayload, ok := profilePayload["stats"].(map[string]any)
	if !ok || statsPayload["pnlUsd"] == nil || statsPayload["winRate"] == nil {
		t.Fatalf("profile show by address stats = %#v, want populated stats object", profilePayload["stats"])
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "profile", "positions")
	if err != nil {
		t.Fatalf("profile positions error = %v\nstderr:\n%s", err, stderr)
	}
	var positionsPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &positionsPayload); err != nil {
		t.Fatalf("json.Unmarshal(profile positions stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	items, ok := positionsPayload["items"].([]any)
	if !ok || len(items) != 1 || positionsPayload["total"] != float64(1) {
		t.Fatalf("profile positions payload = %#v, want items/total shape", positionsPayload)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "profile", "unclaimed")
	if err != nil {
		t.Fatalf("profile unclaimed error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "totalUnclaimedPayoutUsd") {
		t.Fatalf("profile unclaimed stdout missing summary\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "funding", "info")
	if err != nil {
		t.Fatalf("funding info error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "depositWalletAddress") || !strings.Contains(stdout, "recentHistory") {
		t.Fatalf("funding info stdout missing funding metadata\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "funding", "bridge", "--amount", "25")
	if err != nil {
		t.Fatalf("funding bridge json error = %v\nstderr:\n%s", err, stderr)
	}
	var bridgePayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &bridgePayload); err != nil {
		t.Fatalf("json.Unmarshal(funding bridge stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	if bridgePayload["paymentUri"] != "xrpl:rDepositWallet?dt=4242&amount=25" {
		t.Fatalf("paymentUri = %#v, want %q", bridgePayload["paymentUri"], "xrpl:rDepositWallet?dt=4242&amount=25")
	}

	stdout, stderr, err = executeCLI(t, "--api-url", server.URL+"/api/cli", "funding", "bridge", "--amount", "25")
	if err != nil {
		t.Fatalf("funding bridge text error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "Bridge Funding QR") || !strings.Contains(stdout, "Instructions") {
		t.Fatalf("funding bridge text stdout missing preview sections\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "xrpl:rDepositWallet?dt=4242&amount=25") {
		t.Fatalf("funding bridge text stdout missing payment URI\nstdout:\n%s", stdout)
	}

	xrplWallet, err := axrpl.NewRandomWallet()
	if err != nil {
		t.Fatalf("NewRandomWallet() error = %v", err)
	}
	stdout, stderr, err = executeCLI(t, "--json", "wallet", "xrpl-import", "--seed", xrplWallet.Seed())
	if err != nil {
		t.Fatalf("wallet xrpl-import error = %v\nstderr:\n%s", err, stderr)
	}

	originalSubmitBridgePayment := submitBridgePayment
	submitBridgePayment = func(_ context.Context, rpcURL string, seed string, destination string, destinationTag int, amountXRP string) (string, error) {
		if rpcURL == "" || seed == "" {
			t.Fatalf("submitBridgePayment() received empty rpcURL or seed")
		}
		if destination != "rDepositWallet" || destinationTag != 4242 || amountXRP != "25" {
			t.Fatalf("submitBridgePayment() args = (%q, %d, %q), want (%q, %d, %q)", destination, destinationTag, amountXRP, "rDepositWallet", 4242, "25")
		}
		return "XRPLTX123", nil
	}
	t.Cleanup(func() {
		submitBridgePayment = originalSubmitBridgePayment
	})

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "funding", "bridge-submit", "--amount", "25")
	if err != nil {
		t.Fatalf("funding bridge-submit error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "XRPLTX123") || !strings.Contains(stdout, xrplWallet.Address()) {
		t.Fatalf("funding bridge-submit stdout missing tx hash or xrpl wallet\nstdout:\n%s", stdout)
	}
}

func TestFundingHelpShowsSubcommandFlags(t *testing.T) {
	setCLIEnv(t)

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"funding", "info", "--help"}, want: "[wallet-address]"},
		{args: []string{"funding", "direct", "--help"}, want: "--to string"},
		{args: []string{"funding", "bridge", "--help"}, want: "--submit"},
	}

	for _, test := range tests {
		stdout, stderr, err := executeCLI(t, test.args...)
		if err != nil {
			t.Fatalf("executeCLI(%v) error = %v\nstderr:\n%s", test.args, err, stderr)
		}
		if !strings.Contains(stdout, test.want) {
			t.Fatalf("executeCLI(%v) output missing %q\nstdout:\n%s", test.args, test.want, stdout)
		}
	}
}

func TestPredictBuyNegativeAmountReturnsValidationError(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	_, _, err := executeCLI(t, "--api-url", server.URL+"/api/cli", "predict", "buy", "market-1", "--label", "Yes", "--amount", "--", "-1")
	if err == nil {
		t.Fatal("predict buy negative amount error = nil, want validation failure")
	}
	if !strings.Contains(err.Error(), "amount must be greater than zero") {
		t.Fatalf("predict buy negative amount error = %q, want amount validation", err)
	}
}

func TestPredictBuyDryRunReturnsQuote(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	originalLoadMarketState := loadMarketState
	loadMarketState = func(_ context.Context, _ string, _ common.Address) (*evm.MarketState, error) {
		now := time.Now().UTC()
		return &evm.MarketState{
			Status:           0,
			OutcomeCount:     2,
			MarketOpen:       uint64(now.Add(-1 * time.Hour).Unix()),
			BetsClose:        uint64(now.Add(1 * time.Hour).Unix()),
			TotalVirtualPool: testBigInt("100000000000000000000"),
			MaxTimeBonus:     testBigInt("1500000000000000000"),
			TotalPool:        testBigInt("200000000000000000000"),
			OutcomePools: []*big.Int{
				testBigInt("100000000000000000000"),
				testBigInt("100000000000000000000"),
			},
			VirtualSeeds: []*big.Int{
				testBigInt("50000000000000000000"),
				testBigInt("50000000000000000000"),
			},
			TotalWeightedShares: []*big.Int{
				testBigInt("10000000000000000000"),
				testBigInt("10000000000000000000"),
			},
		}, nil
	}
	t.Cleanup(func() {
		loadMarketState = originalLoadMarketState
	})

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "predict", "buy", "market-1", "--label", "Yes", "--amount", "1", "--dry-run")
	if err != nil {
		t.Fatalf("predict buy dry-run error = %v\nstderr:\n%s", err, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal(dry-run stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	if payload["dryRun"] != true {
		t.Fatalf("dryRun = %#v, want true", payload["dryRun"])
	}
	quote, ok := payload["quote"].(map[string]any)
	if !ok || quote["amountXrp"] != "1" {
		t.Fatalf("quote payload = %#v, want quote object with amountXrp", payload["quote"])
	}
}

func TestClaimMarketRedeemsClobPositions(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	originalGetERC1155Balance := getERC1155Balance
	originalRedeemCTFMarket := redeemCTFMarket
	originalLoadCTFMarketMetadata := loadCTFMarketMetadata
	getERC1155Balance = func(_ context.Context, _ string, token common.Address, _ common.Address, tokenID *big.Int) (*big.Int, error) {
		switch {
		case token == common.HexToAddress("0x00000000000000000000000000000000000000D1") && tokenID.String() == "101":
			return big.NewInt(5), nil
		case token == common.HexToAddress("0x00000000000000000000000000000000000000D2") && tokenID.String() == "202":
			return big.NewInt(3), nil
		default:
			return big.NewInt(0), nil
		}
	}
	loadCTFMarketMetadata = func(_ context.Context, _ string, market common.Address) (*evm.CTFMarketMetadata, error) {
		switch market.Hex() {
		case common.HexToAddress("0x00000000000000000000000000000000000000C1").Hex():
			return &evm.CTFMarketMetadata{ConditionalTokens: common.HexToAddress("0x00000000000000000000000000000000000000D1")}, nil
		case common.HexToAddress("0x00000000000000000000000000000000000000C2").Hex():
			return &evm.CTFMarketMetadata{ConditionalTokens: common.HexToAddress("0x00000000000000000000000000000000000000D2")}, nil
		default:
			return nil, fmt.Errorf("unexpected market %s", market.Hex())
		}
	}
	redeemCTFMarket = func(_ context.Context, _ string, _ *big.Int, _ string, market common.Address, indexSets []*big.Int) (common.Hash, error) {
		seed := market.Hex()
		if len(indexSets) > 0 {
			seed += indexSets[0].String()
		}
		return common.BytesToHash([]byte(seed)), nil
	}
	t.Cleanup(func() {
		getERC1155Balance = originalGetERC1155Balance
		loadCTFMarketMetadata = originalLoadCTFMarketMetadata
		redeemCTFMarket = originalRedeemCTFMarket
	})

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "claim", "market", "clob-1")
	if err != nil {
		t.Fatalf("claim market clob-1 error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "redeemedLegs") || !strings.Contains(stdout, "transactions") {
		t.Fatalf("claim market clob-1 stdout missing redemption payload\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Yes") || !strings.Contains(stdout, "No") {
		t.Fatalf("claim market clob-1 stdout missing redeemed side labels\nstdout:\n%s", stdout)
	}
}

func TestWalletAccountsListAndUse(t *testing.T) {
	setCLIEnv(t)

	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"); err != nil {
		t.Fatalf("wallet import default error = %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--account", "trader-two", "--private-key", "59c6995e998f97a5a0044966f094538e1d7dff27c1d312bb7f6d1ab8d1b2b5d7"); err != nil {
		t.Fatalf("wallet import trader-two error = %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := executeCLI(t, "--json", "wallet", "accounts", "list")
	if err != nil {
		t.Fatalf("wallet accounts list error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "\"account\": \"default\"") || !strings.Contains(stdout, "\"account\": \"trader-two\"") {
		t.Fatalf("wallet accounts list stdout missing accounts\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\"activeAccount\": \"default\"") {
		t.Fatalf("wallet accounts list stdout missing active account\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "wallet", "accounts", "use", "trader-two")
	if err != nil {
		t.Fatalf("wallet accounts use error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "\"activeAccount\": \"trader-two\"") {
		t.Fatalf("wallet accounts use stdout missing new active account\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "wallet", "show")
	if err != nil {
		t.Fatalf("wallet show after account switch error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "\"profile\": \"trader-two\"") {
		t.Fatalf("wallet show stdout missing switched account\nstdout:\n%s", stdout)
	}
}

func TestClobSmokeDryRunUsesImportedAccount(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	originalGetERC20Balance := getERC20Balance
	originalGetERC20Allowance := getERC20Allowance
	originalIsERC1155ApprovedForAll := isERC1155ApprovedForAll
	originalGetERC1155Balance := getERC1155Balance
	getERC20Balance = func(_ context.Context, _ string, _ common.Address, _ common.Address) (*big.Int, error) {
		return big.NewInt(1_000_000), nil
	}
	getERC20Allowance = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ common.Address) (*big.Int, error) {
		return big.NewInt(1_000_000), nil
	}
	isERC1155ApprovedForAll = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ common.Address) (bool, error) {
		return true, nil
	}
	getERC1155Balance = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ *big.Int) (*big.Int, error) {
		return big.NewInt(0), nil
	}
	t.Cleanup(func() {
		getERC20Balance = originalGetERC20Balance
		getERC20Allowance = originalGetERC20Allowance
		isERC1155ApprovedForAll = originalIsERC1155ApprovedForAll
		getERC1155Balance = originalGetERC1155Balance
	})

	stdout, stderr, err := executeCLI(
		t,
		"--json",
		"--api-url", server.URL+"/api/cli",
		"clob",
		"--projection-url", server.URL,
		"smoke",
		"clob-1",
	)
	if err != nil {
		t.Fatalf("clob smoke dry-run error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "\"mode\": \"dry-run\"") || !strings.Contains(stdout, "\"marketId\": \"clob-1\"") {
		t.Fatalf("clob smoke dry-run stdout missing smoke payload\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "primaryWallet") || !strings.Contains(stdout, "order") || !strings.Contains(stdout, "\"account\": \"default\"") {
		t.Fatalf("clob smoke dry-run stdout missing wallet or order sections\nstdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "0x00000000000000000000000000000000000000C1") {
		t.Fatalf("clob smoke dry-run stdout missing binding address\nstdout:\n%s", stdout)
	}
}

func TestClobSmokeLiveSubmitsAndCancelsOrder(t *testing.T) {
	setCLIEnv(t)
	server, state := newMockAPIServer(t)
	defer server.Close()
	state.clobConflictsLeft = 2

	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"); err != nil {
		t.Fatalf("wallet import default error = %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--account", "trader-two", "--private-key", "59c6995e998f97a5a0044966f094538e1d7dff27c1d312bb7f6d1ab8d1b2b5d7"); err != nil {
		t.Fatalf("wallet import trader-two error = %v\nstderr:\n%s", err, stderr)
	}

	originalGetERC20Balance := getERC20Balance
	originalGetERC20Allowance := getERC20Allowance
	originalIsERC1155ApprovedForAll := isERC1155ApprovedForAll
	originalGetERC1155Balance := getERC1155Balance
	getERC20Balance = func(_ context.Context, _ string, _ common.Address, _ common.Address) (*big.Int, error) {
		return big.NewInt(1_000_000), nil
	}
	getERC20Allowance = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ common.Address) (*big.Int, error) {
		return big.NewInt(1_000_000), nil
	}
	isERC1155ApprovedForAll = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ common.Address) (bool, error) {
		return true, nil
	}
	getERC1155Balance = func(_ context.Context, _ string, _ common.Address, _ common.Address, _ *big.Int) (*big.Int, error) {
		return big.NewInt(0), nil
	}
	t.Cleanup(func() {
		getERC20Balance = originalGetERC20Balance
		getERC20Allowance = originalGetERC20Allowance
		isERC1155ApprovedForAll = originalIsERC1155ApprovedForAll
		getERC1155Balance = originalGetERC1155Balance
	})

	stdout, stderr, err := executeCLI(
		t,
		"--json",
		"--api-url", server.URL+"/api/cli",
		"clob",
		"--projection-url", server.URL,
		"--eventstore-url", server.URL+"/api",
		"smoke",
		"clob-1",
		"--secondary-account", "trader-two",
		"--live",
	)
	if err != nil {
		t.Fatalf("clob smoke live error = %v\nstderr:\n%s", err, stderr)
	}
	for _, want := range []string{"\"mode\": \"live\"", "submission", "fetchedOrder", "cancel", "secondaryWallet"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("clob smoke live stdout missing %q\nstdout:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "\"orderId\": \"order-1\"") {
		t.Fatalf("clob smoke live stdout missing order id\nstdout:\n%s", stdout)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.clobSubmitCalls != 3 {
		t.Fatalf("clob submit calls = %d, want 3 after two retryable conflicts", state.clobSubmitCalls)
	}
}

func TestMarketsListMyPositionsFiltersAndIncludesSpotPrices(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	originalLoadMarketState := loadMarketState
	loadMarketState = func(_ context.Context, _ string, _ common.Address) (*evm.MarketState, error) {
		return &evm.MarketState{
			TotalPool:        testBigInt("200000000000000000000"),
			TotalVirtualPool: testBigInt("100000000000000000000"),
			OutcomePools: []*big.Int{
				testBigInt("100000000000000000000"),
				testBigInt("100000000000000000000"),
			},
			VirtualSeeds: []*big.Int{
				testBigInt("50000000000000000000"),
				testBigInt("50000000000000000000"),
			},
		}, nil
	}
	t.Cleanup(func() {
		loadMarketState = originalLoadMarketState
	})

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "markets", "list", "--my-positions", "--spot-prices")
	if err != nil {
		t.Fatalf("markets list --my-positions error = %v\nstderr:\n%s", err, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal(markets list stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one filtered market", payload["items"])
	}
	market, ok := items[0].(map[string]any)
	if !ok || market["id"] != "market-1" {
		t.Fatalf("market item = %#v, want market-1", items[0])
	}
	prices, ok := market["currentSpotPrices"].([]any)
	if !ok || len(prices) != 2 {
		t.Fatalf("currentSpotPrices = %#v, want two spot-price entries", market["currentSpotPrices"])
	}
}

func TestProfileUpdateSendsSignedMetadata(t *testing.T) {
	setCLIEnv(t)
	server, state := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := executeCLI(
		t,
		"--json",
		"--api-url", server.URL+"/api/cli",
		"profile", "update",
		"--display-name", "agent-zero",
		"--avatar-url", "https://example.com/avatar.png",
	)
	if err != nil {
		t.Fatalf("profile update error = %v\nstderr:\n%s", err, stderr)
	}

	state.mu.Lock()
	request := state.lastProfileUpdate
	state.mu.Unlock()
	if request.DisplayName == nil || *request.DisplayName != "agent-zero" {
		t.Fatalf("displayName = %#v, want agent-zero", request.DisplayName)
	}
	if request.AvatarURL == nil || *request.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("avatarUrl = %#v, want https://example.com/avatar.png", request.AvatarURL)
	}
	if request.Signature == "" {
		t.Fatal("signature = empty, want signed profile update")
	}
	if !strings.Contains(stdout, "agent-zero") || !strings.Contains(stdout, "https://example.com/avatar.png") {
		t.Fatalf("profile update stdout missing updated profile fields\nstdout:\n%s", stdout)
	}
}

func TestRewardsShowAndClaimCommands(t *testing.T) {
	setCLIEnv(t)
	server, state := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "show")
	if err != nil {
		t.Fatalf("rewards show error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "totalClaimableEpochRewardsXrp") || !strings.Contains(stdout, "dailyChestClaimed") || !strings.Contains(stdout, "referralCode") {
		t.Fatalf("rewards show stdout missing rewards fields\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--api-url", server.URL+"/api/cli", "rewards", "show")
	if err != nil {
		t.Fatalf("rewards show text error = %v\nstderr:\n%s", err, stderr)
	}
	for _, want := range []string{
		"Referral Code",
		"default-alpha",
		"Predict using $5+",
		"Post on X tagging @AxiomProtocol_",
		"Place a bet of $10+ on a single outcome",
		"Claim winnings from a resolved market",
		"Place bets of $1+ on 3+ different markets",
		"Run axiom markets list, then axiom predict buy",
		"Complete this task in the web app rewards view",
		"Run axiom profile unclaimed, then axiom claim batch",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rewards show text stdout missing %q\nstdout:\n%s", want, stdout)
		}
	}

	originalClaimEpochRewards := claimEpochRewards
	var claimedContract common.Address
	claimEpochRewards = func(_ context.Context, _ string, _ *big.Int, _ string, contractAddress common.Address, _ *big.Int, _ *big.Int, _ []common.Hash) (common.Hash, error) {
		claimedContract = contractAddress
		return common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"), nil
	}
	originalWaitForReceipt := waitForTxReceipt
	waitForTxReceipt = func(_ context.Context, _ string, txHash common.Hash) (*types.Receipt, error) {
		return &types.Receipt{TxHash: txHash, Status: 1}, nil
	}
	t.Cleanup(func() {
		claimEpochRewards = originalClaimEpochRewards
		waitForTxReceipt = originalWaitForReceipt
	})

	if _, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "claim", "daily"); err != nil {
		t.Fatalf("rewards claim daily error = %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "claim", "weekly"); err != nil {
		t.Fatalf("rewards claim weekly error = %v\nstderr:\n%s", err, stderr)
	}
	if _, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "claim", "epoch"); err != nil {
		t.Fatalf("rewards claim epoch error = %v\nstderr:\n%s", err, stderr)
	}

	state.mu.Lock()
	lastPath := state.lastRewardsPath
	lastAction := state.lastRewardsAction
	state.mu.Unlock()
	if !strings.Contains(lastPath, "/rewards/epochs/12") {
		t.Fatalf("last rewards path = %q, want epoch sync path", lastPath)
	}
	if lastAction.TxHash == "" || lastAction.Signature == "" {
		t.Fatalf("last rewards action = %+v, want signed sync payload with tx hash", lastAction)
	}
	if claimedContract.Hex() != "0x00000000000000000000000000000000000000AA" {
		t.Fatalf("claimEpochRewards contract = %q, want config-provided rewards contract", claimedContract.Hex())
	}
}

func TestRewardsClaimEpochRecoveryUsesProvidedTxHash(t *testing.T) {
	setCLIEnv(t)
	server, state := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	originalClaimEpochRewards := claimEpochRewards
	claimCalls := 0
	claimEpochRewards = func(_ context.Context, _ string, _ *big.Int, _ string, _ common.Address, _ *big.Int, _ *big.Int, _ []common.Hash) (common.Hash, error) {
		claimCalls++
		return common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"), nil
	}
	originalWaitForReceipt := waitForTxReceipt
	waitForTxReceipt = func(_ context.Context, _ string, txHash common.Hash) (*types.Receipt, error) {
		return &types.Receipt{TxHash: txHash, Status: 1}, nil
	}
	t.Cleanup(func() {
		claimEpochRewards = originalClaimEpochRewards
		waitForTxReceipt = originalWaitForReceipt
	})

	state.mu.Lock()
	state.rewardsSyncStatus = http.StatusBadGateway
	state.rewardsSyncError = "sync unavailable"
	state.mu.Unlock()

	_, _, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "claim", "epoch")
	if err == nil {
		t.Fatal("rewards claim epoch error = nil, want sync recovery error")
	}
	if !strings.Contains(err.Error(), "claimed on-chain in tx 0x1111111111111111111111111111111111111111111111111111111111111111") {
		t.Fatalf("rewards claim epoch error = %q, want on-chain recovery context", err)
	}
	if !strings.Contains(err.Error(), "axiom rewards claim epoch 12 --tx-hash 0x1111111111111111111111111111111111111111111111111111111111111111") {
		t.Fatalf("rewards claim epoch error = %q, want recovery command", err)
	}
	if claimCalls != 1 {
		t.Fatalf("claimEpochRewards calls = %d, want 1 after failed sync", claimCalls)
	}

	state.mu.Lock()
	state.rewardsSyncStatus = 0
	state.rewardsSyncError = ""
	state.mu.Unlock()

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "rewards", "claim", "epoch", "12", "--tx-hash", "0x1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("rewards claim epoch recovery error = %v\nstderr:\n%s", err, stderr)
	}
	if claimCalls != 1 {
		t.Fatalf("claimEpochRewards calls = %d, want sync-only retry to skip another on-chain claim", claimCalls)
	}
	if !strings.Contains(stdout, "0x1111111111111111111111111111111111111111111111111111111111111111") {
		t.Fatalf("rewards claim epoch recovery stdout missing tx hash\nstdout:\n%s", stdout)
	}
}

func TestClaimBatchReturnsClaimedMarketDetails(t *testing.T) {
	setCLIEnv(t)
	server, _ := newMockAPIServer(t)
	defer server.Close()

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	originalBatchClaim := batchClaimMarkets
	batchClaimMarkets = func(_ context.Context, _ string, _ *big.Int, _ string, _ common.Address, _ []common.Address) (common.Hash, error) {
		return common.HexToHash("0x123"), nil
	}
	t.Cleanup(func() {
		batchClaimMarkets = originalBatchClaim
	})

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "claim", "batch")
	if err != nil {
		t.Fatalf("claim batch error = %v\nstderr:\n%s", err, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal(claim batch stdout) error = %v\nstdout:\n%s", err, stdout)
	}
	if payload["totalClaimedPayoutUsd"] != "22.00" {
		t.Fatalf("totalClaimedPayoutUsd = %#v, want %q", payload["totalClaimedPayoutUsd"], "22.00")
	}
	claimed, ok := payload["claimedMarkets"].([]any)
	if !ok || len(claimed) != 1 {
		t.Fatalf("claimedMarkets = %#v, want one market", payload["claimedMarkets"])
	}
}

func TestWalletBalanceFlagsSelectNetworks(t *testing.T) {
	setCLIEnv(t)

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	if _, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey); err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}
	xrplWallet, err := axrpl.NewRandomWallet()
	if err != nil {
		t.Fatalf("NewRandomWallet() error = %v", err)
	}
	if _, stderr, err := executeCLI(t, "--json", "wallet", "xrpl-import", "--seed", xrplWallet.Seed()); err != nil {
		t.Fatalf("wallet xrpl-import error = %v\nstderr:\n%s", err, stderr)
	}

	originalEVMBalance := getEVMBalance
	originalXRPLBalance := getXRPLBalance
	getEVMBalance = func(_ context.Context, _ string, _ common.Address) (*big.Int, error) {
		return testBigInt("1500000000000000000"), nil
	}
	getXRPLBalance = func(_ context.Context, _ string, _ string) (string, error) {
		return "25", nil
	}
	t.Cleanup(func() {
		getEVMBalance = originalEVMBalance
		getXRPLBalance = originalXRPLBalance
	})

	stdout, stderr, err := executeCLI(t, "--json", "wallet", "balance", "--evm")
	if err != nil {
		t.Fatalf("wallet balance --evm error = %v\nstderr:\n%s", err, stderr)
	}
	if strings.Contains(stdout, "xrplBalanceXrp") || !strings.Contains(stdout, "evmBalanceXrp") {
		t.Fatalf("wallet balance --evm stdout = %s, want only evm balance", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "wallet", "balance", "--xrpl")
	if err != nil {
		t.Fatalf("wallet balance --xrpl error = %v\nstderr:\n%s", err, stderr)
	}
	if strings.Contains(stdout, "evmBalanceXrp") || !strings.Contains(stdout, "xrplBalanceXrp") {
		t.Fatalf("wallet balance --xrpl stdout = %s, want only xrpl balance", stdout)
	}
}

func TestAuthRegisterUsesMillisecondPrecisionIssuedAt(t *testing.T) {
	setCLIEnv(t)

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	_, err := evm.WalletFromPrivateKeyHex(privateKey)
	if err != nil {
		t.Fatalf("WalletFromPrivateKeyHex() error = %v", err)
	}

	var request api.RegisterRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/cli/register":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(register body) error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(api.RegisterResponse{
				WalletAddress:         request.WalletAddress,
				DisplayName:           "default",
				ReferralCode:          "default-alpha",
				DepositDestinationTag: 4242,
				Created:               true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, stderr, err := executeCLI(t, "--json", "wallet", "import", "--private-key", privateKey)
	if err != nil {
		t.Fatalf("wallet import error = %v\nstderr:\n%s", err, stderr)
	}

	stdout, stderr, err := executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "auth", "register")
	if err != nil {
		t.Fatalf("auth register error = %v\nstderr:\n%s", err, stderr)
	}
	if matched, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, request.IssuedAt); err != nil {
		t.Fatalf("regexp.MatchString() error = %v", err)
	} else if !matched {
		t.Fatalf("issuedAt = %q, want millisecond-precision ISO timestamp", request.IssuedAt)
	}
	if !strings.Contains(stdout, request.WalletAddress) || !strings.Contains(stdout, "4242") {
		t.Fatalf("auth register stdout missing successful payload\nstdout:\n%s", stdout)
	}
}

func TestCommandHelperFunctions(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	message := buildRegistrationMessage("0xabc", "device-123", issuedAt)
	if !strings.Contains(message, "Wallet: 0xabc") || !strings.Contains(message, "Device: device-123") {
		t.Fatalf("buildRegistrationMessage() = %q, want wallet and device markers", message)
	}
	if !strings.Contains(message, "Issued At: 2026-03-10T00:00:00.000Z") {
		t.Fatalf("buildRegistrationMessage() = %q, want millisecond-precision issued-at line", message)
	}
	formattedIssuedAt := formatRegistrationIssuedAt(time.Date(2026, time.March, 10, 0, 0, 0, 123456789, time.UTC))
	if formattedIssuedAt != "2026-03-10T00:00:00.123Z" {
		t.Fatalf("formatRegistrationIssuedAt() = %q, want %q", formattedIssuedAt, "2026-03-10T00:00:00.123Z")
	}

	wei, err := parseXRPToWei("1.5")
	if err != nil {
		t.Fatalf("parseXRPToWei() error = %v", err)
	}
	if got := formatWeiToXRP(wei); got != "1.500000" {
		t.Fatalf("formatWeiToXRP(parseXRPToWei()) = %q, want %q", got, "1.500000")
	}
	if _, err := parseXRPToWei("1.1234567890123456789"); err == nil {
		t.Fatal("parseXRPToWei() error = nil, want decimal precision validation")
	}

	uri := buildXRPLPaymentURI("rDepositWallet", 4242, "25")
	if uri != "xrpl:rDepositWallet?dt=4242&amount=25" {
		t.Fatalf("buildXRPLPaymentURI() = %q, want %q", uri, "xrpl:rDepositWallet?dt=4242&amount=25")
	}

	preview := renderBridgeFundingPreview(bridgeFundingPreview{
		DepositWalletAddress: "rDepositWallet",
		DestinationTag:       4242,
		AmountXRP:            "25",
		PaymentURI:           uri,
		QRCode:               "██",
		Instructions:         []string{"Send XRP.", "Include destination tag."},
	})
	if !strings.Contains(preview, "Bridge Funding QR") || !strings.Contains(preview, "Send XRP.") {
		t.Fatalf("renderBridgeFundingPreview() missing sections\npreview:\n%s", preview)
	}

	filtered := filterMarketsByCategory(&api.MarketsResponse{
		Items: []api.MarketListItem{
			{ID: "1", Category: "crypto"},
			{ID: "2", Category: "sports"},
			{ID: "3", Category: "crypto"},
		},
	}, "crypto", 1, 1)
	if len(filtered.Items) != 1 || filtered.Items[0].ID != "3" || filtered.Total != 2 {
		t.Fatalf("filterMarketsByCategory() = %+v, want one paged crypto item with total 2", filtered)
	}
}

func executeCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetCLIFlags()
	cmd := newRootCommand()
	cmd.SetArgs(normalizeCLIArgs(args))
	stdout, stderr, err := captureStdIO(func() error {
		return cmd.Execute()
	})
	resetCLIFlags()
	return stdout, stderr, err
}

func captureStdIO(run func() error) (string, string, error) {
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stdoutBuf, stdoutReader)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderrBuf, stderrReader)
	}()

	err := run()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	wg.Wait()
	return stdoutBuf.String(), stderrBuf.String(), err
}

func setCLIEnv(t *testing.T) {
	t.Helper()
	configHome := t.TempDir()
	t.Setenv("APPDATA", configHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", configHome)
	t.Setenv("AXIOM_CLI_SECRET_STORE", "file")
	t.Setenv("AXIOM_CLI_SECRET_PASSPHRASE", "test-passphrase")
}

func resetCLIFlags() {
	flagAPIURL = ""
	flagRPCURL = ""
	flagXRPLURL = ""
	flagJSON = false
	flagProfile = ""
}

type mockAPIState struct {
	lastRegister      api.RegisterRequest
	lastProfileUpdate api.UpdateProfileRequest
	lastRewardsAction api.RewardsActionRequest
	lastRewardsPath   string
	lastDeviceHeader  string
	lastClobOrder     api.ClobSignedOrderPayload
	clobSubmitCalls   int
	clobConflictsLeft int
	rewardsSyncError  string
	rewardsSyncStatus int
	rewardsAddress    string
	clobOrders        map[string]api.ClobOrder
	clobFills         []api.ClobFill
	mu                sync.Mutex
}

func newMockAPIServer(t *testing.T) (*httptest.Server, *mockAPIState) {
	t.Helper()

	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	state := &mockAPIState{rewardsAddress: "0x00000000000000000000000000000000000000AA", clobOrders: make(map[string]api.ClobOrder)}
	markets := []api.MarketListItem{
		{
			ID:              "market-0",
			MarketType:      "binary",
			Title:           "Will BTC reach $120k?",
			Category:        "crypto",
			Status:          "active",
			StartsAt:        now.Add(-2 * time.Hour),
			EndsAt:          now.Add(2 * time.Hour),
			ContractAddress: "0x0000000000000000000000000000000000000001",
			Outcomes:        []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
		},
		{
			ID:              "market-1",
			MarketType:      "binary",
			Title:           "Will XRP close above $3.00?",
			Category:        "crypto",
			Status:          "active",
			StartsAt:        now.Add(-1 * time.Hour),
			EndsAt:          now.Add(1 * time.Hour),
			ContractAddress: "0x0000000000000000000000000000000000000002",
			Outcomes:        []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
		},
		{
			ID:              "market-2",
			MarketType:      "binary",
			Title:           "Will the Lakers win tonight?",
			Category:        "sports",
			Status:          "active",
			StartsAt:        now.Add(-3 * time.Hour),
			EndsAt:          now.Add(3 * time.Hour),
			ContractAddress: "0x0000000000000000000000000000000000000003",
			Outcomes:        []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/books/") && strings.HasSuffix(r.URL.Path, "/depth"):
			_ = json.NewEncoder(w).Encode(api.ClobDepth{
				Bids: []api.ClobDepthLevel{{ClobID: "clob-1-0", Side: "buy", Price: 45, TotalQty: 12, OrderCount: 2}},
				Asks: []api.ClobDepthLevel{{ClobID: "clob-1-0", Side: "sell", Price: 55, TotalQty: 8, OrderCount: 1}},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/books/"):
			_ = json.NewEncoder(w).Encode(api.ClobBook{ClobID: "clob-1-0", MarketID: "clob-1", Outcome: 0, Creator: "0xcreator", Status: "open", BidCount: 2, AskCount: 1, TradeCount: 0, Volume24h: 0, EventSequence: 1, CreatedAt: ptrTime(now), UpdatedAt: ptrTime(now)})
		case r.Method == http.MethodPost && r.URL.Path == "/api/orders":
			var body struct {
				SignedOrder api.ClobSignedOrderPayload `json:"signed_order"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode(clob submit body) error = %v", err)
			}
			state.mu.Lock()
			state.clobSubmitCalls++
			state.lastClobOrder = body.SignedOrder
			if state.clobConflictsLeft > 0 {
				state.clobConflictsLeft--
				state.mu.Unlock()
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "save aggregate: append events: version conflict: stream was modified"})
				break
			}
			orderID := fmt.Sprintf("order-%d", len(state.clobOrders)+1)
			price := 1
			state.clobOrders[orderID] = api.ClobOrder{OrderID: orderID, ClobID: fmt.Sprintf("%s-%d", body.SignedOrder.Market, body.SignedOrder.Outcome), Maker: body.SignedOrder.Maker, Side: "buy", OrderType: "limit", Price: &price, Quantity: 1, Remaining: 1, TotalFilled: 0, Status: "open", EventSequence: len(state.clobOrders) + 1, CreatedAt: ptrTime(now), UpdatedAt: ptrTime(now)}
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(api.ClobOrderResponse{OrderID: orderID, RemainingQuantity: 1, TradeCount: 0, WasAddedToBook: true})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/orders/"):
			orderID := filepath.Base(r.URL.Path)
			state.mu.Lock()
			order := state.clobOrders[orderID]
			order.Status = "cancelled"
			order.Remaining = 0
			order.UpdatedAt = ptrTime(now)
			state.clobOrders[orderID] = order
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(api.ClobOrderResponse{OrderID: orderID, RemainingQuantity: 0, TradeCount: 0, WasAddedToBook: false})
		case r.Method == http.MethodGet && r.URL.Path == "/orders":
			state.mu.Lock()
			orders := make([]api.ClobOrder, 0, len(state.clobOrders))
			for _, order := range state.clobOrders {
				if maker := strings.TrimSpace(r.URL.Query().Get("maker")); maker != "" && !strings.EqualFold(order.Maker, maker) {
					continue
				}
				if clobID := strings.TrimSpace(r.URL.Query().Get("clob_id")); clobID != "" && order.ClobID != clobID {
					continue
				}
				if activeOnly := strings.TrimSpace(r.URL.Query().Get("active_only")); activeOnly == "true" && order.Status != "open" {
					continue
				}
				orders = append(orders, order)
			}
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(orders)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/orders/"):
			orderID := filepath.Base(r.URL.Path)
			state.mu.Lock()
			order, ok := state.clobOrders[orderID]
			state.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				break
			}
			_ = json.NewEncoder(w).Encode(order)
		case r.Method == http.MethodGet && r.URL.Path == "/fills":
			state.mu.Lock()
			fills := append([]api.ClobFill(nil), state.clobFills...)
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(fills)
		case r.Method == http.MethodPost && r.URL.Path == "/api/cli/register":
			var request api.RegisterRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(register body) error = %v", err)
			}
			state.mu.Lock()
			state.lastRegister = request
			state.lastDeviceHeader = r.Header.Get("X-Axiom-CLI-Device")
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(api.RegisterResponse{
				WalletAddress:         request.WalletAddress,
				DisplayName:           "default",
				ReferralCode:          "default-alpha",
				DepositDestinationTag: 4242,
				Created:               true,
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rewards/daily-chest"):
			var request api.RewardsActionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(daily chest body) error = %v", err)
			}
			state.mu.Lock()
			state.lastRewardsAction = request
			state.lastRewardsPath = r.URL.Path
			state.lastDeviceHeader = r.Header.Get("X-Axiom-CLI-Device")
			state.mu.Unlock()
			prizeAmount := 500
			_ = json.NewEncoder(w).Encode(api.DailyChestClaimResponse{Success: true, PrizeAmount: &prizeAmount, PrizeLabel: "500 points"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rewards/lottery/"):
			var request api.RewardsActionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(weekly chest body) error = %v", err)
			}
			state.mu.Lock()
			state.lastRewardsAction = request
			state.lastRewardsPath = r.URL.Path
			state.lastDeviceHeader = r.Header.Get("X-Axiom-CLI-Device")
			state.mu.Unlock()
			prizeAmount := 2500
			_ = json.NewEncoder(w).Encode(api.WeeklyChestClaimResponse{Success: true, PrizeType: "points", PrizeAmount: &prizeAmount, PrizeLabel: "2500 points", IsConsolation: false, CashConvertedToPoints: false})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/rewards/epochs/"):
			var request api.RewardsActionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(epoch sync body) error = %v", err)
			}
			state.mu.Lock()
			state.lastRewardsAction = request
			state.lastRewardsPath = r.URL.Path
			state.lastDeviceHeader = r.Header.Get("X-Axiom-CLI-Device")
			rewardsSyncStatus := state.rewardsSyncStatus
			rewardsSyncError := state.rewardsSyncError
			state.mu.Unlock()
			if rewardsSyncStatus > 0 {
				w.WriteHeader(rewardsSyncStatus)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": rewardsSyncError})
				return
			}
			_ = json.NewEncoder(w).Encode(api.EpochRewardClaimResponse{
				Success:       true,
				WalletAddress: request.WalletAddress,
				EpochID:       12,
				TxHash:        request.TxHash,
				ClaimedReward: api.EpochReward{EpochID: 12, Points: 1200, AmountWei: "1000000000000000000", AmountXRP: "1", Proof: []string{"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, HasClaimed: true, DateEnded: now.Add(-24 * time.Hour), IsExpired: false, Claimable: false},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/cli/profile/"):
			var request api.UpdateProfileRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(profile update body) error = %v", err)
			}
			state.mu.Lock()
			state.lastProfileUpdate = request
			state.lastDeviceHeader = r.Header.Get("X-Axiom-CLI-Device")
			state.mu.Unlock()
			tag := 4242
			rank := 7
			displayName := "default"
			if request.DisplayName != nil {
				displayName = *request.DisplayName
			}
			avatarURL := ""
			if request.AvatarURL != nil {
				avatarURL = *request.AvatarURL
			}
			_ = json.NewEncoder(w).Encode(api.ProfileSummary{
				WalletAddress:         filepath.Base(r.URL.Path),
				DisplayName:           displayName,
				AvatarURL:             avatarURL,
				ReferralCode:          "default-alpha",
				DepositDestinationTag: &tag,
				MemberSince:           ptrTime(now.Add(-7 * 24 * time.Hour)),
				LastLoginAt:           ptrTime(now),
				Stats:                 api.ProfileStats{TotalPredictions: 12, ResolvedMarkets: 5, OpenMarkets: 7, UnclaimedMarkets: 1, UnclaimedPayoutUSD: "22.00", UnclaimedPnlUSD: "3.00", LeaderboardRank: &rank, PnlUSD: 4.56, PnlPercent: 7.89, VolumeUSD: 123.45, WinRate: 66.6, TradeCount: 12},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cli/markets":
			_ = json.NewEncoder(w).Encode(api.MarketsResponse{Items: markets, Total: len(markets), Limit: len(markets), Offset: 0})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cli/markets/market-1":
			_ = json.NewEncoder(w).Encode(api.MarketDetails{
				MarketListItem: api.MarketListItem{
					ID:              "market-1",
					MarketType:      "binary",
					Title:           "Will XRP close above $3.00?",
					Category:        "crypto",
					Status:          "active",
					StartsAt:        now.Add(-1 * time.Hour),
					EndsAt:          now.Add(1 * time.Hour),
					ContractAddress: "0x0000000000000000000000000000000000000002",
					Outcomes:        []api.Outcome{{Index: 0, Label: "Yes", Description: "Above $3.00"}, {Index: 1, Label: "No", Description: "At or below $3.00"}},
				},
				SettlementToken:    "0x0000000000000000000000000000000000000000",
				Creator:            "0xcreator",
				OwnerAddress:       "0xowner",
				ResolutionCriteria: "Close price above $3.00 on the candle.",
				Tags:               []string{"crypto", "daily"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cli/markets/clob-1":
			resolvedOutcomeIndex := 0
			_ = json.NewEncoder(w).Encode(api.MarketDetails{
				MarketListItem: api.MarketListItem{
					ID:                   "clob-1",
					MarketType:           "binary",
					MarketImplementation: "AxiomCTFMarket",
					Title:                "Will XRP close above $3.00 on Friday?",
					Category:             "crypto",
					Status:               "resolved",
					StartsAt:             now.Add(-24 * time.Hour),
					EndsAt:               now.Add(-2 * time.Hour),
					ContractAddress:      "0x00000000000000000000000000000000000000C1",
					IsResolved:           true,
					LogicalMarketAddresses: []string{
						"0x00000000000000000000000000000000000000C1",
						"0x00000000000000000000000000000000000000C2",
					},
					CTFOutcomeMarkets: []api.CtfOutcomeMarketBinding{
						{OutcomeID: "outcome-yes", OutcomeIndex: 0, Label: "Yes", ContractAddress: "0x00000000000000000000000000000000000000C1", OutcomeTokenIDs: []string{"101", "102"}, QuestionID: "question-yes", ConditionID: "condition-yes"},
						{OutcomeID: "outcome-no", OutcomeIndex: 1, Label: "No", ContractAddress: "0x00000000000000000000000000000000000000C2", OutcomeTokenIDs: []string{"201", "202"}, QuestionID: "question-no", ConditionID: "condition-no"},
					},
					Outcomes: []api.Outcome{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
				},
				SettlementToken:      "0x0000000000000000000000000000000000000000",
				Creator:              "0xcreator",
				OwnerAddress:         "0xowner",
				ResolvedOutcomeIndex: &resolvedOutcomeIndex,
				ResolutionCriteria:   "Friday close must settle above $3.00.",
				Tags:                 []string{"crypto", "clob"},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/cli/profile/") && strings.HasSuffix(r.URL.Path, "/rewards"):
			epochID := 12
			endsAt := now.Add(24 * time.Hour)
			poolXRP := 42.5
			estimatedPayout := 1.75
			poolShare := 4.12
			claimTime := now.Add(-24 * time.Hour)
			_ = json.NewEncoder(w).Encode(api.RewardsResponse{
				WalletAddress: filepath.Base(filepath.Dir(r.URL.Path)),
				Summary: &api.RewardsSummary{
					Address:             filepath.Base(filepath.Dir(r.URL.Path)),
					ReferralCode:        "default-alpha",
					TotalReferrals:      2,
					CurrentEpochID:      &epochID,
					CurrentEpochEndsAt:  &endsAt,
					CurrentEpochPoints:  1200,
					TradingPoints:       1000,
					ReferralPoints:      150,
					BonusPoints:         50,
					PoolXRP:             &poolXRP,
					GlobalTotalPoints:   ptrInt(10000),
					PoolSharePercentage: &poolShare,
					EstimatedPayoutXRP:  &estimatedPayout,
				},
				DailyTasks:                    &api.DailyTaskStatus{HasPredictTask: true, HasDailyTwitterPostTask: false, HasBigBetTask: true, HasClaimWinningsTask: false, HasMultiMarketTask: true, CompletedCount: 3, RequiredCount: 3, HasCompletedRequirement: true, DailyChestClaimed: false},
				Streak:                        &api.RewardsStreak{CurrentStreak: 7, LongestStreak: 9, LastActivityDate: &claimTime, DaysUntilLottery: 0, HasAvailableLotteryTicket: true, CompletedDailyTasksCount: 3, RequiredDailyTasksCount: 3, HasCompletedDailyTaskRequirement: true, HasCompletedDailyBetTask: true, HasCompletedDailyTwitterPostTask: false, HasCompletedBigBetTask: true, HasCompletedClaimWinningsTask: false, HasCompletedMultiMarketTask: true},
				LotteryTickets:                []api.LotteryTicketInfo{{ID: 77, Status: "available", EarnedAt: now.Add(-2 * time.Hour)}},
				EpochRewards:                  []api.EpochReward{{EpochID: 12, Points: 1200, AmountWei: "1000000000000000000", AmountXRP: "1", Proof: []string{"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, HasClaimed: false, DateEnded: now.Add(-24 * time.Hour), IsExpired: false, Claimable: true}},
				TotalClaimableEpochRewardsXRP: "1",
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/cli/profile/") && strings.HasSuffix(r.URL.Path, "/positions"):
			_ = json.NewEncoder(w).Encode(api.PositionsResponse{Items: []api.PositionItem{{MarketID: "market-1", MarketAddress: "0x0000000000000000000000000000000000000002", Title: "Will XRP close above $3.00?", Status: "active", OutcomeIndex: 0, OutcomeLabel: "Yes", AmountUSD: "25.00", Shares: "100", CreatedAt: now}}, Total: 1})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/cli/profile/") && strings.HasSuffix(r.URL.Path, "/unclaimed"):
			response := api.UnclaimedResponse{}
			response.Summary.TotalUnclaimedPayoutUSD = "22.00"
			response.Summary.TotalUnclaimedPnlUSD = "3.00"
			response.Summary.TotalCount = 1
			response.Summary.MarketCount = 1
			response.Summary.SeriesCount = 0
			response.Items = []api.UnclaimedItem{{MarketID: "market-1", MarketAddress: "0x0000000000000000000000000000000000000002", Title: "Will XRP close above $3.00?", PayoutUSD: "22.00", PnlUSD: "3.00", ResolvedOutcome: 0, ResolvedAt: now}}
			_ = json.NewEncoder(w).Encode(response)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/cli/profile/"):
			tag := 4242
			rank := 7
			_ = json.NewEncoder(w).Encode(api.ProfileSummary{
				WalletAddress:         filepath.Base(r.URL.Path),
				DisplayName:           "default",
				ReferralCode:          "default-alpha",
				DepositDestinationTag: &tag,
				MemberSince:           ptrTime(now.Add(-7 * 24 * time.Hour)),
				LastLoginAt:           ptrTime(now),
				Stats:                 api.ProfileStats{TotalPredictions: 12, ResolvedMarkets: 5, OpenMarkets: 7, UnclaimedMarkets: 1, UnclaimedPayoutUSD: "22.00", UnclaimedPnlUSD: "3.00", LeaderboardRank: &rank, PnlUSD: 4.56, PnlPercent: 7.89, VolumeUSD: 123.45, WinRate: 66.6, TradeCount: 12},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/cli/funding/"):
			tag := 4242
			_ = json.NewEncoder(w).Encode(api.FundingResponse{
				WalletAddress:         filepath.Base(r.URL.Path),
				DepositDestinationTag: &tag,
				DepositWalletAddress:  "rDepositWallet",
				Notes:                 []string{"Send XRP on XRPL with the destination tag shown below."},
				RecentHistory:         []api.FundingHistoryItem{{Kind: "bridge", Status: "completed", AmountXRP: "10", TxHash: "0xhash", BridgeTxHash: "0xbridge", CreatedAt: now, UpdatedAt: now}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cli/config":
			state.mu.Lock()
			rewardsAddress := state.rewardsAddress
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(api.ConfigResponse{APIVersion: "v1", Network: "xrpl-mainnet", ChainID: 1440000, NativeSymbol: "XRP", RPCURL: "https://rpc.xrplevm.org", ExplorerBaseURL: "https://explorer.xrplevm.org", AxiomUtilityAddress: "0xutility", AxiomRewardsAddress: rewardsAddress, DepositWalletAddress: "rDepositWallet"})
		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(handler), state
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func ptrInt(value int) *int {
	return &value
}

func testBigInt(value string) *big.Int {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		panic("invalid big int: " + value)
	}
	return parsed
}
