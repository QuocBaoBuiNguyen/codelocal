package mailer

import "testing"

func TestFromEnvSelectsGmailProvider(t *testing.T) {
	t.Setenv("CODELOCAL_EMAIL_PROVIDER", "gmail")
	t.Setenv("CODELOCAL_EMAIL_FROM", "CodeLocal <sender@gmail.com>")
	t.Setenv("GMAIL_CLIENT_ID", "client-id")
	t.Setenv("GMAIL_CLIENT_SECRET", "client-secret")
	t.Setenv("GMAIL_REFRESH_TOKEN", "refresh-token")

	sender, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	gmail, ok := sender.(*GmailClient)
	if !ok {
		t.Fatalf("FromEnv() returned %T, want *GmailClient", sender)
	}
	if gmail.From != "CodeLocal <sender@gmail.com>" || gmail.ClientID != "client-id" || gmail.ClientSecret != "client-secret" || gmail.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected Gmail configuration: %+v", gmail)
	}
}

func TestFromEnvRejectsUnknownProvider(t *testing.T) {
	t.Setenv("CODELOCAL_EMAIL_PROVIDER", "carrier-pigeon")

	if _, err := FromEnv(); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
