package commands

import (
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
		return configInit(g)
	case "help", "--help", "-h":
		return fmt.Errorf("usage: config <init>")
	default:
		return fmt.Errorf("usage: unknown subcommand %q (init)", args[0])
	}
}

func configInit(g *cmdutil.GlobalFlags) error {
	cfgPath := config.DefaultConfigPath()
	if g.ConfigPath != "" {
		cfgPath = g.ConfigPath
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
		return output.PrintJSON(os.Stdout, map[string]string{"status": "created", "path": cfgPath})
	} else {
		fmt.Printf("Config created at %s\nEdit it with your account details.\n", cfgPath)
	}
	return nil
}
