package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("LOGX_CONFIG", path)
	return path
}

func TestConfigPathPrecedence(t *testing.T) {
	// $LOGX_CONFIG wins outright.
	t.Setenv("LOGX_CONFIG", "/explicit/config.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := configPath(); got != "/explicit/config.yaml" {
		t.Errorf("configPath() = %q, want $LOGX_CONFIG to win", got)
	}

	// Without it, $XDG_CONFIG_HOME/logx/config.yaml.
	t.Setenv("LOGX_CONFIG", "")
	if got, want := configPath(), filepath.Join("/xdg", "logx", "config.yaml"); got != want {
		t.Errorf("configPath() = %q, want %q (from $XDG_CONFIG_HOME)", got, want)
	}

	// Without either, ~/.config/logx/config.yaml.
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}
	if got, want := configPath(), filepath.Join(home, ".config", "logx", "config.yaml"); got != want {
		t.Errorf("configPath() = %q, want %q (home fallback)", got, want)
	}
}

func TestApplyFieldAliasesRegistersConfigFields(t *testing.T) {
	// Alias registration is deliberately global and additive (a startup-only
	// operation with no unregister), so this test uses keys namespaced with a
	// zz_cfg_ prefix that no other test input contains.
	var cfg fileConfig
	cfg.Fields.Level = []string{"zz_cfg_level"}
	cfg.Fields.Message = []string{"zz_cfg_msg"}
	cfg.applyFieldAliases()

	entry := logging.ParseLogEntry(`{"zz_cfg_level":"error","zz_cfg_msg":"db down"}`)
	if entry.Level != logging.ERROR || !entry.LevelDetected {
		t.Errorf("config level alias not applied: level=%v detected=%v", entry.Level, entry.LevelDetected)
	}
	if entry.Message != "db down" {
		t.Errorf("config message alias not applied: %q", entry.Message)
	}
}

func TestApplyFieldAliasesEmptyConfigIsNoop(t *testing.T) {
	var cfg fileConfig
	cfg.applyFieldAliases() // no fields configured: must not disturb built-ins

	entry := logging.ParseLogEntry(`{"level":"warn","msg":"disk almost full"}`)
	if entry.Level != logging.WARN || entry.Message != "disk almost full" {
		t.Errorf("built-in parsing disturbed: level=%v message=%q", entry.Level, entry.Message)
	}
}

func TestLoadConfigValid(t *testing.T) {
	writeConfig(t, "level: WARN\ntheme: light\ncolor: never\nfields:\n  level: [lvl]\n")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error: %v", err)
	}
	if cfg.Level != "WARN" || cfg.Theme != "light" || cfg.Color != "never" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Fields.Level) != 1 || cfg.Fields.Level[0] != "lvl" {
		t.Fatalf("custom level field not parsed: %+v", cfg.Fields)
	}
}

func TestLoadConfigMissingIsNotError(t *testing.T) {
	t.Setenv("LOGX_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("missing config should not error, got %v", err)
	}
	if cfg.Level != "" {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestLoadConfigMalformedErrors(t *testing.T) {
	writeConfig(t, "level: WARN\n\tbad: : :\n")
	if _, err := loadConfig(); err == nil {
		t.Fatal("malformed config should error")
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	writeConfig(t, "unknown_setting: WARN\n") // not a recognized key
	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown config key should error (strict parsing)")
	}
}

func TestStringDefaultPrecedence(t *testing.T) {
	// flag explicitly set wins over config.
	if got := stringDefault("flagval", "cfgval", true); got != "flagval" {
		t.Errorf("explicit flag should win, got %q", got)
	}
	// flag unset falls back to config.
	if got := stringDefault("default", "cfgval", false); got != "cfgval" {
		t.Errorf("config should fill in, got %q", got)
	}
	// neither: keep the flag default.
	if got := stringDefault("default", "", false); got != "default" {
		t.Errorf("built-in default should remain, got %q", got)
	}
}

func TestEffectiveLevelUsesConfigThenFlag(t *testing.T) {
	prev := loadedConfig
	t.Cleanup(func() { loadedConfig = prev })
	loadedConfig = fileConfig{Level: "WARN"}

	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x"}
		c.Flags().StringP(flagLevel, "l", "DEBUG", "")
		return c
	}

	// Flag not set → config WARN.
	c := newCmd()
	if err := c.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := effectiveLevel(c); got != "WARN" {
		t.Errorf("effectiveLevel = %q, want WARN (from config)", got)
	}

	// Flag set → flag wins.
	c = newCmd()
	if err := c.ParseFlags([]string{"--level", "ERROR"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := effectiveLevel(c); got != "ERROR" {
		t.Errorf("effectiveLevel = %q, want ERROR (explicit flag)", got)
	}
}
