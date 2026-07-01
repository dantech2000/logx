// Package kubernetes provides functionality for interacting with Kubernetes clusters
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/dantech2000/logx/internal/logging"
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

// getSingleContainerName returns the name of the container to fetch logs from.
// If there's only one container, it returns that container's name.
// If there are multiple containers, it prompts the user to select one.
func (lf *LogFetcher) getSingleContainerName(ctx context.Context) (string, error) {
	pod, err := lf.Clientset.CoreV1().Pods(lf.Namespace).Get(ctx, lf.PodName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error fetching pod details: %w", err)
	}
	return lf.selectContainerName(pod)
}

// selectContainerName is the pure counterpart of getSingleContainerName: it
// operates on an already-fetched pod so prepareLogRequest can reuse a single
// Get call instead of fetching the pod again just to resolve the container name.
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

	// Show the prompt and get user's selection
	if err := survey.AskOne(prompt, &selectedIdx, survey.WithPageSize(10)); err != nil {
		if errors.Is(err, terminal.InterruptErr) {
			return "", errors.New("operation cancelled")
		}
		return "", fmt.Errorf("selection failed: %w", err)
	}

	return containers[selectedIdx].Name, nil
}

// hasPreviousContainer checks if a container has previous terminated instances
func (lf *LogFetcher) hasPreviousContainer(ctx context.Context, containerName string) (bool, error) {
	pod, err := lf.Clientset.CoreV1().Pods(lf.Namespace).Get(ctx, lf.PodName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("error fetching pod details: %w", err)
	}
	return lf.previousContainerTerminated(pod, containerName)
}

// previousContainerTerminated is the pure counterpart of hasPreviousContainer:
// it operates on an already-fetched pod so prepareLogRequest can reuse a single
// Get call instead of fetching the pod again just to check restart history.
func (lf *LogFetcher) previousContainerTerminated(pod *corev1.Pod, containerName string) (bool, error) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.RestartCount > 0, nil
		}
	}
	return false, fmt.Errorf("container %q not found in pod %q", containerName, lf.PodName)
}

// LogWriter is an io.Writer that feeds each written line through a shared
// logging.Pipeline and writes the rendered result. It expects exactly one log
// line per Write call (with no embedded newline), which is how GetLogs drives it
// from the line reader; this per-line contract is also what lets later features
// (multi-container / multi-pod) wrap one LogWriter per stream and merge them.
type LogWriter struct {
	writer   io.Writer
	pipeline *logging.Pipeline
}

// Write implements io.Writer. p must be a single log line.
func (w *LogWriter) Write(p []byte) (n int, err error) {
	out, ok := w.pipeline.ProcessLine(string(p))
	if !ok {
		return len(p), nil
	}
	if _, err := fmt.Fprintln(w.writer, out); err != nil {
		return len(p), err
	}
	return len(p), nil
}

// NewLogWriter creates a LogWriter that emits every entry at or above DEBUG.
func NewLogWriter(w io.Writer) *LogWriter {
	return NewLogWriterWithPipeline(w, logging.NewPipeline(logging.PipelineOptions{MinLevel: logging.DEBUG}))
}

// NewLogWriterWithPipeline creates a LogWriter backed by a caller-configured
// Pipeline, so the filter level (and later, richer filters) is set in one place.
func NewLogWriterWithPipeline(w io.Writer, pipeline *logging.Pipeline) *LogWriter {
	return &LogWriter{writer: w, pipeline: pipeline}
}

// GetLogs retrieves logs from the specified container.
// If no container is specified, it will prompt the user to select one.
// It handles both current and previous container instances based on the Previous flag.
func (lf *LogFetcher) GetLogs(ctx context.Context) error {
	if _, err := lf.prepareLogRequest(ctx); err != nil {
		return err
	}

	pipeline := lf.newPipeline()
	err := lf.streamLogs(ctx, lf.Namespace, lf.PodName, lf.ContainerName, pipeline, func(line string) error {
		if _, err := fmt.Fprintln(lf.Writer, line); err != nil {
			return fmt.Errorf("error writing log line: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if stats := pipeline.Stats(); stats != nil {
		return stats.Write(lf.Writer)
	}

	return nil
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
