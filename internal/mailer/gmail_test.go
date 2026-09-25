package mailer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGmailSendRefreshesOAuthTokenAndSendsMIMEMessage(t *testing.T) {
	var tokenCalls atomic.Int32
	var sentRaw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			want := url.Values{
				"client_id":     {"client-id"},
				"client_secret": {"client-secret"},
				"refresh_token": {"refresh-token"},
				"grant_type":    {"refresh_token"},
			}
			if r.Form.Encode() != want.Encode() {
				t.Errorf("token form = %q, want %q", r.Form.Encode(), want.Encode())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "token_type": "Bearer", "expires_in": 3600})
		case "/gmail/v1/users/me/messages/send":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			var payload struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode Gmail payload: %v", err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(payload.Raw)
			if err != nil {
				t.Errorf("decode raw message: %v", err)
			}
			sentRaw = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":"message-1"}`)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &GmailClient{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "refresh-token",
		From:         "CodeLocal <sender@gmail.com>",
		TokenURL:     server.URL + "/token",
		APIBaseURL:   server.URL,
		HTTPClient:   server.Client(),
	}
	err := client.Send(context.Background(), Message{
		To:      "recipient@example.com",
		Subject: "Mã xác minh",
		Text:    "Your code is 123456.",
		HTML:    "<strong>Your code is 123456.</strong>",
	}, "otp-1")
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("token endpoint called %d times, want 1", tokenCalls.Load())
	}
	for _, fragment := range []string{
		`From: "CodeLocal" <sender@gmail.com>`,
		"To: <recipient@example.com>",
		"Subject: =?UTF-8?",
		"Content-Type: multipart/alternative;",
		"Your code is 123456.",
		"<strong>Your code is 123456.</strong>",
		"Message-ID: <",
	} {
		if !strings.Contains(sentRaw, fragment) {
			t.Errorf("raw message missing %q:\n%s", fragment, sentRaw)
		}
	}
}

func TestGmailSendReportsAPIErrorWithoutLeakingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "super-secret-access-token", "expires_in": 3600})
		case "/gmail/v1/users/me/messages/send":
			http.Error(w, `{"error":{"message":"sender rejected"}}`, http.StatusForbidden)
		}
	}))
	defer server.Close()

	client := &GmailClient{
		ClientID: "client-id", ClientSecret: "client-secret", RefreshToken: "refresh-token",
		From: "sender@gmail.com", TokenURL: server.URL + "/token", APIBaseURL: server.URL, HTTPClient: server.Client(),
	}
	err := client.Send(context.Background(), Message{To: "recipient@example.com", Subject: "Verify", Text: "123456"}, "otp-1")
	if err == nil || !strings.Contains(err.Error(), "gmail request failed (403)") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "super-secret-access-token") {
		t.Fatalf("error leaked access token: %v", err)
	}
}

func TestGmailSendBatchValidatesEveryMessageBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "request must not be sent", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &GmailClient{
		ClientID: "client-id", ClientSecret: "client-secret", RefreshToken: "refresh-token",
		From: "sender@gmail.com", TokenURL: server.URL + "/token", APIBaseURL: server.URL, HTTPClient: server.Client(),
	}
	err := client.SendBatch(context.Background(), []Message{
		{To: "first@example.com", Subject: "Release", Text: "Available"},
		{To: "not-an-email", Subject: "Release", Text: "Available"},
	}, "release-1")
	if err == nil || !strings.Contains(err.Error(), "message 2") {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("made %d HTTP requests before validating the full batch", requests.Load())
	}
}

func TestGmailSendBatchAllowsEmptyBatchWithoutConfiguration(t *testing.T) {
	var client *GmailClient
	if err := client.SendBatch(context.Background(), nil, ""); err != nil {
		t.Fatalf("empty batch returned error: %v", err)
	}
}

func TestGmailSendBatchUsesOneBlindCopyMessage(t *testing.T) {
	var sendCalls atomic.Int32
	var sentRaw string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "expires_in": 3600})
		case "/gmail/v1/users/me/messages/send":
			sendCalls.Add(1)
			var payload struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode Gmail payload: %v", err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(payload.Raw)
			if err != nil {
				t.Errorf("decode raw message: %v", err)
			}
			sentRaw = string(raw)
			_, _ = io.WriteString(w, `{"id":"batch-message"}`)
		}
	}))
	defer server.Close()

	client := &GmailClient{
		ClientID: "client-id", ClientSecret: "client-secret", RefreshToken: "refresh-token",
		From: "sender@gmail.com", TokenURL: server.URL + "/token", APIBaseURL: server.URL, HTTPClient: server.Client(),
	}
	err := client.SendBatch(context.Background(), []Message{
		{To: "first@example.com", Subject: "Release", Text: "Available", HTML: "<p>Available</p>"},
		{To: "second@example.com", Subject: "Release", Text: "Available", HTML: "<p>Available</p>"},
	}, "release-1")
	if err != nil {
		t.Fatal(err)
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("Gmail send endpoint called %d times, want 1", sendCalls.Load())
	}
	for _, fragment := range []string{"To: undisclosed-recipients:;", "Bcc: <first@example.com>,", " <second@example.com>"} {
		if !strings.Contains(sentRaw, fragment) {
			t.Errorf("raw batch message missing %q:\n%s", fragment, sentRaw)
		}
	}
	if strings.Contains(sentRaw, "To: <first@example.com>") || strings.Contains(sentRaw, "To: <second@example.com>") {
		t.Fatalf("raw batch exposed a recipient in the To header:\n%s", sentRaw)
	}
}

func TestGmailSendBatchRejectsDifferentContentBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "expires_in": 3600})
	}))
	defer server.Close()

	client := &GmailClient{
		ClientID: "client-id", ClientSecret: "client-secret", RefreshToken: "refresh-token",
		From: "sender@gmail.com", TokenURL: server.URL + "/token", APIBaseURL: server.URL, HTTPClient: server.Client(),
	}
	err := client.SendBatch(context.Background(), []Message{
		{To: "first@example.com", Subject: "First", Text: "First body"},
		{To: "second@example.com", Subject: "Second", Text: "Second body"},
	}, "release-1")
	if err == nil || !strings.Contains(err.Error(), "identical subject and content") {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("made %d HTTP requests for a heterogeneous batch", requests.Load())
	}
}
