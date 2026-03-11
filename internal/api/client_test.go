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
