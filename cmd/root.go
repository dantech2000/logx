package cmd

import (
	"fmt"
	"os"

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
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			_ = cmd.Help()
			return
		}
		if err := runLogs(cmd, args); err != nil {
			fmt.Printf("Error running logs command: %v\n", err)
			os.Exit(1)
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	addLogFlags(rootCmd)
	rootCmd.ValidArgsFunction = completePodNames
	_ = rootCmd.RegisterFlagCompletionFunc("container", completeContainerNames)
}
