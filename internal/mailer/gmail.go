package mailer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultGmailAPIBaseURL = "https://gmail.googleapis.com"
	defaultGmailTokenURL   = "https://oauth2.googleapis.com/token"
)

// GmailClient sends messages through Gmail's HTTPS API using an OAuth refresh token.
type GmailClient struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	From         string
	TokenURL     string
	APIBaseURL   string
	HTTPClient   *http.Client

	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

type gmailTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func gmailFromEnv() (*GmailClient, error) {
	client := &GmailClient{
		ClientID:     strings.TrimSpace(os.Getenv("GMAIL_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GMAIL_CLIENT_SECRET")),
		RefreshToken: strings.TrimSpace(os.Getenv("GMAIL_REFRESH_TOKEN")),
		From:         strings.TrimSpace(os.Getenv("CODELOCAL_EMAIL_FROM")),
		TokenURL:     defaultGmailTokenURL,
		APIBaseURL:   defaultGmailAPIBaseURL,
		HTTPClient:   &http.Client{Timeout: 12 * time.Second},
	}
	if err := client.validateConfig(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *GmailClient) validateConfig() error {
	if c == nil {
		return errors.New("mailer is not configured")
	}
	required := []struct {
		name  string
		value string
	}{
		{"GMAIL_CLIENT_ID", c.ClientID},
		{"GMAIL_CLIENT_SECRET", c.ClientSecret},
		{"GMAIL_REFRESH_TOKEN", c.RefreshToken},
		{"CODELOCAL_EMAIL_FROM", c.From},
	}
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%s is required", item.name)
		}
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("CODELOCAL_EMAIL_FROM is invalid: %w", err)
	}
	return nil
}

func (c *GmailClient) Send(ctx context.Context, message Message, idempotencyKey string) error {
	if err := c.validateConfig(); err != nil {
		return err
	}
	raw, err := c.rawMessage(message, idempotencyKey)
	if err != nil {
		return err
	}
	return c.sendRaw(ctx, raw)
}

func (c *GmailClient) sendRaw(ctx context.Context, raw []byte) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Raw string `json:"raw"`
	}{Raw: base64.RawURLEncoding.EncodeToString(raw)})
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = defaultGmailAPIBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/gmail/v1/users/me/messages/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		return nil
	}
	rawError, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	messageText := strings.TrimSpace(string(rawError))
	if messageText == "" {
		messageText = resp.Status
	}
	return fmt.Errorf("gmail request failed (%d): %s", resp.StatusCode, messageText)
}

func (c *GmailClient) SendBatch(ctx context.Context, messages []Message, idempotencyKey string) error {
	if len(messages) == 0 {
		return nil
	}
	if err := c.validateConfig(); err != nil {
		return err
	}
	first := messages[0]
	recipients := make([]string, 0, len(messages))
	for index, message := range messages {
		if _, err := c.rawMessage(message, ""); err != nil {
			return fmt.Errorf("validate Gmail message %d of %d: %w", index+1, len(messages), err)
		}
		if message.Subject != first.Subject || message.Text != first.Text || message.HTML != first.HTML {
			return errors.New("Gmail batch messages must have identical subject and content")
		}
		recipients = append(recipients, message.To)
	}
	raw, err := c.rawMessageForRecipients(first, recipients, true, idempotencyKey)
	if err != nil {
		return err
	}
	return c.sendRaw(ctx, raw)
}

func (c *GmailClient) token(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.accessToken != "" && time.Now().Add(time.Minute).Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	form := url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"refresh_token": {c.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	tokenURL := strings.TrimSpace(c.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultGmailTokenURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return "", fmt.Errorf("gmail OAuth token refresh failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var token gmailTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&token); err != nil {
		return "", fmt.Errorf("decode Gmail OAuth token: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return "", errors.New("Gmail OAuth token response did not include an access token")
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 3600
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func (c *GmailClient) rawMessage(message Message, idempotencyKey string) ([]byte, error) {
	return c.rawMessageForRecipients(message, []string{message.To}, false, idempotencyKey)
}

func (c *GmailClient) rawMessageForRecipients(message Message, recipients []string, blind bool, idempotencyKey string) ([]byte, error) {
	if len(recipients) == 0 || strings.TrimSpace(message.Subject) == "" {
		return nil, errors.New("email recipient and subject are required")
	}
	addresses := make([]*mail.Address, 0, len(recipients))
	for _, recipient := range recipients {
		address, err := mail.ParseAddress(strings.TrimSpace(recipient))
		if err != nil {
			return nil, errors.New("email recipient and subject are required")
		}
		addresses = append(addresses, address)
	}
	if strings.ContainsAny(message.Subject, "\r\n") {
		return nil, errors.New("email subject must not contain line breaks")
	}
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return nil, fmt.Errorf("CODELOCAL_EMAIL_FROM is invalid: %w", err)
	}

	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	_, _ = fmt.Fprintf(&body, "From: %s\r\n", from.String())
	if blind {
		_, _ = fmt.Fprint(&body, "To: undisclosed-recipients:;\r\n")
		_, _ = fmt.Fprintf(&body, "Bcc: %s", addresses[0].String())
		for _, address := range addresses[1:] {
			_, _ = fmt.Fprintf(&body, ",\r\n %s", address.String())
		}
		_, _ = fmt.Fprint(&body, "\r\n")
	} else {
		_, _ = fmt.Fprintf(&body, "To: %s\r\n", addresses[0].String())
	}
	_, _ = fmt.Fprintf(&body, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", message.Subject))
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		digest := sha256.Sum256([]byte(key))
		_, _ = fmt.Fprintf(&body, "Message-ID: <%s@gmail.api.codelocal>\r\n", hex.EncodeToString(digest[:16]))
	}
	_, _ = fmt.Fprint(&body, "MIME-Version: 1.0\r\n")
	_, _ = fmt.Fprintf(&body, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", parts.Boundary())
	if err := writeGmailPart(parts, "text/plain", message.Text); err != nil {
		return nil, err
	}
	if strings.TrimSpace(message.HTML) != "" {
		if err := writeGmailPart(parts, "text/html", message.HTML); err != nil {
			return nil, err
		}
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func writeGmailPart(writer *multipart.Writer, mediaType, content string) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", mediaType+`; charset="UTF-8"`)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(part)
	if _, err := encoded.Write([]byte(content)); err != nil {
		return err
	}
	return encoded.Close()
}

func (c *GmailClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 12 * time.Second}
}

var _ Sender = (*GmailClient)(nil)
var _ Sender = (*Client)(nil)
