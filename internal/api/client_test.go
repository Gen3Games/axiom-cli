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

	response, err := client.ListAllMarkets(context.Background(), "active", "", "", "", 0)
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
	if !strings.Contains(err.Error(), "local API unreachable") {
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

func TestGetMarketContractAddressesUsesAppRootEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"network": "xrpl-mainnet",
			"addresses": map[string]any{
				"marketFactory":     "0x00000000000000000000000000000000000000F1",
				"protocolConfig":    "0x00000000000000000000000000000000000000F2",
				"vaultRegistry":     "0x00000000000000000000000000000000000000F3",
				"ctfExchange":       "0x00000000000000000000000000000000000000F4",
				"ctfLauncher":       "0x00000000000000000000000000000000000000F5",
				"conditionalTokens": "0x00000000000000000000000000000000000000F6",
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.GetMarketContractAddresses(context.Background(), "xrpl-mainnet")
	if err != nil {
		t.Fatalf("GetMarketContractAddresses() error = %v", err)
	}
	if gotPath != "/api/markets/contract-addresses?network=xrpl-mainnet" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/markets/contract-addresses?network=xrpl-mainnet")
	}
	if response.MarketFactory != "0x00000000000000000000000000000000000000F1" {
		t.Fatalf("MarketFactory = %q, want canonical factory address", response.MarketFactory)
	}
	if response.ConditionalTokens != "0x00000000000000000000000000000000000000F6" {
		t.Fatalf("ConditionalTokens = %q, want canonical conditional tokens address", response.ConditionalTokens)
	}
}

func TestUploadMarketMetadataUsesAppRootEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRequest UploadMetadataRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(upload body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UploadMetadataResponse{
			Success:       true,
			Network:       "xrpl-mainnet",
			SignerAddress: gotRequest.WalletAddress,
			CID:           "bafytest",
			IPFSURI:       "ipfs://bafytest",
			GatewayURL:    "https://axiom.mypinata.cloud/ipfs/bafytest",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.UploadMarketMetadata(context.Background(), UploadMetadataRequest{
		Network:       "xrpl-mainnet",
		WalletAddress: "0x00000000000000000000000000000000000000A1",
		Metadata: MarketMetadata{
			Name:               "Upload Test",
			Description:        "Test payload",
			Category:           "crypto",
			Tags:               []string{},
			Outcomes:           []OutcomeMetadata{{Index: 0, Label: "Yes"}, {Index: 1, Label: "No"}},
			ResolutionCriteria: "Test only",
			CreatedAt:          "2026-01-01T00:00:00Z",
			EndsAt:             "2026-01-02T00:00:00Z",
			OutcomeCount:       2,
		},
		Message:   "signed message",
		Signature: "0xabc",
	})
	if err != nil {
		t.Fatalf("UploadMarketMetadata() error = %v", err)
	}
	if gotPath != "/api/markets/upload-metadata" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/markets/upload-metadata")
	}
	if gotRequest.Metadata.Name != "Upload Test" {
		t.Fatalf("request metadata = %+v, want uploaded payload", gotRequest.Metadata)
	}
	if response.IPFSURI != "ipfs://bafytest" {
		t.Fatalf("IPFSURI = %q, want %q", response.IPFSURI, "ipfs://bafytest")
	}
}

func TestRegisterClobMarketUsesAppRootEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRequest RegisterClobMarketRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(register-clob body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RegisterClobMarketResponse{
			Success:       true,
			MarketID:      gotRequest.MarketID,
			SignerAddress: "0x00000000000000000000000000000000000000A1",
			RegisteredContracts: []RegisteredClobContract{{
				ContractAddress: "0x00000000000000000000000000000000000000C1",
				OutcomeIndex:    0,
				OutcomeLabel:    "Yes",
				OutcomeTokenIDs: []string{"101", "102"},
			}},
			BooksCreated: 2,
			BooksTotal:   2,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.RegisterClobMarket(context.Background(), RegisterClobMarketRequest{
		MarketID:       "logical-market-1",
		Network:        "xrpl-mainnet",
		ChainID:        1440000,
		RPCURL:         "https://rpc.xrplevm.org",
		Addresses:      []string{"0x00000000000000000000000000000000000000C1"},
		IsVisible:      true,
		AllowUnindexed: true,
		Metadata: RegisterClobMarketMetadata{
			Name:            "Logical Market",
			Category:        "crypto",
			MarketType:      "yes_no",
			EvidenceSources: []string{"https://example.com/rules"},
			Image:           "ipfs://logical-market-image",
			StartsAt:        "2026-01-01T00:00:00Z",
			EndsAt:          "2026-01-02T00:00:00Z",
			DisplayOutcomes: []RegisterClobMarketDisplayOutcome{
				{Key: "yes", Label: "Yes"},
				{Key: "no", Label: "No"},
			},
		},
		Message:   "signed register-clob-market message",
		Signature: "0xabc",
		BookSignatures: []RegisterClobBookSignature{{
			OutcomeIndex: 0,
			Signature:    "0xdef",
		}},
	})
	if err != nil {
		t.Fatalf("RegisterClobMarket() error = %v", err)
	}
	if gotPath != "/api/markets/register-clob-market" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/markets/register-clob-market")
	}
	if gotRequest.MarketID != "logical-market-1" {
		t.Fatalf("request marketId = %q, want logical payload", gotRequest.MarketID)
	}
	if gotRequest.Metadata.Image != "ipfs://logical-market-image" {
		t.Fatalf("request metadata image = %q, want logical registration image", gotRequest.Metadata.Image)
	}
	if len(gotRequest.Metadata.EvidenceSources) != 1 || gotRequest.Metadata.EvidenceSources[0] != "https://example.com/rules" {
		t.Fatalf("request metadata evidenceSources = %+v, want preserved evidence source", gotRequest.Metadata.EvidenceSources)
	}
	if len(gotRequest.BookSignatures) != 1 || gotRequest.BookSignatures[0].Signature != "0xdef" {
		t.Fatalf("request bookSignatures = %+v, want preserved signatures", gotRequest.BookSignatures)
	}
	if response.MarketID != "logical-market-1" || response.BooksCreated != 2 {
		t.Fatalf("response = %+v, want logical registration payload", response)
	}
}

func TestGetClobDepthUsesHostedProjectionBase(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ClobDepth{
			Bids: []ClobDepthLevel{{ClobID: "market-123-1", Side: "buy", Price: 6100, TotalQty: 12500000, OrderCount: 2}},
			Asks: []ClobDepthLevel{{ClobID: "market-123-1", Side: "sell", Price: 6400, TotalQty: 7000000, OrderCount: 1}},
		})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	depth, err := client.GetClobDepth(context.Background(), server.URL, "market-123", 1, "no")
	if err != nil {
		t.Fatalf("GetClobDepth() error = %v", err)
	}
	if gotPath != "/books/market-123/1/depth?token_side=no" {
		t.Fatalf("request path = %q, want %q", gotPath, "/books/market-123/1/depth?token_side=no")
	}
	if len(depth.Bids) != 1 || depth.Bids[0].Price != 6100 {
		t.Fatalf("depth.Bids = %+v, want hosted bid ladder", depth.Bids)
	}
	if len(depth.Asks) != 1 || depth.Asks[0].Price != 6400 {
		t.Fatalf("depth.Asks = %+v, want hosted ask ladder", depth.Asks)
	}
}

func TestListClobOrdersPreservesQueryFilters(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]ClobOrder{{
			OrderID:   "order-1",
			ClobID:    "market-123-0",
			Maker:     "0xabc",
			Side:      "buy",
			Status:    "open",
			Price:     intPtr(6200),
			Quantity:  5000000,
			Remaining: 5000000,
		}})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	filters := url.Values{}
	filters.Set("clob_id", "market-123-0")
	filters.Set("maker", "0xabc")
	filters.Set("active_only", "true")
	orders, err := client.ListClobOrders(context.Background(), server.URL, filters)
	if err != nil {
		t.Fatalf("ListClobOrders() error = %v", err)
	}
	if !strings.Contains(gotQuery, "clob_id=market-123-0") || !strings.Contains(gotQuery, "maker=0xabc") || !strings.Contains(gotQuery, "active_only=true") {
		t.Fatalf("query = %q, want hosted CLOB filters preserved", gotQuery)
	}
	if len(orders) != 1 || orders[0].OrderID != "order-1" {
		t.Fatalf("orders = %+v, want hosted orders payload", orders)
	}
}

func TestGetClobOrderUsesHostedProjectionBase(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ClobOrder{
			OrderID:   "order-1",
			ClobID:    "market-123-0",
			Maker:     "0xabc",
			Side:      "buy",
			OrderType: "limit",
			Price:     intPtr(6200),
			Quantity:  100,
			Remaining: 80,
			Status:    "open",
		})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	order, err := client.GetClobOrder(context.Background(), server.URL, "order-1")
	if err != nil {
		t.Fatalf("GetClobOrder() error = %v", err)
	}
	if gotPath != "/orders/order-1" {
		t.Fatalf("request path = %q, want %q", gotPath, "/orders/order-1")
	}
	if order.OrderID != "order-1" || order.Remaining != 80 {
		t.Fatalf("order = %+v, want hosted order response", order)
	}
}

func TestGetClobFillUsesHostedProjectionBase(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		createdAt := time.Unix(1700000000, 0).UTC()
		_ = json.NewEncoder(w).Encode(ClobFill{
			TradeID:   "fill-1",
			ClobID:    "market-123-0",
			Buyer:     "0xbuyer",
			Seller:    "0xseller",
			Price:     6100,
			Quantity:  25,
			CreatedAt: &createdAt,
		})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	fill, err := client.GetClobFill(context.Background(), server.URL, "fill-1")
	if err != nil {
		t.Fatalf("GetClobFill() error = %v", err)
	}
	if gotPath != "/fills/fill-1" {
		t.Fatalf("request path = %q, want %q", gotPath, "/fills/fill-1")
	}
	if fill.TradeID != "fill-1" || fill.Price != 6100 {
		t.Fatalf("fill = %+v, want hosted fill response", fill)
	}
}

func TestSubmitClobOrderUsesHostedEventstoreBase(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotPayload struct {
		SignedOrder ClobSignedOrderPayload `json:"signed_order"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ClobOrderResponse{
			OrderID:           "order-2",
			RemainingQuantity: 40,
			TradeCount:        1,
			WasAddedToBook:    true,
		})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.SubmitClobOrder(context.Background(), server.URL+"/api", ClobSignedOrderPayload{
		Maker:           "0xabc",
		Taker:           "0x0000000000000000000000000000000000000000",
		CollateralToken: "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		OutcomeToken:    "0x43e3fa6De5D87dd7265053FA55601d1972984edA",
		OutcomeTokenID:  "101",
		TokenSide:       "yes",
		Side:            0,
		MakerAmount:     "5000",
		TakerAmount:     "10000",
		Expiration:      "1711929600",
		Nonce:           "1711926000000",
		FeeRateBps:      "0",
		Signature:       "0xdeadbeef",
		Market:          "market-123",
		Outcome:         0,
		OrderType:       0,
	})
	if err != nil {
		t.Fatalf("SubmitClobOrder() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/orders" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/orders")
	}
	if gotPayload.SignedOrder.Market != "market-123" || gotPayload.SignedOrder.OutcomeTokenID != "101" {
		t.Fatalf("payload = %+v, want signed order preserved", gotPayload.SignedOrder)
	}
	if gotPayload.SignedOrder.TokenSide != "yes" {
		t.Fatalf("payload tokenSide = %q, want yes", gotPayload.SignedOrder.TokenSide)
	}
	if response.OrderID != "order-2" || !response.WasAddedToBook {
		t.Fatalf("response = %+v, want created resting order response", response)
	}
}

func TestCancelClobOrderUsesHostedEventstoreBase(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotContentType string
	var gotRequest ClobCancelOrderRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ClobOrderResponse{
			OrderID:           "order-1",
			RemainingQuantity: 0,
			TradeCount:        0,
			WasAddedToBook:    false,
		})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.CancelClobOrder(context.Background(), server.URL+"/api", "order-1", ClobCancelOrderRequest{
		Market:    "market-123",
		Outcome:   0,
		TokenSide: "yes",
		Requester: "0xabc",
		Nonce:     "123",
		Deadline:  "456",
		Signature: "0xsig",
		Reason:    "user-requested",
	})
	if err != nil {
		t.Fatalf("CancelClobOrder() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodDelete)
	}
	if gotPath != "/api/orders/order-1" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/orders/order-1")
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if gotRequest.Requester != "0xabc" || gotRequest.Market != "market-123" || gotRequest.Outcome != 0 {
		t.Fatalf("request = %+v, want requester/market/outcome preserved", gotRequest)
	}
	if gotRequest.TokenSide != "yes" || gotRequest.Nonce != "123" || gotRequest.Deadline != "456" || gotRequest.Signature != "0xsig" {
		t.Fatalf("request = %+v, want signed cancel payload preserved", gotRequest)
	}
	if response.OrderID != "order-1" || response.RemainingQuantity != 0 {
		t.Fatalf("response = %+v, want cancelled order response payload", response)
	}
}

func TestCloseClobBookUsesHostedEventstoreBase(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotAdminToken string
	var gotRequest ClobCloseBookRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.RequestURI()
		gotAdminToken = r.Header.Get("X-Admin-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ClobBookLifecycleResponse{Status: "closed", Message: "book closed"})
	}))
	defer server.Close()

	client, err := NewClient("https://example.com/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.CloseClobBook(context.Background(), server.URL+"/api", "market-123", 2, "no", "admin-token", ClobCloseBookRequest{Requester: "0xabc", Reason: "resolved", TokenSide: "no"})
	if err != nil {
		t.Fatalf("CloseClobBook() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/books/market-123/2/close?token_side=no" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/books/market-123/2/close?token_side=no")
	}
	if gotAdminToken != "admin-token" {
		t.Fatalf("X-Admin-Token = %q, want %q", gotAdminToken, "admin-token")
	}
	if gotRequest.Reason != "resolved" || gotRequest.TokenSide != "no" {
		t.Fatalf("request = %+v, want close-book payload preserved", gotRequest)
	}
	if response.Status != "closed" {
		t.Fatalf("response = %+v, want closed status", response)
	}
}

func TestResolveClobMarketUsesAppRootRoute(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotRequest ResolveClobMarketRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request body) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ResolveClobMarketResponse{
			Success:              true,
			MarketID:             "market-123",
			SignerAddress:        "0xabc",
			ResolvedOutcomeID:    "market-123-yes",
			ResolvedOutcomeLabel: "Yes",
			WinningOutcomeIndex:  0,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "device-123")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	response, err := client.ResolveClobMarket(context.Background(), ResolveClobMarketRequest{
		MarketID:            "market-123",
		Network:             "xrpl-mainnet",
		RPCURL:              "https://rpc.xrplevm.org",
		WalletAddress:       "0xabc",
		WinningOutcomeIndex: 0,
		ResolutionTxHashes:  []string{"0x1"},
		Message:             "signed resolve message",
		Signature:           "0xsig",
	})
	if err != nil {
		t.Fatalf("ResolveClobMarket() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/api/markets/resolve-clob-market" {
		t.Fatalf("request path = %q, want %q", gotPath, "/api/markets/resolve-clob-market")
	}
	if gotRequest.WinningOutcomeIndex != 0 || gotRequest.MarketID != "market-123" {
		t.Fatalf("request = %+v, want resolve payload preserved", gotRequest)
	}
	if response.ResolvedOutcomeID != "market-123-yes" {
		t.Fatalf("response = %+v, want resolved outcome id", response)
	}
}

func intPtr(value int) *int {
	return &value
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

func TestGetMarketParsesImplementationAwarePayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/markets/test-clob-market" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/api/cli/markets/test-clob-market")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                   "test-clob-market",
			"marketType":           "standalone",
			"marketImplementation": "AxiomCTFMarket",
			"title":                "Test CLOB Market",
			"headline":             nil,
			"description":          "hidden fixture",
			"category":             "world",
			"status":               "active",
			"startsAt":             "2026-03-20T00:00:00Z",
			"endsAt":               "2030-12-31T23:59:00Z",
			"resolveBy":            "2031-01-31T23:59:00Z",
			"contractAddress":      "0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa",
			"chainId":              1440000,
			"isResolved":           false,
			"isSeries":             false,
			"metadataUri":          nil,
			"imageUrl":             nil,
			"logicalMarketAddresses": []string{
				"0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa",
				"0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb",
			},
			"ctfOutcomeMarkets": []map[string]any{
				{
					"outcomeId":       "yes",
					"outcomeIndex":    0,
					"label":           "Yes",
					"contractAddress": "0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa",
					"outcomeTokenIds": []string{"1", "2"},
				},
			},
			"outcomes":             []map[string]any{{"index": 0, "label": "Yes", "description": ""}},
			"settlementToken":      nil,
			"creator":              nil,
			"ownerAddress":         nil,
			"resolvedOutcomeIndex": nil,
			"resolutionCriteria":   "rules",
			"tags":                 []string{"internal"},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api/cli", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	market, err := client.GetMarket(context.Background(), "test-clob-market", "")
	if err != nil {
		t.Fatalf("GetMarket() error = %v", err)
	}
	if market.MarketImplementation != "AxiomCTFMarket" {
		t.Fatalf("MarketImplementation = %q, want %q", market.MarketImplementation, "AxiomCTFMarket")
	}
	if len(market.LogicalMarketAddresses) != 2 {
		t.Fatalf("LogicalMarketAddresses = %v, want 2 addresses", market.LogicalMarketAddresses)
	}
	if len(market.CTFOutcomeMarkets) != 1 || market.CTFOutcomeMarkets[0].Label != "Yes" {
		t.Fatalf("CTFOutcomeMarkets = %+v, want parsed grouped binding", market.CTFOutcomeMarkets)
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
