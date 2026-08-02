package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/lgforsberg/bifrost/internal/testimap"
)

func staticToken(token string) TokenSource {
	return func(context.Context) (string, error) { return token, nil }
}

// The worked example from Google's SASL XOAUTH2 specification, which
// Microsoft's documentation repeats byte for byte. A mechanism this small is
// still easy to get wrong in the separators, and getting it wrong shows up
// only as a server refusing to authenticate.
func TestXOAuth2_MatchesTheSpecifiedInitialResponse(t *testing.T) {
	const (
		user  = "someuser@example.com"
		token = "ya29.vF9dft4qmTc2Nvb3RlckBhdHRhdmlzdGEuY29tCg"
		want  = "dXNlcj1zb21ldXNlckBleGFtcGxlLmNvbQFhdXRoPUJlYXJlciB5YTI5LnZG" +
			"OWRmdDRxbVRjMk52YjNSbGNrQmhkSFJoZG1semRHRXVZMjl0Q2cBAQ=="
	)

	client := &xoauth2Client{username: user, token: token}
	mechanism, response, err := client.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mechanism != "XOAUTH2" {
		t.Errorf("mechanism = %q, want XOAUTH2", mechanism)
	}

	// The response goes out raw and the protocol layer encodes it, so this
	// encodes here to compare against the published value.
	if got := base64.StdEncoding.EncodeToString(response); got != want {
		t.Errorf("initial response = %q,\n                want %q", got, want)
	}
}

// A challenge only ever means refusal. Answering it is what lets the server
// state the reason, and the reason is worth keeping.
func TestXOAuth2_KeepsTheRefusalReason(t *testing.T) {
	client := &xoauth2Client{username: "me@example.com", token: "t"}
	if _, _, err := client.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const refusal = `{"status":"400","schemes":"Bearer","scope":"https://mail.google.com/"}`
	response, err := client.Next([]byte(refusal))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if len(response) != 0 {
		t.Errorf("answered a refusal with %q, want an empty response", response)
	}
	if detail := saslFailureDetail(client); !strings.Contains(detail, "scope") {
		t.Errorf("failure detail = %q, want the server's reason", detail)
	}
}

// Nothing to report for a mechanism that was never challenged, or one that is
// not ours.
func TestSASLFailureDetail_QuietWhenThereIsNothingToSay(t *testing.T) {
	if detail := saslFailureDetail(&xoauth2Client{}); detail != "" {
		t.Errorf("detail = %q, want none before a refusal", detail)
	}
}

func TestSASLExchange(t *testing.T) {
	tests := []struct {
		name          string
		account       AccountConfig
		wantMechanism string
		wantErr       error
		wantErrText   string
	}{
		{
			name:          "no mechanism keeps password auth",
			account:       AccountConfig{Address: "me@example.com"},
			wantMechanism: "",
		},
		{
			name: "xoauth2",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: AuthXOAuth2,
				TokenSource:   staticToken("tok"),
			},
			wantMechanism: "XOAUTH2",
		},
		{
			name: "oauthbearer",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: AuthOAuthBearer,
				TokenSource:   staticToken("tok"),
			},
			wantMechanism: "OAUTHBEARER",
		},
		{
			// Someone writing it the way the provider's documentation spells
			// it should not have to discover that we wanted lower case.
			name: "mechanism is not case sensitive",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: "XOAuth2",
				TokenSource:   staticToken("tok"),
			},
			wantMechanism: "XOAUTH2",
		},
		{
			name: "unknown mechanism is refused by name",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: "kerberos",
				TokenSource:   staticToken("tok"),
			},
			wantErr:     ErrInvalidConfig,
			wantErrText: "kerberos",
		},
		{
			name: "a mechanism with no token source",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: AuthXOAuth2,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			// Sending an empty token gets a refusal that reads like a scope
			// or expiry problem, which is the wrong thing to go and check.
			name: "an empty token is caught before it is sent",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: AuthXOAuth2,
				TokenSource:   staticToken("   "),
			},
			wantErr: ErrAuthFailed,
		},
		{
			name: "a token source that fails",
			account: AccountConfig{
				Address:       "me@example.com",
				AuthMechanism: AuthXOAuth2,
				TokenSource: func(context.Context) (string, error) {
					return "", errors.New("helper exploded")
				},
			},
			wantErrText: "helper exploded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mechanism, client, err := tt.account.saslExchange(context.Background())

			if tt.wantErr == nil && tt.wantErrText == "" {
				if err != nil {
					t.Fatalf("saslExchange: %v", err)
				}
				if mechanism != tt.wantMechanism {
					t.Errorf("mechanism = %q, want %q", mechanism, tt.wantMechanism)
				}
				if (client == nil) != (tt.wantMechanism == "") {
					t.Errorf("client = %v for mechanism %q", client, mechanism)
				}
				return
			}

			if err == nil {
				t.Fatalf("saslExchange succeeded, want an error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want one wrapping %v", err, tt.wantErr)
			}
			if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantErrText)
			}
		})
	}
}

func TestMechanismOffered(t *testing.T) {
	offered := []string{"PLAIN", "LOGIN", "XOAUTH2"}

	if !mechanismOffered("XOAUTH2", offered) {
		t.Error("XOAUTH2 is in the list")
	}
	// Servers are not consistent about case in a capability list.
	if !mechanismOffered("xoauth2", offered) {
		t.Error("the comparison should ignore case")
	}
	if mechanismOffered("OAUTHBEARER", offered) {
		t.Error("OAUTHBEARER is not in the list")
	}
	if mechanismOffered("PLAIN", nil) {
		t.Error("nothing is offered by a server that named nothing")
	}
}

// A server that does not do OAuth at all should say so, rather than let the
// attempt fail as though the token were the problem. The two are fixed in
// entirely different places.
func TestConnect_SaysWhenTheServerHasNoSuchMechanism(t *testing.T) {
	srv := testimap.Start(t, testIMAPUser, testIMAPPass, testimap.Hooks{})

	client := NewIMAPClient(AccountConfig{
		Address:        testIMAPUser,
		IMAPHost:       srv.Host,
		IMAPPort:       srv.Port,
		IMAPEncryption: "none",
		AuthMechanism:  AuthXOAuth2,
		TokenSource:    staticToken("tok"),
	}, discardLogger())

	err := client.Connect(context.Background())
	if err == nil {
		_ = client.Close()
		t.Fatal("connecting with a mechanism the server lacks succeeded")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("error = %v, want one wrapping ErrAuthFailed", err)
	}
	if !strings.Contains(err.Error(), "XOAUTH2") {
		t.Errorf("error = %v, want it to name the mechanism that is missing", err)
	}
}

// A broken token helper must not be reported as a network or credential
// problem: the thing to go and fix is the helper.
func TestConnect_ReportsAFailingTokenSource(t *testing.T) {
	srv := testimap.Start(t, testIMAPUser, testIMAPPass, testimap.Hooks{})

	client := NewIMAPClient(AccountConfig{
		Address:        testIMAPUser,
		IMAPHost:       srv.Host,
		IMAPPort:       srv.Port,
		IMAPEncryption: "none",
		AuthMechanism:  AuthXOAuth2,
		TokenSource: func(context.Context) (string, error) {
			return "", errors.New("no cached credential")
		},
	}, discardLogger())

	err := client.Connect(context.Background())
	if err == nil {
		_ = client.Close()
		t.Fatal("connecting with a failing token source succeeded")
	}
	if !strings.Contains(err.Error(), "no cached credential") {
		t.Errorf("error = %v, want the helper's own complaint", err)
	}
}
