package mcpgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const publicCatalogUserID = "__codelocal_public_catalog__"

var publicDiscoveryMethods = map[string]struct{}{
	"initialize":                {},
	"server/discover":           {},
	"notifications/initialized": {},
	"tools/list":                {},
	"ping":                      {},
}

func publicDiscoveryEnvelope(value any) bool {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return false
		}
		for _, item := range typed {
			if !publicDiscoveryEnvelope(item) {
				return false
			}
		}
		return true
	case map[string]any:
		method, _ := typed["method"].(string)
		_, ok := publicDiscoveryMethods[method]
		return ok
	default:
		return false
	}
}

func publicDiscoveryRequest(r *http.Request) bool {
	if r == nil || r.URL.Path != "/mcp" || r.Method != http.MethodPost || r.Body == nil || r.ContentLength < 0 || r.ContentLength > legacyToolCompatibilityMaxBody {
		return false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))

	var envelope any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(&envelope) == nil && publicDiscoveryEnvelope(envelope)
}

// PublicDiscoveryHandler exposes only the static MCP catalog handshake. It uses
// a dedicated catalog identity so anonymous discovery never obtains a real
// CodeLocal user identity or workspace/runtime route.
func (s *Service) PublicDiscoveryHandler() http.Handler {
	stream := streamableMCPHandler(func(*http.Request) *mcp.Server {
		return s.serverFor(publicCatalogUserID)
	})
	surface := PublicToolSurface()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !publicDiscoveryRequest(r) {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-CodeLocal-Tool-Surface-Version", fmt.Sprint(surface.Version))
		w.Header().Set("X-CodeLocal-Tool-Surface-Hash", surface.Hash)
		stream.ServeHTTP(w, r)
	})
}

// PublicDiscoveryOrProtected lets unauthenticated AI-host scanners read only the
// MCP catalog needed to render Actions. Bearer-authenticated requests and every
// non-discovery request continue through the OAuth-protected handler.
func PublicDiscoveryOrProtected(public, protected http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			protected.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			protected.ServeHTTP(w, r)
			return
		}
		if publicDiscoveryRequest(r) {
			public.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}
