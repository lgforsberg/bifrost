package cmdutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lgforsberg/bifrost/internal/config"
)

type GlobalFlags struct {
	Account    string
	JSON       bool
	Verbose    bool
	ConfigPath string

	// Timeout overrides the configured network timeout for this invocation.
	// Zero means whatever the config said, which may itself be zero for the
	// built-in defaults.
	Timeout time.Duration
	Ctx     context.Context
	Logger  *slog.Logger
	Config  *config.Config

	// Where output goes. Both are nil outside tests, which is why commands
	// reach for them through Out and Err rather than directly.
	Stdout io.Writer
	Stderr io.Writer
}

// Out is where a command's result goes: JSON, tables, everything a caller
// might pipe somewhere.
func (g *GlobalFlags) Out() io.Writer {
	if g.Stdout == nil {
		return os.Stdout
	}
	return g.Stdout
}

// Err is for anything that is not the result, such as the warnings a send
// reports after the message has already gone out. Keeping those off stdout is
// what lets a script parse the result without filtering.
func (g *GlobalFlags) Err() io.Writer {
	if g.Stderr == nil {
		return os.Stderr
	}
	return g.Stderr
}

// Usage writes usage text and reports success, for a caller that asked what a
// command takes. Returning an error here would exit 2, which is the code for
// getting the invocation wrong; asking is not the same thing. It goes to
// stderr because that is where the flag package puts the same text, and one
// stream for all usage beats two.
func (g *GlobalFlags) Usage(text string) error {
	fmt.Fprintln(g.Err(), text)
	return nil
}
