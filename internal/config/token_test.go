package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lgforsberg/bifrost/mail"
)

func TestTokenCommandSource(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		want      string
		wantErr   string
		wantIsErr error
	}{
		{
			name: "reads the token from stdout",
			argv: []string{"sh", "-c", "printf ya29.a0AfH6"},
			want: "ya29.a0AfH6",
		},
		{
			// Nearly every way of printing a token adds a newline.
			name: "surrounding whitespace is not part of the token",
			argv: []string{"sh", "-c", "echo '  ya29.a0AfH6  '"},
			want: "ya29.a0AfH6",
		},
		{
			name: "arguments reach the command",
			argv: []string{"sh", "-c", "printf %s \"$1\"", "sh", "from-argv"},
			want: "from-argv",
		},
		{
			name:      "a failing command reports what it said",
			argv:      []string{"sh", "-c", "echo 'refresh token expired' >&2; exit 1"},
			wantErr:   "refresh token expired",
			wantIsErr: mail.ErrAuthFailed,
		},
		{
			name:      "printing nothing is an error, not an empty token",
			argv:      []string{"sh", "-c", "exit 0"},
			wantErr:   "printed no token",
			wantIsErr: mail.ErrAuthFailed,
		},
		{
			// A helper that prints the whole JSON token response, or a banner
			// above the token, would otherwise send the lot as a credential.
			name:      "more than one line is refused",
			argv:      []string{"sh", "-c", "echo banner; echo tok"},
			wantErr:   "more than one line",
			wantIsErr: mail.ErrAuthFailed,
		},
		{
			name:      "a command that does not exist",
			argv:      []string{"bifrost-no-such-token-helper"},
			wantErr:   "bifrost-no-such-token-helper",
			wantIsErr: mail.ErrAuthFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := tokenCommandSource(tt.argv, 10*time.Second)(context.Background())

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("token source: %v", err)
				}
				if token != tt.want {
					t.Errorf("token = %q, want %q", token, tt.want)
				}
				return
			}

			if err == nil {
				t.Fatalf("token source returned %q, want an error", token)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantErr)
			}
			if tt.wantIsErr != nil && !errors.Is(err, tt.wantIsErr) {
				t.Errorf("error = %v, want one wrapping %v", err, tt.wantIsErr)
			}
		})
	}
}

// A helper that waits must be cut off rather than hang the command, because
// nothing is going to arrive to unblock it.
func TestTokenCommandSource_Timeout(t *testing.T) {
	start := time.Now()
	_, err := tokenCommandSource([]string{"sleep", "30"}, 100*time.Millisecond)(context.Background())

	if err == nil {
		t.Fatal("a helper that never finished returned a token")
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Errorf("error = %v, want it to say the helper ran out of time", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %s, want the timeout to have applied", elapsed)
	}
}

// The helper gets no stdin, so one that decides to prompt fails instead of
// waiting. Bifrost's own stdin may be a message body being piped in, which is
// the other reason not to hand it over.
func TestTokenCommandSource_NoStdin(t *testing.T) {
	token, err := tokenCommandSource([]string{"sh", "-c", "cat; printf tok"}, 5*time.Second)(context.Background())
	if err != nil {
		t.Fatalf("token source: %v", err)
	}
	if token != "tok" {
		t.Errorf("token = %q, want the helper to have read an immediate EOF", token)
	}
}

func TestValidateAuth(t *testing.T) {
	tests := []struct {
		name    string
		account accountJSON
		wantErr string
	}{
		{
			name:    "a password account",
			account: accountJSON{Password: "p"},
		},
		{
			name: "xoauth2 with a command",
			account: accountJSON{
				AuthMechanism: "xoauth2",
				TokenCommand:  []string{"get-token"},
			},
		},
		{
			name: "oauthbearer with a command",
			account: accountJSON{
				AuthMechanism: "oauthbearer",
				TokenCommand:  []string{"get-token"},
			},
		},
		{
			name: "an unknown mechanism",
			account: accountJSON{
				AuthMechanism: "ntlm",
				TokenCommand:  []string{"get-token"},
			},
			wantErr: "ntlm",
		},
		{
			name:    "a mechanism with nothing to get a token from",
			account: accountJSON{AuthMechanism: "xoauth2"},
			wantErr: "needs a tokenCommand",
		},
		{
			name: "an empty command name",
			account: accountJSON{
				AuthMechanism: "xoauth2",
				TokenCommand:  []string{"  "},
			},
			wantErr: "needs a tokenCommand",
		},
		{
			// Dead settings are refused rather than ignored, so the config
			// cannot say one thing while something else happens.
			name: "a password beside a token mechanism",
			account: accountJSON{
				AuthMechanism: "xoauth2",
				TokenCommand:  []string{"get-token"},
				Password:      "p",
			},
			wantErr: "would never be used",
		},
		{
			name: "a token command with no mechanism to use it",
			account: accountJSON{
				Password:     "p",
				TokenCommand: []string{"get-token"},
			},
			wantErr: "would never be used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAuth(tt.account)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateAuth: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateAuth accepted %+v, want an error", tt.account)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantErr)
			}
			if !errors.Is(err, mail.ErrInvalidConfig) {
				t.Errorf("error = %v, want one wrapping ErrInvalidConfig", err)
			}
		})
	}
}

// An OAuth account needs no password, which is the point: Gmail and
// Microsoft 365 no longer accept one.
func TestLoad_OAuthAccountNeedsNoPassword(t *testing.T) {
	path := writeTestConfig(t, `{
	  "accounts": [{
	    "address": "me@example.com",
	    "imap": { "host": "outlook.office365.com" },
	    "smtp": { "host": "smtp.office365.com" },
	    "authMechanism": "XOAUTH2",
	    "tokenCommand": ["sh", "-c", "printf tok"]
	  }]
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acct := cfg.Accounts[0]
	if acct.AuthMechanism != mail.AuthXOAuth2 {
		t.Errorf("AuthMechanism = %q, want it normalised to %q", acct.AuthMechanism, mail.AuthXOAuth2)
	}
	if acct.Password != "" {
		t.Errorf("Password = %q, want none", acct.Password)
	}
	if acct.TokenSource == nil {
		t.Fatal("no token source was built from tokenCommand")
	}

	token, err := acct.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("token source: %v", err)
	}
	if token != "tok" {
		t.Errorf("token = %q, want tok", token)
	}
}

func TestLoad_RejectsContradictoryAuth(t *testing.T) {
	path := writeTestConfig(t, `{
	  "accounts": [{
	    "address": "me@example.com",
	    "imap": { "host": "imap.example.com" },
	    "smtp": { "host": "smtp.example.com" },
	    "password": "p",
	    "authMechanism": "xoauth2",
	    "tokenCommand": ["get-token"]
	  }]
	}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a password alongside a token mechanism")
	} else if !errors.Is(err, mail.ErrInvalidConfig) {
		t.Errorf("error = %v, want one wrapping ErrInvalidConfig", err)
	}
}

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing the test config: %v", err)
	}
	return path
}
