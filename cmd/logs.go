package cmd

import (
	"fmt"
	"strings"

	"github.com/dantech2000/logx/pkg/kubernetes"
	"github.com/dantech2000/logx/pkg/logging"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// logOptions holds the command options for the logs command
type logOptions struct {
	container string
	follow    bool
	level     string
	podName   string
	previous  bool
	timeline  bool
}

var logsCmd = &cobra.Command{
	Use:   "logs [pod-name]",
	Short: "Display logs for a Kubernetes pod",
	Long: `Display logs for a Kubernetes pod. You can filter logs by level using the --level flag.
Supported levels are DEBUG, INFO, WARN, and ERROR.`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	addLogFlags(logsCmd)

	// Add completion for pod names
	logsCmd.ValidArgsFunction = completePodNames
	// Add completion for container names
	_ = logsCmd.RegisterFlagCompletionFunc("container", completeContainerNames)
}

func addLogFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("container", "c", "", "Specific container name within the pod")
	cmd.Flags().BoolP("follow", "f", false, "Follow the log output in real-time")
	cmd.Flags().StringP("level", "l", "DEBUG", "Filter logs by level (DEBUG, INFO, WARN, ERROR)")
	cmd.Flags().BoolP("previous", "p", false, "Get previous terminated container logs")
	cmd.Flags().Bool("timeline", false, "Show pod logs and Kubernetes events together sorted by time")
}

// completePodNames provides dynamic completion for pod names
func completePodNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	clientset, namespace, err := kubernetesClientFromFlags(cmd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(cmd.Context(), metav1.ListOptions{
		Limit: 100,
	})
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, toComplete) {
			names = append(names, pod.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeContainerNames provides dynamic completion for container names
func completeContainerNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveError
	}

	clientset, namespace, err := kubernetesClientFromFlags(cmd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	podName := args[0]
	pod, err := clientset.CoreV1().Pods(namespace).Get(cmd.Context(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, container := range pod.Spec.Containers {
		if strings.HasPrefix(container.Name, toComplete) {
			names = append(names, container.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

func getLogOptions(cmd *cobra.Command, args []string) (*logOptions, error) {
	container, err := cmd.Flags().GetString("container")
	if err != nil {
		return nil, fmt.Errorf("error getting container flag: %w", err)
	}

	follow, err := cmd.Flags().GetBool("follow")
	if err != nil {
		return nil, fmt.Errorf("error getting follow flag: %w", err)
	}

	level, err := cmd.Flags().GetString("level")
	if err != nil {
		return nil, fmt.Errorf("error getting level flag: %w", err)
	}

	previous, err := cmd.Flags().GetBool("previous")
	if err != nil {
		return nil, fmt.Errorf("error getting previous flag: %w", err)
	}

	timeline, err := cmd.Flags().GetBool("timeline")
	if err != nil {
		return nil, fmt.Errorf("error getting timeline flag: %w", err)
	}

	return &logOptions{
		container: container,
		follow:    follow,
		level:     level,
		podName:   args[0],
		previous:  previous,
		timeline:  timeline,
	}, nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	options, err := getLogOptions(cmd, args)
	if err != nil {
		return err
	}
	filterLevel, err := logging.ParseLogLevel(options.level)
	if err != nil {
		return fmt.Errorf("invalid level %q: %w", options.level, err)
	}

	clientset, namespace, err := kubernetesClientFromFlags(cmd)
	if err != nil {
		return fmt.Errorf("error getting kubernetes client: %w", err)
	}

	// Create log fetcher with the new interface
	logFetcher := kubernetes.NewLogFetcher(
		clientset,
		namespace,
		options.podName,
		options.follow,
		options.previous,
		cmd.OutOrStdout(),
	)
	logFetcher.ContainerName = options.container
	logFetcher.FilterLevel = filterLevel

	if options.timeline && options.follow {
		return fmt.Errorf("--timeline cannot be used with --follow")
	}

	if options.timeline {
		err = logFetcher.GetTimeline(cmd.Context())
	} else {
		err = logFetcher.GetLogs(cmd.Context())
	}
	if err != nil {
		return fmt.Errorf("error fetching logs: %w", err)
	}

	return nil
}
