package cmd

import (
	"context"
	"errors"
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
	// Load config and configure colorized rendering once, before any command
	// writes output.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		loadedConfig = cfg
		cfg.applyFieldAliases()
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

	// Restore the default signal disposition as soon as the first signal arrives.
	// signal.NotifyContext takes over SIGINT/SIGTERM for the whole process, and
	// its watcher goroutine exits after one signal — so without this a second
	// Ctrl-C was dropped on the floor and the process could only be killed with
	// SIGQUIT/SIGKILL. Now the first signal asks for a graceful stop and a second
	// terminates immediately, which is what users expect when something is stuck.
	go func() {
		<-ctx.Done()
		stop()
	}()

	err := rootCmd.ExecuteContext(ctx)
	if isCleanCancellation(err, ctx.Err()) {
		return nil
	}
	return err
}

// isCleanCancellation reports whether err should be treated as a clean exit
// because the run was interrupted by a signal.
//
// Both conditions matter. ctxErr proves a signal actually arrived, and err must
// itself be the cancellation: the original check tested only ctxErr != nil, so
// once Ctrl-C had been pressed *any* subsequent failure — a truncated read, a
// failed fetch — was swallowed and reported as success.
func isCleanCancellation(err, ctxErr error) bool {
	if err == nil || ctxErr == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func init() {
	addKubeFlags(rootCmd)
	addLogFlags(rootCmd)
	addOutputFlags(rootCmd)
}
