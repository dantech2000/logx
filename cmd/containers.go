package cmd

import (
	"fmt"

	"github.com/dantech2000/logx/internal/format"
	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
)

// containerOptions holds the command options for the containers command
type containerOptions struct {
	podName      string
	outputFormat string
}

var containersCmd = &cobra.Command{
	Use:   "containers [pod-name]",
	Short: "List containers in a Kubernetes pod",
	Long: `List all containers within a specified Kubernetes pod.
This command provides a formatted output of container names for the given pod,
including the total count of containers.

Example usage:
  logx containers my-pod -n my-namespace
  logx containers my-pod -n my-namespace --output json
  logx containers my-pod -n my-namespace -o yaml
  logx containers my-pod -n my-namespace -o posix`,
	Args: cobra.ExactArgs(1),
	RunE: runContainers,
}

func init() {
	rootCmd.AddCommand(containersCmd)
	containersCmd.Flags().StringP(flagOutput, "o", "", "Output format: json, yaml, or posix")

	// Add completion for pod names
	containersCmd.ValidArgsFunction = completePodNames
}

func getContainerOptions(cmd *cobra.Command, args []string) (*containerOptions, error) {
	outputFormat, err := cmd.Flags().GetString(flagOutput)
	if err != nil {
		return nil, fmt.Errorf("error getting output format flag: %w", err)
	}

	return &containerOptions{
		podName:      args[0],
		outputFormat: outputFormat,
	}, nil
}

func runContainers(cmd *cobra.Command, args []string) error {
	clientset, namespace, err := kubernetesClientFromFlags(cmd)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	opts, err := getContainerOptions(cmd, args)
	if err != nil {
		return err
	}

	containers, err := kubernetes.ListContainers(cmd.Context(), clientset, namespace, opts.podName)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	formatter := format.NewOutputFormatter(opts.podName, namespace, containers)
	output, err := formatter.FormatOutput(opts.outputFormat)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), output)
	return err
}
