package webauth

import (
	"bytes"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

func TestNewSignupCodeIsSixDigits(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9]{6}$`)
	for i := 0; i < 20; i++ {
		code, err := newSignupCode()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(code) {
			t.Fatalf("unexpected verification code: %q", code)
		}
	}
}

func TestSignupCodeHashBindsTokenAndCode(t *testing.T) {
	first := signupCodeHash("token-one", "123456")
	if first == signupCodeHash("token-one", "654321") {
		t.Fatal("verification hash must change with code")
	}
	if first == signupCodeHash("token-two", "123456") {
		t.Fatal("verification hash must change with pending signup token")
	}
}

func TestMaskEmail(t *testing.T) {
	if got := maskEmail("someone@example.com"); got != "s******@example.com" {
		t.Fatalf("maskEmail()=%q", got)
	}
}

func TestLogSignupEmailFailureIncludesCauseWithoutSecrets(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	t.Setenv("CODELOCAL_EMAIL_PROVIDER", "gmail")
	t.Setenv("GMAIL_CLIENT_SECRET", "client-secret-value")
	t.Setenv("GMAIL_REFRESH_TOKEN", "refresh-token-value")
	logSignupEmailFailure(errors.New("gmail OAuth token refresh failed (400): invalid_grant client-secret-value refresh-token-value"))

	logged := output.String()
	for _, expected := range []string{"signup verification email delivery failed", `"provider":"gmail"`, "invalid_grant", "[REDACTED]"} {
		if !strings.Contains(logged, expected) {
			t.Errorf("log missing %q: %s", expected, logged)
		}
	}
	for _, secret := range []string{"client-secret-value", "refresh-token-value"} {
		if strings.Contains(logged, secret) {
			t.Errorf("log leaked secret %q: %s", secret, logged)
		}
	}
}
