// Package kubernetes provides functionality for interacting with Kubernetes clusters
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/dantech2000/logx/internal/logging"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LogFetcher handles retrieving logs from Kubernetes containers
type LogFetcher struct {
	// Clientset is the Kubernetes client
	Clientset kubernetes.Interface
	// Namespace is the Kubernetes namespace
	Namespace string
	// AllNamespaces, when set, makes selector-based fetching search every
	// namespace instead of just Namespace.
	AllNamespaces bool
	// PodName is the name of the pod
	PodName string
	// ContainerName is the name of the container (optional, will prompt if not provided)
	ContainerName string
	// Follow indicates if the logs should be streamed
	Follow bool
	// Previous indicates if logs from a previous container instance should be retrieved
	Previous bool
	// FilterLevel is the minimum level that will be written
	FilterLevel logging.LogLevel
	// Filters carries the content filters (grep/exclude/highlight, field
	// predicates, projection) applied to streamed logs. Its MinLevel is set from
	// FilterLevel when the pipeline is built.
	Filters logging.PipelineOptions
	// Timestamps requests kubelet RFC3339 timestamps on each line (the pipeline
	// recognizes and renders them).
	Timestamps bool
	// SinceSeconds and SinceTime bound how far back to read (only one is set);
	// TailLines caps the number of trailing lines. Nil means unbounded.
	SinceSeconds *int64
	SinceTime    *metav1.Time
	TailLines    *int64
	// MaxConcurrency bounds how many container log streams are read at once in
	// --all-containers/--selector mode. Zero or negative means the package default.
	MaxConcurrency int
	// Writer is where the logs will be written
	Writer io.Writer
}

// NewLogFetcher creates a new LogFetcher instance
func NewLogFetcher(clientset kubernetes.Interface, namespace, podName string, follow bool, previous bool, writer io.Writer) *LogFetcher {
	return &LogFetcher{
		Clientset:   clientset,
		Namespace:   namespace,
		PodName:     podName,
		Follow:      follow,
		Previous:    previous,
		FilterLevel: logging.DEBUG,
		Writer:      writer,
	}
}

// selectContainerName resolves the container to fetch logs from on an
// already-fetched pod: the sole container's name when there is exactly one,
// otherwise an interactive prompt to pick among them.
func (lf *LogFetcher) selectContainerName(pod *corev1.Pod) (string, error) {
	containerCount := len(pod.Spec.Containers)
	switch containerCount {
	case 0:
		return "", fmt.Errorf("no containers found in pod %s", lf.PodName)
	case 1:
		return pod.Spec.Containers[0].Name, nil
	}

	// Create container info list for the prompt
	containers := make([]ContainerInfo, containerCount)
	options := make([]string, containerCount)

	for i, c := range pod.Spec.Containers {
		ready, status := GetContainerStatus(pod, c.Name)
		info := ContainerInfo{
			Name:   c.Name,
			Ready:  ready,
			Status: status,
			Image:  c.Image,
		}
		containers[i] = info
		options[i] = FormatContainerInfo(info)
	}

	// Prompting only makes sense on an interactive terminal. Without this the
	// prompt was attempted regardless, failing with a bare "selection failed:
	// EOF" in scripts and pipelines instead of saying what to do about it.
	if !stdinIsTerminal() {
		return "", fmt.Errorf("pod %q has %d containers; specify one with --container (%s)",
			lf.PodName, containerCount, strings.Join(containerNamesOf(containers), ", "))
	}

	// Prepare the survey prompt
	var selectedIdx int
	prompt := &survey.Select{
		Message: "Choose a container:",
		Options: options,
		Filter: func(filter string, value string, index int) bool {
			container := containers[index]
			filter = strings.ToLower(filter)
			return strings.Contains(strings.ToLower(container.Name), filter) ||
				strings.Contains(strings.ToLower(container.Status), filter) ||
				strings.Contains(strings.ToLower(container.Image), filter)
		},
	}

	// Show the prompt and get user's selection. The prompt is driven on stderr,
	// not stdout: survey defaults to os.Stdout, which meant the ANSI prompt was
	// written into redirected output, so `logx logs pod -o json > out.json` put
	// escape sequences and the menu into out.json.
	askOpts := []survey.AskOpt{
		survey.WithPageSize(10),
		survey.WithStdio(os.Stdin, os.Stderr, os.Stderr),
	}
	if err := survey.AskOne(prompt, &selectedIdx, askOpts...); err != nil {
		if errors.Is(err, terminal.InterruptErr) {
			return "", errors.New("operation cancelled")
		}
		return "", fmt.Errorf("selection failed: %w", err)
	}

	return containers[selectedIdx].Name, nil
}

// stdinIsTerminal reports whether stdin is an interactive terminal rather than a
// pipe, file, or /dev/null, so the container picker only prompts when someone is
// actually there to answer.
//
// This uses a real terminal check rather than testing os.ModeCharDevice:
// /dev/null is a character device too, so the mode test reports a terminal for
// the very redirection this is meant to detect.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// containerNamesOf extracts the names from container info, for error messages.
func containerNamesOf(containers []ContainerInfo) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name
	}
	return names
}

// previousContainerTerminated reports, from an already-fetched pod, whether the
// named container has restarted — i.e. whether --previous has logs to fetch.
// It scans init and ephemeral container statuses as well as regular ones, so the
// set of containers it recognizes matches podHasContainer and the container
// picker. Checking only regular containers meant `-c init-db -p` was accepted as
// a valid container and then rejected as nonexistent — while reading a
// crashlooping init container's prior logs is exactly what -p is for.
func (lf *LogFetcher) previousContainerTerminated(pod *corev1.Pod, containerName string) (bool, error) {
	for _, status := range allContainerStatuses(pod) {
		if status.Name == containerName {
			return status.RestartCount > 0, nil
		}
	}
	return false, fmt.Errorf("container %q not found in pod %q", containerName, lf.PodName)
}

// GetLogs retrieves logs from the specified container.
// If no container is specified, it will prompt the user to select one.
// It handles both current and previous container instances based on the Previous flag.
func (lf *LogFetcher) GetLogs(ctx context.Context) error {
	if _, err := lf.prepareLogRequest(ctx); err != nil {
		return err
	}

	pipeline := lf.newPipeline()
	// The digest is written even when the stream failed, mirroring
	// fanInStreams: --stats over a container that died mid-fetch still reports
	// everything read before the failure instead of printing nothing.
	streamErr := lf.streamLogs(ctx, lf.Namespace, lf.PodName, lf.ContainerName, pipeline, func(line string) error {
		if _, err := fmt.Fprintln(lf.Writer, line); err != nil {
			return fmt.Errorf("error writing log line: %w", err)
		}
		return nil
	})

	if stats := pipeline.Stats(); stats != nil {
		if werr := stats.Write(lf.Writer); werr != nil && streamErr == nil {
			return werr
		}
	}
	return streamErr
}

// streamLogs opens the log stream for one container and drives pipeline over it
// line by line, calling emit for each rendered line. Shared by the single-stream
// (GetLogs) and prefixed multi-stream (streamPrefixed) paths, which differ only
// in how an emitted line is written.
func (lf *LogFetcher) streamLogs(ctx context.Context, namespace, pod, container string, pipeline *logging.Pipeline, emit func(string) error) error {
	opts := lf.podLogOptions(container)
	req := lf.Clientset.CoreV1().Pods(namespace).GetLogs(pod, &opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("error opening log stream for %s/%s: %w", pod, container, err)
	}
	defer func() { _ = stream.Close() }()

	scanner := logging.NewLineReader(stream)
	for scanner.Scan() {
		out, ok := pipeline.ProcessLine(scanner.Text())
		if !ok {
			continue
		}
		if err := emit(out); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log stream for %s/%s: %w", pod, container, err)
	}
	return nil
}

// fetchPod fetches a pod with the shared "error fetching pod details" wrapping
// used by every pod lookup in this package.
func fetchPod(ctx context.Context, clientset kubernetes.Interface, namespace, name string) (*corev1.Pod, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod details: %w", err)
	}
	return pod, nil
}

// getPod fetches the fetcher's own pod.
func (lf *LogFetcher) getPod(ctx context.Context) (*corev1.Pod, error) {
	return fetchPod(ctx, lf.Clientset, lf.Namespace, lf.PodName)
}

// podLogOptions builds the PodLogOptions for a container from the fetcher's
// settings. Shared by single- and multi-stream paths so the log window is
// defined in one place.
func (lf *LogFetcher) podLogOptions(container string) corev1.PodLogOptions {
	return corev1.PodLogOptions{
		Container:    container,
		Follow:       lf.Follow,
		Previous:     lf.Previous,
		Timestamps:   lf.Timestamps,
		SinceSeconds: lf.SinceSeconds,
		SinceTime:    lf.SinceTime,
		TailLines:    lf.TailLines,
	}
}

// newPipeline builds the shared logging pipeline from the fetcher's filters,
// with the level taken authoritatively from FilterLevel.
func (lf *LogFetcher) newPipeline() *logging.Pipeline {
	return lf.newStreamPipeline(nil)
}

// newStreamPipeline builds a per-stream pipeline. When shared is non-nil the
// pipeline records into that shared (thread-safe) Stats so --stats aggregates
// across every concurrent stream; otherwise it behaves like newPipeline.
func (lf *LogFetcher) newStreamPipeline(shared *logging.Stats) *logging.Pipeline {
	opts := lf.Filters
	opts.MinLevel = lf.FilterLevel
	if shared != nil {
		return logging.NewPipelineWithStats(opts, shared)
	}
	return logging.NewPipeline(opts)
}
