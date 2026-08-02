package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lgforsberg/bifrost/internal/cmdutil"
)

// The README documents the flag after the command, which used to be swallowed
// silently: the config landed at the default path instead, and nothing said so.
// Both paths here are temporary, so a regression cannot write to a real home
// directory.
func TestConfigInit_FlagAfterTheCommandWins(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	explicit := filepath.Join(dir, "explicit.json")

	err := Config(&cmdutil.GlobalFlags{ConfigPath: global}, []string{"init", "--config", explicit})
	if err != nil {
		t.Fatalf("config init: %v", err)
	}

	if _, err := os.Stat(explicit); err != nil {
		t.Errorf("nothing written to the path given after the command: %v", err)
	}
	if _, err := os.Stat(global); err == nil {
		t.Error("the flag after the command was ignored in favour of the global one")
	}
}

func TestConfigInit_UsesTheGlobalFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := Config(&cmdutil.GlobalFlags{ConfigPath: path}, []string{"init"}); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("nothing written to the global config path: %v", err)
	}
}

// The environment variable was ignored here while every other command
// honoured it, so `config init` wrote the template to the default path and
// the next command went looking somewhere else and found nothing.
func TestConfigInit_UsesTheEnvironmentVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "from-env.json")
	t.Setenv("BIFROST_CONFIG", path)

	if err := Config(&cmdutil.GlobalFlags{}, []string{"init"}); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("nothing written to the path BIFROST_CONFIG names: %v", err)
	}
}

// A flag is a deliberate instruction for one invocation; the environment is
// standing configuration, so the flag wins.
func TestConfigInit_FlagBeatsTheEnvironmentVariable(t *testing.T) {
	dir := t.TempDir()
	fromEnv := filepath.Join(dir, "from-env.json")
	fromFlag := filepath.Join(dir, "from-flag.json")
	t.Setenv("BIFROST_CONFIG", fromEnv)

	if err := Config(&cmdutil.GlobalFlags{ConfigPath: fromFlag}, []string{"init"}); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if _, err := os.Stat(fromFlag); err != nil {
		t.Errorf("nothing written to the path the flag names: %v", err)
	}
	if _, err := os.Stat(fromEnv); err == nil {
		t.Error("the environment variable overrode the flag")
	}
}

// Writing over a working config would cost someone their account setup.
func TestConfigInit_RefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"accounts":[]}`), 0600); err != nil {
		t.Fatalf("seeding a config: %v", err)
	}

	if err := Config(&cmdutil.GlobalFlags{ConfigPath: path}, []string{"init"}); err == nil {
		t.Fatal("config init overwrote an existing config")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(data) != `{"accounts":[]}` {
		t.Error("the existing config was modified")
	}
}

func TestConfigInit_RejectsStrayArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	err := Config(&cmdutil.GlobalFlags{ConfigPath: path}, []string{"init", "extra"})
	if err == nil {
		t.Fatal("config init accepted an argument it does not understand")
	}
	// The usage: prefix is what main maps to exit code 2.
	if !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("error %q should be a usage error", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a config was written despite the bad invocation")
	}
}
