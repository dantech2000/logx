package logging

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// Theme holds the colors logx uses to render a log entry. Themes are swappable
// so output stays readable on both dark and light terminals. The colors still
// obey the global color.NoColor switch (see ApplyColorMode), so a theme only
// decides which codes are used when color is on.
type Theme struct {
	levels    map[LogLevel]*color.Color
	timestamp *color.Color
	logger    *color.Color
	key       *color.Color
	value     *color.Color
	quote     *color.Color
	errorText *color.Color
}

// darkTheme is the default, tuned for dark terminal backgrounds.
func darkTheme() *Theme {
	return &Theme{
		levels: map[LogLevel]*color.Color{
			TRACE: color.New(color.FgHiBlack),
			DEBUG: color.New(color.FgCyan),
			INFO:  color.New(color.FgGreen),
			WARN:  color.New(color.FgYellow),
			ERROR: color.New(color.FgRed),
			FATAL: color.New(color.FgWhite, color.BgRed, color.Bold),
		},
		timestamp: color.New(color.FgBlue),
		logger:    color.New(color.FgMagenta),
		key:       color.New(color.FgCyan),
		value:     color.New(color.FgWhite),
		quote:     color.New(color.FgHiBlack),
		errorText: color.New(color.FgRed, color.Bold),
	}
}

// lightTheme is tuned for light terminal backgrounds: it is the dark theme with
// the low-contrast cyan/white swapped for blue/black (DEBUG level, keys, values).
func lightTheme() *Theme {
	t := darkTheme()
	t.levels[DEBUG] = color.New(color.FgBlue)
	t.key = color.New(color.FgBlue)
	t.value = color.New(color.FgBlack)
	return t
}

// activeTheme is the theme used by the formatter. It is configured once at
// startup (SetTheme) before any streaming begins; the formatter only reads it.
var activeTheme = darkTheme()

// levelColor returns the color for a level, defaulting to the DEBUG color for an
// unknown level so output never loses its level column.
func (t *Theme) levelColor(level LogLevel) *color.Color {
	if c, ok := t.levels[level]; ok {
		return c
	}
	return t.levels[DEBUG]
}

// SetTheme selects the active color theme by name ("dark", "light", or "auto"/""
// for the default dark theme).
func SetTheme(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto", "dark":
		activeTheme = darkTheme()
	case "light":
		activeTheme = lightTheme()
	default:
		return fmt.Errorf("unknown theme %q (valid: dark, light)", name)
	}
	return nil
}

// ColorMode controls whether output is colorized.
type ColorMode int

// Color modes: Auto follows the terminal (TTY + NO_COLOR), Always and Never force
// the decision.
const (
	ColorAuto ColorMode = iota
	ColorAlways
	ColorNever
)

// ParseColorMode parses a --color value.
func ParseColorMode(s string) (ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ColorAuto, nil
	case "always", "force", "on", "true":
		return ColorAlways, nil
	case "never", "off", "false", "none":
		return ColorNever, nil
	default:
		return ColorAuto, fmt.Errorf("unknown color mode %q (valid: auto, always, never)", s)
	}
}

// defaultNoColor captures fatih/color's startup decision (driven by NO_COLOR,
// TERM=dumb, and whether stdout is a TTY) so ColorAuto can restore it exactly.
var defaultNoColor = color.NoColor

// ApplyColorMode applies a color mode globally. ColorAuto restores the terminal's
// own decision (so NO_COLOR and non-TTY output stay uncolored); Always and Never
// override it. An explicit mode always wins over the environment.
func ApplyColorMode(mode ColorMode) {
	switch mode {
	case ColorAlways:
		color.NoColor = false
	case ColorNever:
		color.NoColor = true
	default:
		color.NoColor = defaultNoColor
	}
}
