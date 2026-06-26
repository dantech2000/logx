package cmd

import (
	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

// addOutputFlags registers the persistent color/theme flags shared by every
// command. They are persistent on the root so `logs`, `parse`, and friends all
// honor them.
func addOutputFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagColor, "auto", "Colorize output: auto, always, or never")
	cmd.PersistentFlags().Bool(flagNoColor, false, "Disable colorized output (alias for --color=never)")
	cmd.PersistentFlags().String(flagTheme, "dark", "Color theme: dark or light")

	_ = cmd.RegisterFlagCompletionFunc(flagTheme, staticFlagCompletion(themeCompletions...))
	_ = cmd.RegisterFlagCompletionFunc(flagColor, staticFlagCompletion(colorCompletions...))
}

// applyOutputConfig resolves the color/theme flags and configures the logging
// package's global rendering. It runs from the root PersistentPreRunE, before any
// command produces output. An explicit --no-color or --color wins over the
// environment; otherwise auto follows the terminal (NO_COLOR + TTY).
func applyOutputConfig(cmd *cobra.Command) error {
	noColor, err := cmd.Flags().GetBool(flagNoColor)
	if err != nil {
		return err
	}
	colorStr, err := cmd.Flags().GetString(flagColor)
	if err != nil {
		return err
	}
	colorStr = stringDefault(colorStr, loadedConfig.Color, cmd.Flags().Changed(flagColor))
	mode, err := logging.ParseColorMode(colorStr)
	if err != nil {
		return err
	}
	if noColor {
		mode = logging.ColorNever
	}
	logging.ApplyColorMode(mode)

	theme, err := cmd.Flags().GetString(flagTheme)
	if err != nil {
		return err
	}
	theme = stringDefault(theme, loadedConfig.Theme, cmd.Flags().Changed(flagTheme))
	return logging.SetTheme(theme)
}
