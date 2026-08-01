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
