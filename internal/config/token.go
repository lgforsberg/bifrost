package config

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lgforsberg/bifrost/mail"
)

// defaultTokenTimeout bounds a token helper that the account has not given a
// timeout of its own. It is generous for a command that reads a cached token
// off disk or asks a local agent, and far too short for one that waits on a
// person, which is the intent: the CLI never blocks on interactive input, so
// neither may a helper it runs.
const defaultTokenTimeout = 30 * time.Second

// maxStderrDetail caps how much of a failing helper's diagnostics we repeat.
// Enough to carry a real message, not enough to bury the error under a stack
// trace or a usage screen.
const maxStderrDetail = 500

// tokenCommandSource returns a TokenSource that runs argv and reads an access
// token from its standard output.
//
// The command is a list of arguments rather than a string for a shell to
// split. It costs a little convenience in the config file and removes a class
// of problem entirely: no quoting rules to get wrong, and no path to shell
// interpretation of a value that came out of a file.
//
// Delegating to a command is the whole design. Acquiring an OAuth token means
// provider-specific endpoints, client registrations and scopes, plus a
// browser consent step at least once, and a refresh token to keep safe
// afterwards. Bifrost would have to become a secrets store and pick sides
// between providers to do that itself, and it would have to prompt, which it
// does not do. A command composes with whatever already holds the
// credentials: gcloud, az, a password manager's CLI, a refresher on a timer.
func tokenCommandSource(argv []string, timeout time.Duration) mail.TokenSource {
	if timeout <= 0 {
		timeout = defaultTokenTimeout
	}
	name := expandTilde(argv[0])
	args := argv[1:]

	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, name, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// No stdin: a helper that decides to prompt should fail rather than
		// wait, and Bifrost's own stdin may be a message body being piped in.
		cmd.Stdin = nil

		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("token command %s did not finish within %s: %w",
					name, timeout, mail.ErrAuthFailed)
			}
			return "", fmt.Errorf("token command %s failed: %w%s: %w",
				name, err, stderrDetail(stderr.String()), mail.ErrAuthFailed)
		}

		token := strings.TrimSpace(stdout.String())
		if token == "" {
			return "", fmt.Errorf("token command %s printed no token%s: %w",
				name, stderrDetail(stderr.String()), mail.ErrAuthFailed)
		}
		// A helper that prints a banner, or the whole JSON token response,
		// would otherwise send the lot as a credential and get back a
		// refusal that says nothing about the real mistake.
		if strings.ContainsAny(token, "\r\n") {
			return "", fmt.Errorf("token command %s printed more than one line, want the access token alone: %w",
				name, mail.ErrAuthFailed)
		}
		return token, nil
	}
}

// stderrDetail formats a helper's diagnostics for inclusion in an error,
// returning "" when it said nothing.
func stderrDetail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > maxStderrDetail {
		s = s[:maxStderrDetail] + "..."
	}
	return " (" + strings.ReplaceAll(s, "\n", "; ") + ")"
}

// validateAuth checks that an account's authentication settings make sense
// together, so a mistake is reported when the config is read rather than
// after a connection has been opened and refused.
//
// Settings that do nothing are rejected rather than ignored: a password left
// beside a token mechanism, or a token command with no mechanism to use it,
// both read as working configuration and are not.
func validateAuth(acct accountJSON) error {
	mechanism := strings.ToLower(strings.TrimSpace(acct.AuthMechanism))

	if mechanism == mail.AuthPassword {
		if len(acct.TokenCommand) > 0 {
			return fmt.Errorf("tokenCommand is set but authMechanism is not, so the token would never be used; set authMechanism to %q: %w",
				mail.AuthXOAuth2, mail.ErrInvalidConfig)
		}
		return nil
	}

	if mechanism != mail.AuthXOAuth2 && mechanism != mail.AuthOAuthBearer {
		return fmt.Errorf("authMechanism %q is not one Bifrost knows, want %q or %q: %w",
			acct.AuthMechanism, mail.AuthXOAuth2, mail.AuthOAuthBearer, mail.ErrInvalidConfig)
	}
	if len(acct.TokenCommand) == 0 || strings.TrimSpace(acct.TokenCommand[0]) == "" {
		return fmt.Errorf("authMechanism %q needs a tokenCommand to get a token from: %w",
			mechanism, mail.ErrInvalidConfig)
	}
	if acct.Password != "" || acct.PasswordFile != "" {
		return fmt.Errorf("authMechanism %q is set, so the password would never be used; remove password and passwordFile: %w",
			mechanism, mail.ErrInvalidConfig)
	}
	return nil
}
