package cmd

import (
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// configWithArgs builds a bare command carrying the persistent output flags,
// parses the given flag args (which merges the persistent flags the way real
// execution does), and runs applyOutputConfig.
func configWithArgs(t *testing.T, args ...string) error {
	t.Helper()
	cmd := &cobra.Command{Use: "x"}
	addOutputFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags(%v) error: %v", args, err)
	}
	return applyOutputConfig(cmd)
}

func TestApplyOutputConfigColorModes(t *testing.T) {
	prev := color.NoColor
	t.Cleanup(func() { color.NoColor = prev })

	if err := configWithArgs(t, "--color=never"); err != nil {
		t.Fatalf("applyOutputConfig(never) error: %v", err)
	}
	if !color.NoColor {
		t.Fatal("--color=never should disable color")
	}

	if err := configWithArgs(t, "--color=always"); err != nil {
		t.Fatalf("applyOutputConfig(always) error: %v", err)
	}
	if color.NoColor {
		t.Fatal("--color=always should enable color")
	}

	// --no-color overrides --color=always.
	if err := configWithArgs(t, "--color=always", "--no-color"); err != nil {
		t.Fatalf("applyOutputConfig(no-color) error: %v", err)
	}
	if !color.NoColor {
		t.Fatal("--no-color should win over --color=always")
	}
}

func TestApplyOutputConfigRejectsBadValues(t *testing.T) {
	if err := configWithArgs(t, "--color=rainbow"); err == nil {
		t.Error("invalid --color should error")
	}
	if err := configWithArgs(t, "--theme=neon"); err == nil {
		t.Error("invalid --theme should error")
	}
}
