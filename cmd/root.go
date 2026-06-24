package cmd

import (
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runLogs(cmd, args)
	},
}

// Execute runs the root command and returns any error to the caller (main),
// which is responsible for reporting it and setting the exit code.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	addKubeFlags(rootCmd)
	addLogFlags(rootCmd)
	rootCmd.ValidArgsFunction = completePodNames
	_ = rootCmd.RegisterFlagCompletionFunc("container", completeContainerNames)
}
