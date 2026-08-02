package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
	"github.com/lgforsberg/bifrost/internal/config"
	"github.com/lgforsberg/bifrost/internal/output"
)

func Config(g *cmdutil.GlobalFlags, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: config <init>")
	}

	switch args[0] {
	case "init":
		return configInit(g, args[1:])
	case "help", "--help", "-h":
		return g.Usage("usage: config <init>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (init)", args[0])
	}
}

func configInit(g *cmdutil.GlobalFlags, args []string) error {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	// Accepted here as well as globally. Writing a config somewhere the caller
	// did not ask for is worse than taking the same flag in two places, and
	// after the command is where people reach for it.
	pathFlag := fs.String("config", "", "path to write the config to")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: config init takes no arguments, got %q", fs.Arg(0))
	}

	cfgPath := config.DefaultConfigPath()
	if g.ConfigPath != "" {
		cfgPath = g.ConfigPath
	}
	if *pathFlag != "" {
		cfgPath = *pathFlag
	}

	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("config file already exists at %s", cfgPath)
	}

	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(cfgPath, []byte(config.TemplateJSON()), 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	if g.JSON {
		return output.PrintJSON(g.Out(), map[string]string{"status": "created", "path": cfgPath})
	} else {
		fmt.Fprintf(g.Out(), "Config created at %s\nEdit it with your account details.\n", cfgPath)
	}
	return nil
}
