package cmdutil

import (
	"context"
	"log/slog"

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
}
