package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-sasl"
)

// Authentication mechanisms an account can ask for.
//
// AuthPassword is the zero value, so an account that says nothing keeps the
// behaviour it had: IMAP LOGIN and SMTP AUTH PLAIN.
const (
	AuthPassword    = ""
	AuthXOAuth2     = "xoauth2"
	AuthOAuthBearer = "oauthbearer"
)

// TokenSource returns a bearer token for one connection attempt.
//
// It is a function rather than a string for two reasons. An access token
// lives about an hour, so it has to be fetched when the connection is made
// rather than when the config is read. And keeping it behind a call means the
// token never sits in the config file or in AccountConfig at rest, which
// matters more for a token than for a password: a token is a bearer
// credential that no second factor protects.
type TokenSource func(ctx context.Context) (string, error)

// saslExchange picks the SASL mechanism an account has asked for and gets it
// a token. It returns an empty mechanism and a nil client for password auth,
// which IMAP and SMTP each handle their own way.
func (a *AccountConfig) saslExchange(ctx context.Context) (string, sasl.Client, error) {
	mechanism := strings.ToLower(strings.TrimSpace(a.AuthMechanism))
	if mechanism == AuthPassword {
		return "", nil, nil
	}

	if a.TokenSource == nil {
		return "", nil, fmt.Errorf("%s needs a token source, but none is configured: %w", mechanism, ErrInvalidConfig)
	}

	token, err := a.TokenSource(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("getting an access token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		// Sending it would produce a refusal from the server that reads like
		// a scope or expiry problem, sending the reader after the wrong thing.
		return "", nil, fmt.Errorf("the token source returned nothing: %w", ErrAuthFailed)
	}

	username := a.EffectiveUsername()
	switch mechanism {
	case AuthXOAuth2:
		return "XOAUTH2", &xoauth2Client{username: username, token: token}, nil
	case AuthOAuthBearer:
		return "OAUTHBEARER", sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
			Username: username,
			Token:    token,
		}), nil
	default:
		return "", nil, fmt.Errorf("unknown auth mechanism %q, want %q or %q: %w",
			a.AuthMechanism, AuthXOAuth2, AuthOAuthBearer, ErrInvalidConfig)
	}
}

// xoauth2Client implements Google's XOAUTH2, which go-sasl does not carry.
//
// XOAUTH2 rather than the standardised OAUTHBEARER (RFC 7628) because
// Microsoft 365 documents XOAUTH2 as the mechanism for IMAP and SMTP and does
// not offer the other, while Gmail advertises both. One mechanism therefore
// reaches both providers, which between them are most of what an account
// needs OAuth for at all.
type xoauth2Client struct {
	username string
	token    string

	// challenge is what the server said when it refused. The mechanism
	// reports failure by challenging with a JSON document rather than by
	// refusing outright, and that document names the scope that was missing
	// or the expiry that passed. It is the one useful thing to hand back to
	// someone whose token is not working.
	challenge string
}

// Start returns the initial client response. The caller applies base64.
func (c *xoauth2Client) Start() (string, []byte, error) {
	// The separator is a Control-A, per Google's definition of the mechanism
	// and Microsoft's, which agree byte for byte.
	return "XOAUTH2", []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

// Next answers a challenge, which only ever means the token was refused: a
// successful exchange is a single round trip. The protocol wants an empty
// reply before the server will state the failure, so answering is what lets
// the real error arrive rather than a broken exchange.
func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	c.challenge = strings.TrimSpace(string(challenge))
	return []byte{}, nil
}

// saslFailureDetail returns what the mechanism learned about a refusal, ready
// to append to an error message. Without it an XOAUTH2 failure says only that
// authentication did not work, because the reason arrived in a challenge
// rather than in the server's refusal.
func saslFailureDetail(client sasl.Client) string {
	c, ok := client.(*xoauth2Client)
	if !ok || c.challenge == "" {
		return ""
	}
	return c.challenge
}

// mechanismOffered reports whether the server named this mechanism. Checking
// first turns "authentication failed" into a sentence that says the server
// was never going to accept this, and lists what it would have accepted.
func mechanismOffered(mechanism string, offered []string) bool {
	for _, m := range offered {
		if strings.EqualFold(m, mechanism) {
			return true
		}
	}
	return false
}
