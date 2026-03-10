package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
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
		{args: []string{"funding", "--help"}, want: "Handle direct XRP funding"},
		{args: []string{"funding", "info", "--help"}, want: "Show funding instructions"},
		{args: []string{"funding", "direct", "--help"}, want: "Send native XRP on XRPL EVM directly"},
		{args: []string{"funding", "bridge", "--help"}, want: "Prepare XRPL relay funding for your Axiom wallet."},
		{args: []string{"predict", "--help"}, want: "Place predictions on Axiom markets"},
		{args: []string{"predict", "quote", "--help"}, want: "Preview weighted shares"},
		{args: []string{"predict", "buy", "--help"}, want: "Buy into an Axiom market outcome"},
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

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "auth", "register")
	if err != nil {
		t.Fatalf("auth register error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "4242") {
		t.Fatalf("auth register stdout missing destination tag\nstdout:\n%s", stdout)
	}
	if state.lastRegister.WalletAddress != wallet.Address().Hex() {
		t.Fatalf("register wallet = %q, want %q", state.lastRegister.WalletAddress, wallet.Address().Hex())
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
	if !strings.Contains(stdout, wallet.Address().Hex()) || !strings.Contains(stdout, "default") {
		t.Fatalf("profile show stdout missing profile data\nstdout:\n%s", stdout)
	}

	stdout, stderr, err = executeCLI(t, "--json", "--api-url", server.URL+"/api/cli", "profile", "positions")
	if err != nil {
		t.Fatalf("profile positions error = %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "positions") && !strings.Contains(stdout, "marketId") {
		// JSON output should contain field names from the response payload.
		t.Fatalf("profile positions stdout missing JSON payload\nstdout:\n%s", stdout)
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
}

func TestAuthRegisterRetriesWithLowercaseWalletAddress(t *testing.T) {
	setCLIEnv(t)

	privateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	wallet, err := evm.WalletFromPrivateKeyHex(privateKey)
	if err != nil {
		t.Fatalf("WalletFromPrivateKeyHex() error = %v", err)
	}

	var requests []api.RegisterRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/cli/register":
			var request api.RegisterRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(register body) error = %v", err)
			}
			requests = append(requests, request)
			if request.WalletAddress != strings.ToLower(request.WalletAddress) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"Invalid wallet signature."}`))
				return
			}
			_ = json.NewEncoder(w).Encode(api.RegisterResponse{
				WalletAddress:         request.WalletAddress,
				DisplayName:           "default",
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
	if len(requests) != 2 {
		t.Fatalf("register request count = %d, want 2", len(requests))
	}
	if requests[0].WalletAddress != wallet.Address().Hex() {
		t.Fatalf("first walletAddress = %q, want %q", requests[0].WalletAddress, wallet.Address().Hex())
	}
	if requests[1].WalletAddress != strings.ToLower(wallet.Address().Hex()) {
		t.Fatalf("second walletAddress = %q, want %q", requests[1].WalletAddress, strings.ToLower(wallet.Address().Hex()))
	}
	if requests[0].IssuedAt != requests[1].IssuedAt {
		t.Fatalf("issuedAt mismatch between retries: %q vs %q", requests[0].IssuedAt, requests[1].IssuedAt)
	}
	if !strings.Contains(stdout, strings.ToLower(wallet.Address().Hex())) || !strings.Contains(stdout, "4242") {
		t.Fatalf("auth register stdout missing successful retry payload\nstdout:\n%s", stdout)
	}
}

func TestCommandHelperFunctions(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	message := buildRegistrationMessage("0xabc", "device-123", issuedAt)
	if !strings.Contains(message, "Wallet: 0xabc") || !strings.Contains(message, "Device: device-123") {
		t.Fatalf("buildRegistrationMessage() = %q, want wallet and device markers", message)
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
	cmd.SetArgs(args)
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
	t.Setenv("APPDATA", t.TempDir())
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
	lastRegister     api.RegisterRequest
	lastDeviceHeader string
	mu               sync.Mutex
}

func newMockAPIServer(t *testing.T) (*httptest.Server, *mockAPIState) {
	t.Helper()

	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	state := &mockAPIState{}
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
				DepositDestinationTag: 4242,
				Created:               true,
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
				DepositDestinationTag: &tag,
				MemberSince:           ptrTime(now.Add(-7 * 24 * time.Hour)),
				LastLoginAt:           ptrTime(now),
				Stats: struct {
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
				}{TotalPredictions: 12, ResolvedMarkets: 5, OpenMarkets: 7, UnclaimedMarkets: 1, UnclaimedPayoutUSD: "22.00", UnclaimedPnlUSD: "3.00", LeaderboardRank: &rank, PnlUSD: 4.56, PnlPercent: 7.89, VolumeUSD: 123.45, WinRate: 66.6, TradeCount: 12},
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
			_ = json.NewEncoder(w).Encode(api.ConfigResponse{APIVersion: "v1", Network: "xrpl-mainnet", ChainID: 1440000, NativeSymbol: "XRP", RPCURL: "https://rpc.xrplevm.org", ExplorerBaseURL: "https://explorer.xrplevm.org", AxiomUtilityAddress: "0xutility", DepositWalletAddress: "rDepositWallet"})
		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(handler), state
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
