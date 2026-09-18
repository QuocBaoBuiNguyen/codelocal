package mcpgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type toolAuthChallengeRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
}

func unauthenticatedToolCallRequest(r *http.Request) bool {
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

	var request toolAuthChallengeRequest
	if json.Unmarshal(raw, &request) != nil {
		return false
	}
	return request.Method == "tools/call" && len(request.ID) > 0
}

func MCPAuthChallengeHandler(resourceMetadataURL string) http.Handler {
	resourceMetadataURL = strings.TrimSpace(resourceMetadataURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !unauthenticatedToolCallRequest(r) {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid MCP request", http.StatusBadRequest)
			return
		}
		var request toolAuthChallengeRequest
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "invalid MCP request", http.StatusBadRequest)
			return
		}

		challenge := fmt.Sprintf(
			`Bearer resource_metadata="%s", scope="mcp:tools", error="insufficient_scope", error_description="Connect your CodeLocal account to use this tool"`,
			resourceMetadataURL,
		)
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": map[string]any{
				"content": []map[string]string{{
					"type": "text",
					"text": "Authentication required: connect your CodeLocal account.",
				}},
				"_meta": map[string]any{
					"mcp/www_authenticate": []string{challenge},
				},
				"isError": true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(response)
	})
}
