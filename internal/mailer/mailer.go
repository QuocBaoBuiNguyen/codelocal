package mailer

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Sender is the provider-independent email delivery contract used by CodeLocal.
type Sender interface {
	Send(ctx context.Context, message Message, idempotencyKey string) error
	SendBatch(ctx context.Context, messages []Message, idempotencyKey string) error
}

// FromEnv builds the email provider selected by CODELOCAL_EMAIL_PROVIDER.
// The empty value keeps the historical Resend behavior for compatibility.
func FromEnv() (Sender, error) {
	switch provider := strings.ToLower(strings.TrimSpace(os.Getenv("CODELOCAL_EMAIL_PROVIDER"))); provider {
	case "", "resend":
		return resendFromEnv()
	case "gmail":
		return gmailFromEnv()
	default:
		return nil, fmt.Errorf("unsupported email provider %q", provider)
	}
}
