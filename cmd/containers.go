package cmd

import (
	"fmt"
	"os"

	"github.com/dantech2000/logx/pkg/format"
	"github.com/dantech2000/logx/pkg/kubernetes"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// containerOptions holds the command options for the containers command
type containerOptions struct {
	namespace    string
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
	Run:  runContainers,
}

func init() {
	rootCmd.AddCommand(containersCmd)
	containersCmd.Flags().StringP("namespace", "n", "", "Kubernetes namespace (defaults to current context's namespace)")
	containersCmd.Flags().StringP("output", "o", "", "Output format: json, yaml, or posix")

	// Add completion for pod names
	containersCmd.ValidArgsFunction = completePodNames
}

func getContainerOptions(cmd *cobra.Command, args []string, contextNamespace string) (*containerOptions, error) {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return nil, fmt.Errorf("error getting namespace flag: %v", err)
	}

	// Use explicitly provided namespace if set, otherwise use context namespace
	if namespace == "" {
		namespace = contextNamespace
	}

	outputFormat, err := cmd.Flags().GetString("output")
	if err != nil {
		return nil, fmt.Errorf("error getting output format flag: %v", err)
	}

	return &containerOptions{
		namespace:    namespace,
		podName:      args[0],
		outputFormat: outputFormat,
	}, nil
}

func runContainers(cmd *cobra.Command, args []string) {
	clientset, contextNamespace, err := kubernetes.GetKubernetesClient()
	if err != nil {
		color.Red("Error creating Kubernetes client: %v", err)
		os.Exit(1)
	}

	opts, err := getContainerOptions(cmd, args, contextNamespace)
	if err != nil {
		color.Red("Error getting command options: %v", err)
		os.Exit(1)
	}

	containers, err := kubernetes.ListContainers(clientset, opts.namespace, opts.podName)
	if err != nil {
		color.Red("Error listing containers: %v", err)
		os.Exit(1)
	}

	formatter := format.NewOutputFormatter(opts.podName, opts.namespace, containers)
	output, err := formatter.FormatOutput(opts.outputFormat)
	if err != nil {
		color.Red("Error formatting output: %v", err)
		os.Exit(1)
	}

	fmt.Println(output)
}
