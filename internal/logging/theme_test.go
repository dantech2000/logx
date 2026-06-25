package logging

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

// restoreColor saves and restores the global color/theme state so a test that
// forces color on or swaps the theme cannot leak into other tests.
func restoreColor(t *testing.T) {
	t.Helper()
	prevNoColor := color.NoColor
	prevTheme := activeTheme
	t.Cleanup(func() {
		color.NoColor = prevNoColor
		activeTheme = prevTheme
	})
}

func TestParseColorMode(t *testing.T) {
	cases := map[string]ColorMode{
		"":       ColorAuto,
		"auto":   ColorAuto,
		"always": ColorAlways,
		"force":  ColorAlways,
		"never":  ColorNever,
		"off":    ColorNever,
	}
	for in, want := range cases {
		got, err := ParseColorMode(in)
		if err != nil {
			t.Errorf("ParseColorMode(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseColorMode(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseColorMode("rainbow"); err == nil {
		t.Error("ParseColorMode(rainbow) should error")
	}
}

func TestApplyColorModeNeverProducesPlainOutput(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	out := FormatLogEntry(ParseLogEntry("ERROR boom"))
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("ColorNever still produced ANSI escapes: %q", out)
	}
}

func TestApplyColorModeAlwaysProducesColor(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorAlways)
	out := FormatLogEntry(ParseLogEntry("ERROR boom"))
	if !strings.ContainsRune(out, 0x1b) {
		t.Fatalf("ColorAlways produced no ANSI escapes: %q", out)
	}
}

func TestSetThemeChangesColors(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorAlways)

	if err := SetTheme("dark"); err != nil {
		t.Fatalf("SetTheme(dark) error: %v", err)
	}
	dark := FormatLogLevelLabel(DEBUG)

	if err := SetTheme("light"); err != nil {
		t.Fatalf("SetTheme(light) error: %v", err)
	}
	light := FormatLogLevelLabel(DEBUG)

	if dark == light {
		t.Fatalf("dark and light themes produced identical DEBUG labels: %q", dark)
	}
	if err := SetTheme("neon"); err == nil {
		t.Error("SetTheme(neon) should error on an unknown theme")
	}
}
