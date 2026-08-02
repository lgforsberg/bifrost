package cmdutil

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/lgforsberg/bifrost/internal/config"
)

type GlobalFlags struct {
	Account    string
	JSON       bool
	Verbose    bool
	ConfigPath string
	Ctx        context.Context
	Logger     *slog.Logger
	Config     *config.Config

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
