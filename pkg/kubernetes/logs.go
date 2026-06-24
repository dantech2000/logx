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
	"github.com/dantech2000/logx/pkg/logging"
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
	err = survey.AskOne(prompt, &selectedIdx, survey.WithPageSize(10))
	if err != nil {
		if errors.Is(err, terminal.InterruptErr) {
			return "", fmt.Errorf("operation cancelled")
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

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.RestartCount > 0, nil
		}
	}
	return false, fmt.Errorf("container '%s' not found in pod '%s'", containerName, lf.PodName)
}

// LogWriter wraps an io.Writer to process logs before writing
type LogWriter struct {
	writer      io.Writer
	filterLevel logging.LogLevel
}

// Write implements io.Writer interface
func (w *LogWriter) Write(p []byte) (n int, err error) {
	// Convert bytes to string and parse log
	logLine := strings.TrimSpace(string(p))
	if logLine == "" {
		return len(p), nil
	}

	entry := logging.ParseLogEntry(logLine)
	if entry.Level < w.filterLevel {
		return len(p), nil
	}
	parsedLog := logging.FormatLogEntry(entry)
	// Write the parsed log with a newline
	_, err = fmt.Fprintln(w.writer, parsedLog)
	return len(p), err
}

// NewLogWriter creates a new LogWriter
func NewLogWriter(w io.Writer) *LogWriter {
	return &LogWriter{writer: w, filterLevel: logging.DEBUG}
}

// GetLogs retrieves logs from the specified container.
// If no container is specified, it will prompt the user to select one.
// It handles both current and previous container instances based on the Previous flag.
func (lf *LogFetcher) GetLogs(ctx context.Context) error {
	if err := lf.prepareLogRequest(ctx); err != nil {
		return err
	}

	// Now proceed with log fetching
	podLogOpts := corev1.PodLogOptions{
		Container: lf.ContainerName,
		Follow:    lf.Follow,
		Previous:  lf.Previous,
	}

	req := lf.Clientset.CoreV1().Pods(lf.Namespace).GetLogs(lf.PodName, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() { _ = podLogs.Close() }()

	// Create a scanner to read logs line by line
	scanner := logging.NewLineScanner(podLogs)
	logWriter := NewLogWriter(lf.Writer)
	logWriter.filterLevel = lf.FilterLevel

	// Process each log line
	for scanner.Scan() {
		logLine := scanner.Text()
		if _, err := logWriter.Write([]byte(logLine)); err != nil {
			return fmt.Errorf("error writing log line: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}
