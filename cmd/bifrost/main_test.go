package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/mail"
)

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// The code and the exit status are the contract a script branches on, so they
// are worth pinning one by one.
func TestClassifyError(t *testing.T) {
	tests := map[string]struct {
		err      error
		ctx      context.Context
		wantCode string
		wantExit int
	}{
		"not found": {
			err:      fmt.Errorf("message uid 7: %w", mail.ErrNotFound),
			wantCode: "NOT_FOUND",
			wantExit: 1,
		},
		"already exists": {
			err:      fmt.Errorf("folder Work: %w", mail.ErrAlreadyExists),
			wantCode: "ALREADY_EXISTS",
			wantExit: 1,
		},
		"auth failed": {
			err:      fmt.Errorf("login: %w", mail.ErrAuthFailed),
			wantCode: "AUTH_FAILED",
			wantExit: 1,
		},
		"connection failed": {
			err:      fmt.Errorf("dial: %w", mail.ErrConnectionFailed),
			wantCode: "CONNECTION_FAILED",
			wantExit: 1,
		},
		"send rejected": {
			err:      fmt.Errorf("smtp: %w", mail.ErrSendRejected),
			wantCode: "SEND_REJECTED",
			wantExit: 1,
		},
		"invalid config exits 2": {
			err:      fmt.Errorf("no accounts: %w", mail.ErrInvalidConfig),
			wantCode: "CONFIG_ERROR",
			wantExit: 2,
		},
		"usage exits 2": {
			err:      errors.New("usage: read <uid>"),
			wantCode: "USAGE_ERROR",
			wantExit: 2,
		},
		"anything else": {
			err:      errors.New("something went sideways"),
			wantCode: "UNKNOWN",
			wantExit: 1,
		},
		// An interrupt drops the connection underneath the command, so
		// whatever surfaced is a symptom. Reporting it as a connection failure
		// would send someone debugging their network.
		"interrupt outranks the symptom": {
			err:      fmt.Errorf("dial: %w", mail.ErrConnectionFailed),
			ctx:      cancelledContext(),
			wantCode: "INTERRUPTED",
			wantExit: 1,
		},
		// A usage error is decided before the command runs, so an interrupt
		// arriving afterwards does not change what was wrong.
		"usage outranks an interrupt": {
			err:      errors.New("usage: read <uid>"),
			ctx:      cancelledContext(),
			wantCode: "USAGE_ERROR",
			wantExit: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			code, exit, reported := classifyError(&cmdutil.GlobalFlags{Ctx: tt.ctx}, tt.err)

			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if exit != tt.wantExit {
				t.Errorf("exit = %d, want %d", exit, tt.wantExit)
			}
			if reported == nil {
				t.Fatal("no error to report")
			}
			// The original has to survive, since the message is all a reader
			// gets to work out what happened.
			if !errors.Is(reported, tt.err) {
				t.Errorf("reported %v, which no longer wraps the original", reported)
			}
		})
	}
}

func TestClassifyError_InterruptSaysSo(t *testing.T) {
	_, _, reported := classifyError(
		&cmdutil.GlobalFlags{Ctx: cancelledContext()},
		fmt.Errorf("dial: %w", mail.ErrConnectionFailed),
	)

	want := "interrupted: dial: connection failed"
	if reported.Error() != want {
		t.Errorf("message = %q, want %q", reported.Error(), want)
	}
}
