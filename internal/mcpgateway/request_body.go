package mcpgateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var errMCPRequestBodyTooLarge = errors.New("MCP request body too large")

func readBoundedMCPRequestBody(r *http.Request) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, io.ErrUnexpectedEOF
	}
	if r.ContentLength > legacyToolCompatibilityMaxBody {
		return nil, errMCPRequestBodyTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, legacyToolCompatibilityMaxBody+1))
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	if len(raw) > legacyToolCompatibilityMaxBody {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		return nil, errMCPRequestBodyTooLarge
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	return raw, nil
}

func MCPRequestMethod(r *http.Request) string {
	if r == nil || r.URL.Path != "/mcp" || r.Method != http.MethodPost || r.Body == nil {
		return ""
	}
	raw, err := readBoundedMCPRequestBody(r)
	if err != nil {
		return ""
	}
	var envelope any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil {
		return ""
	}
	methods := make([]string, 0, 2)
	var collect func(any)
	collect = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				collect(item)
			}
		case map[string]any:
			if method, _ := typed["method"].(string); method != "" {
				methods = append(methods, method)
			}
		}
	}
	collect(envelope)
	return strings.Join(methods, ",")
}
