package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "logx [pod-name]",
	Short: "logx - enhanced Kubernetes pod logs",
	Long: `logx is a command-line tool designed to simplify and enhance
the process of viewing Kubernetes pod logs.

It provides features such as:
- Fetching and displaying logs from specific pods and containers
- Real-time log following
- Listing containers within a pod
- Color-coded output for improved readability

Use "logx [command] --help" for more information about a command.`,
	Args: cobra.MaximumNArgs(1),
	// Errors are surfaced once by main via Execute; cobra should not also print
	// them or the usage text on a runtime (non-usage) error.
	SilenceErrors: true,
	SilenceUsage:  true,
	// Configure colorized rendering once, before any command writes output.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return applyOutputConfig(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// With no pod name, only proceed if a label selector was given;
			// otherwise show help.
			if selector, _ := cmd.Flags().GetString(flagSelector); selector == "" {
				return cmd.Help()
			}
		}
		return runLogs(cmd, args)
	},
}

// Execute runs the root command and returns any error to the caller (main),
// which is responsible for reporting it and setting the exit code. It installs a
// signal-aware context so SIGINT/SIGTERM cancel in-flight operations (notably a
// --follow log stream) cleanly rather than killing the process abruptly.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)
	// If the run was interrupted by a signal (e.g. Ctrl-C on a --follow stream),
	// treat it as a clean cancellation rather than reporting the resulting
	// "context canceled" error and exiting non-zero.
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

func init() {
	addKubeFlags(rootCmd)
	addLogFlags(rootCmd)
	addOutputFlags(rootCmd)
	rootCmd.ValidArgsFunction = completePodNames
	_ = rootCmd.RegisterFlagCompletionFunc(flagContainer, completeContainerNames)
}
