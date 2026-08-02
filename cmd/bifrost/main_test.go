package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
		"pending approval": {
			err:      fmt.Errorf("draft 7 is awaiting approval: %w", mail.ErrPendingApproval),
			wantCode: "PENDING_APPROVAL",
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

// wantsHelp decides whether a broken config is worth reporting, so a false
// positive hides a real error and a false negative makes usage unreachable
// for exactly the person who needs it.
func TestWantsHelp(t *testing.T) {
	tests := map[string]struct {
		args []string
		want bool
	}{
		"long form":                    {args: []string{"--help"}, want: true},
		"short form":                   {args: []string{"-h"}, want: true},
		"after other flags":            {args: []string{"--folder", "INBOX", "--help"}, want: true},
		"help as a subcommand":         {args: []string{"help"}, want: true},
		"subcommand then long form":    {args: []string{"folder", "--help"}, want: true},
		"nothing at all":               {args: nil, want: false},
		"ordinary work":                {args: []string{"--limit", "20"}, want: false},
		"help as a flag value":         {args: []string{"--subject", "help"}, want: false},
		"help as a positional":         {args: []string{"create", "help"}, want: false},
		"after the end-of-flags guard": {args: []string{"--", "--help"}, want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := wantsHelp(tt.args); got != tt.want {
				t.Errorf("wantsHelp(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// The global flags are parsed by hand rather than by the flag package, so
// every form the flag package would accept has to be checked here.
func TestParseGlobalFlags(t *testing.T) {
	tests := map[string]struct {
		args        []string
		wantAccount string
		wantConfig  string
		wantJSON    bool
		wantVerbose bool
		wantRest    []string
		wantErr     string
	}{
		"nothing": {
			args: nil,
		},
		"command only": {
			args:     []string{"inbox"},
			wantRest: []string{"inbox"},
		},
		"separate value": {
			args:        []string{"--account", "alice", "inbox"},
			wantAccount: "alice",
			wantRest:    []string{"inbox"},
		},
		"attached value": {
			args:        []string{"--account=alice", "inbox"},
			wantAccount: "alice",
			wantRest:    []string{"inbox"},
		},
		"one dash is the same as two": {
			args:        []string{"-account", "alice", "-json", "inbox"},
			wantAccount: "alice",
			wantJSON:    true,
			wantRest:    []string{"inbox"},
		},
		"a value may look like a flag": {
			args:       []string{"--config", "--weird.json", "inbox"},
			wantConfig: "--weird.json",
			wantRest:   []string{"inbox"},
		},
		"booleans take an explicit value": {
			args:     []string{"--json=false", "--verbose=true", "inbox"},
			wantJSON: false, wantVerbose: true,
			wantRest: []string{"inbox"},
		},
		"the command's own flags are left alone": {
			args:     []string{"--json", "search", "--json", "--limit", "5"},
			wantJSON: true,
			wantRest: []string{"search", "--json", "--limit", "5"},
		},
		"end of flags": {
			args:     []string{"--json", "--", "inbox"},
			wantJSON: true,
			wantRest: []string{"inbox"},
		},
		"help passes through to be dispatched": {
			args:     []string{"--help"},
			wantRest: []string{"--help"},
		},
		"short help passes through": {
			args:     []string{"-h"},
			wantRest: []string{"-h"},
		},

		"a missing value is not silently ignored": {
			args:    []string{"--account"},
			wantErr: `usage: --account needs a value`,
		},
		"a missing config value is not silently ignored": {
			args:    []string{"--config"},
			wantErr: `usage: --config needs a value`,
		},
		"an empty value is a mistake too": {
			args:    []string{"--account=", "inbox"},
			wantErr: `usage: --account was given an empty value`,
		},
		"an unparseable boolean": {
			args:    []string{"--json=maybe", "inbox"},
			wantErr: `usage: --json takes true or false, not "maybe"`,
		},
		"an unknown global option is named as such": {
			args:    []string{"--bogus", "inbox"},
			wantErr: `usage: unknown global option "--bogus"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			g, rest, err := parseGlobalFlags(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseGlobalFlags(%q) succeeded, want %q", tt.args, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGlobalFlags(%q): %v", tt.args, err)
			}

			if g.Account != tt.wantAccount {
				t.Errorf("account = %q, want %q", g.Account, tt.wantAccount)
			}
			if g.ConfigPath != tt.wantConfig {
				t.Errorf("config = %q, want %q", g.ConfigPath, tt.wantConfig)
			}
			if g.JSON != tt.wantJSON {
				t.Errorf("json = %v, want %v", g.JSON, tt.wantJSON)
			}
			if g.Verbose != tt.wantVerbose {
				t.Errorf("verbose = %v, want %v", g.Verbose, tt.wantVerbose)
			}
			if !slices.Equal(rest, tt.wantRest) {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

// Every usage error the parser reports has to carry the prefix that maps it to
// exit code 2, or a caller cannot tell a mistyped invocation from a failed one.
func TestParseGlobalFlags_ErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"--account"},
		{"--config"},
		{"--account="},
		{"--json=maybe"},
		{"--bogus"},
	} {
		_, _, err := parseGlobalFlags(args)
		if err == nil {
			t.Fatalf("parseGlobalFlags(%q) succeeded, want a usage error", args)
		}
		_, exit, _ := classifyError(&cmdutil.GlobalFlags{}, err)
		if exit != 2 {
			t.Errorf("parseGlobalFlags(%q) error %q exits %d, want 2", args, err, exit)
		}
	}
}

func TestDescribeBuild(t *testing.T) {
	got := describeBuild()

	// Whatever the toolchain stamped, the binary must be able to name a
	// version rather than an empty string.
	if got.Version == "" {
		t.Error("describeBuild reported no version at all")
	}

	// A modified tree is not the tag it sits on, so the constant has to win
	// there or a development build reports the previous release.
	if got.Modified && got.Version != version {
		t.Errorf("version = %q on a modified tree, want the constant %q", got.Version, version)
	}

	for name, tt := range map[string]struct {
		build buildInfo
		want  string
	}{
		"no commit to add": {
			build: buildInfo{Version: "1.2.3"},
			want:  "1.2.3",
		},
		"a commit is shortened": {
			build: buildInfo{Version: "1.2.3", Revision: "db07a1ba22f23e39a4f46cf0064ff7f083ca90ec"},
			want:  "1.2.3 (db07a1ba22f2)",
		},
		"an unclean tree says so": {
			build: buildInfo{Version: "1.2.3", Revision: "db07a1ba22f23e39", Modified: true},
			want:  "1.2.3 (db07a1ba22f2, modified)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tt.build.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
