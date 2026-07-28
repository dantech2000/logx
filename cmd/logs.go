package cmd

import (
	"errors"
	"fmt"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// logOptions holds the command options for the logs command
type logOptions struct {
	container      string
	allContainers  bool
	selector       string
	allNamespaces  bool
	stats          bool
	maxConcurrency int
	follow         bool
	podName        string
	previous       bool
	timeline       bool
	fields         []string
	output         string
}

var logsCmd = &cobra.Command{
	Use:   "logs [pod-name]",
	Short: "Display logs for a Kubernetes pod",
	Long: `Display logs for a Kubernetes pod. You can filter logs by level using the --level flag.
Supported levels are TRACE, DEBUG, INFO, WARN, ERROR, and FATAL.

Provide a pod name, or use --selector to stream logs from all pods matching a
label selector.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	addLogFlags(logsCmd)
}

func addLogFlags(cmd *cobra.Command) {
	cmd.Flags().StringP(flagContainer, "c", "", "Specific container name within the pod")
	cmd.Flags().String(flagSelector, "", "Label selector (e.g. app=api); streams logs from all matching pods")
	cmd.Flags().BoolP(flagAllNamespaces, "A", false, "With --selector, match pods across all namespaces")
	cmd.Flags().BoolP(flagAllContainers, "a", false, "Stream logs from all containers in the pod, prefixed by container name")
	cmd.Flags().Int(flagMaxConcurrency, kubernetes.DefaultMaxConcurrency, "Max container log streams read at once with --all-containers/--selector")
	cmd.Flags().BoolP(flagFollow, "f", false, "Follow the log output in real-time")
	cmd.Flags().BoolP(flagPrevious, "p", false, "Get previous terminated container logs")
	cmd.Flags().Bool(flagTimeline, false, "Show pod logs and Kubernetes events together sorted by time")
	addLevelFlag(cmd)
	addFilterFlags(cmd)
	addLogQueryFlags(cmd)

	// Pod-name and container-name completion apply to every command that takes
	// these flags (both `logx logs` and the root `logx [pod]` shorthand).
	cmd.ValidArgsFunction = completePodNames
	_ = cmd.RegisterFlagCompletionFunc(flagContainer, completeContainerNames)
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

	names := filterPrefixBy(pods.Items, toComplete, func(p corev1.Pod) string { return p.Name })
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

	names := filterPrefixBy(pod.Spec.Containers, toComplete, func(c corev1.Container) string { return c.Name })
	return names, cobra.ShellCompDirectiveNoFileComp
}

func getLogOptions(cmd *cobra.Command, args []string) (*logOptions, error) {
	container, err := getStringFlag(cmd, flagContainer)
	if err != nil {
		return nil, err
	}

	allContainers, err := getBoolFlag(cmd, flagAllContainers)
	if err != nil {
		return nil, err
	}

	selector, err := getStringFlag(cmd, flagSelector)
	if err != nil {
		return nil, err
	}

	allNamespaces, err := getBoolFlag(cmd, flagAllNamespaces)
	if err != nil {
		return nil, err
	}

	stats, err := getBoolFlag(cmd, flagStats)
	if err != nil {
		return nil, err
	}

	maxConcurrency, err := getIntFlag(cmd, flagMaxConcurrency)
	if err != nil {
		return nil, err
	}

	follow, err := getBoolFlag(cmd, flagFollow)
	if err != nil {
		return nil, err
	}

	previous, err := getBoolFlag(cmd, flagPrevious)
	if err != nil {
		return nil, err
	}

	timeline, err := getBoolFlag(cmd, flagTimeline)
	if err != nil {
		return nil, err
	}

	fields, err := cmd.Flags().GetStringSlice(flagFields)
	if err != nil {
		return nil, err
	}

	output, err := getStringFlag(cmd, flagOutput)
	if err != nil {
		return nil, err
	}

	podName := ""
	if len(args) > 0 {
		podName = args[0]
	}

	return &logOptions{
		container:      container,
		allContainers:  allContainers,
		selector:       selector,
		allNamespaces:  allNamespaces,
		stats:          stats,
		maxConcurrency: maxConcurrency,
		follow:         follow,
		podName:        podName,
		previous:       previous,
		timeline:       timeline,
		fields:         fields,
		output:         output,
	}, nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	options, err := getLogOptions(cmd, args)
	if err != nil {
		return err
	}
	// Validate before any expensive work (kubeconfig load, client build) so an
	// invalid flag combination reports the usage error, not a kubeconfig error.
	if err := validateLogOptions(options); err != nil {
		return err
	}
	filterLevel, err := effectiveLogLevel(cmd)
	if err != nil {
		return err
	}

	pipelineOptions, err := buildPipelineOptions(cmd, filterLevel)
	if err != nil {
		return err
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
	logFetcher.Filters = pipelineOptions
	logFetcher.AllNamespaces = options.allNamespaces
	logFetcher.MaxConcurrency = options.maxConcurrency
	if err := applyLogQuery(cmd, logFetcher); err != nil {
		return err
	}

	switch {
	case options.selector != "":
		err = logFetcher.GetSelectedPodLogs(cmd.Context(), options.selector)
	case options.allContainers:
		err = logFetcher.GetAllContainerLogs(cmd.Context())
	case options.timeline:
		err = logFetcher.GetTimeline(cmd.Context())
	default:
		err = logFetcher.GetLogs(cmd.Context())
	}
	if err != nil {
		return fmt.Errorf("error fetching logs: %w", err)
	}

	return nil
}

// validateLogOptions rejects mutually exclusive or incomplete flag combinations.
// The checks are grouped by the flag they hang off so each group stays readable
// as new flags are added.
func validateLogOptions(o *logOptions) error {
	for _, check := range []func(*logOptions) error{
		validateTargetSelection,
		validateTimelineCombinations,
		validatePreviousCombinations,
	} {
		if err := check(o); err != nil {
			return err
		}
	}
	if o.maxConcurrency < 1 {
		return errors.New("--max-concurrency must be at least 1")
	}
	return nil
}

// validateTargetSelection checks that exactly one log target is identified.
func validateTargetSelection(o *logOptions) error {
	if o.podName == "" && o.selector == "" {
		return errors.New("provide a pod name or use --selector")
	}
	if o.selector != "" && o.podName != "" {
		return errors.New("provide either a pod name or --selector, not both")
	}
	if o.allNamespaces && o.selector == "" {
		return errors.New("--all-namespaces requires --selector")
	}
	if o.allContainers && o.container != "" {
		return errors.New("--all-containers cannot be combined with --container")
	}
	return nil
}

// validateTimelineCombinations rejects the flags --timeline cannot honor.
//
// The timeline interleaves two different record types (log lines and cluster
// events) in one fixed rendering, so the flags that replace that rendering have
// nothing to act on. Rejecting them beats the previous silent no-op, which
// exited 0 having ignored what the user asked for. Content filters
// (--grep/--exclude/--where) do apply and are handled by the timeline's log
// collector.
func validateTimelineCombinations(o *logOptions) error {
	if !o.timeline {
		return nil
	}
	if o.follow {
		return errors.New("--timeline cannot be used with --follow")
	}
	if o.allContainers {
		return errors.New("--all-containers cannot be combined with --timeline")
	}
	if o.selector != "" {
		return errors.New("--selector cannot be combined with --timeline")
	}
	if o.stats {
		return errors.New("--stats cannot be combined with --timeline")
	}
	if len(o.fields) > 0 {
		return errors.New("--fields cannot be combined with --timeline")
	}
	if o.output != "" && o.output != "text" {
		return fmt.Errorf("--output %s cannot be combined with --timeline", o.output)
	}
	return nil
}

// validatePreviousCombinations requires --previous to have a single target.
//
// -p asks for one container's prior instance, but the fan-out paths never run
// the -p precondition check, so every stream was stamped Previous:true and any
// container that had not restarted failed the whole command with a raw API error.
func validatePreviousCombinations(o *logOptions) error {
	if !o.previous {
		return nil
	}
	if o.allContainers {
		return errors.New("--previous cannot be combined with --all-containers; select one container with --container")
	}
	if o.selector != "" {
		return errors.New("--previous cannot be combined with --selector; select one pod and container")
	}
	return nil
}
