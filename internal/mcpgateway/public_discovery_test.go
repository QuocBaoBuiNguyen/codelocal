package mcpgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func discoveryRequest(method string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`))
}

func TestPublicDiscoveryRequestAllowsOnlyCatalogHandshake(t *testing.T) {
	for _, method := range []string{"initialize", "server/discover", "notifications/initialized", "tools/list", "ping"} {
		req := discoveryRequest(method)
		if !publicDiscoveryRequest(req) {
			t.Fatalf("%s should be public discovery", method)
		}
	}
	for _, method := range []string{"tools/call", "resources/list", "prompts/list"} {
		req := discoveryRequest(method)
		if publicDiscoveryRequest(req) {
			t.Fatalf("%s must remain OAuth-protected", method)
		}
	}

	batch := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[
		{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}},
		{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
	]`))
	if !publicDiscoveryRequest(batch) {
		t.Fatal("all-discovery batch should be public")
	}
	mixed := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[
		{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}},
		{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"list"}}}
	]`))
	if publicDiscoveryRequest(mixed) {
		t.Fatal("batch containing tools/call must remain OAuth-protected")
	}
}

func TestPublicDiscoveryOrProtectedRoutesCatalogWithoutOpeningExecution(t *testing.T) {
	var publicCalls atomic.Int32
	var protectedCalls atomic.Int32
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protectedCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	handler := PublicDiscoveryOrProtected(public, protected)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, discoveryRequest("tools/list"))
	if rec.Code != http.StatusOK || publicCalls.Load() != 1 || protectedCalls.Load() != 0 {
		t.Fatalf("unauthenticated tools/list routing status=%d public=%d protected=%d", rec.Code, publicCalls.Load(), protectedCalls.Load())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, discoveryRequest("tools/call"))
	if rec.Code != http.StatusUnauthorized || protectedCalls.Load() != 1 {
		t.Fatalf("unauthenticated tools/call must be protected: status=%d protected=%d", rec.Code, protectedCalls.Load())
	}

	req := discoveryRequest("tools/list")
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || protectedCalls.Load() != 2 {
		t.Fatalf("authenticated discovery must use protected user route: status=%d protected=%d", rec.Code, protectedCalls.Load())
	}
}

func TestPublicDiscoveryHandlerListsCompactToolsWithoutOAuthClaims(t *testing.T) {
	s := &Service{
		servers:      map[string]*mcp.Server{},
		routes:       map[string]map[string]string{},
		shownUpdates: map[string]map[string]struct{}{},
	}
	httpServer := httptest.NewServer(s.PublicDiscoveryHandler())
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "codelocal-public-discovery-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect public discovery MCP client: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("public tools/list failed: %v", err)
	}
	if len(result.Tools) != len(compactToolDefinitions()) || len(result.Tools) != 14 {
		t.Fatalf("public discovery returned %d tools, want 14", len(result.Tools))
	}
}
