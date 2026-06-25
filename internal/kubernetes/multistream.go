package kubernetes

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dantech2000/logx/internal/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetAllContainerLogs streams logs from every container in the pod (regular,
// init, and ephemeral) concurrently, prefixing each line with a color-coded
// container name and merging them onto the fetcher's writer. Writes are
// serialized so lines never interleave mid-line. It returns the first stream
// error, if any, after all streams finish.
func (lf *LogFetcher) GetAllContainerLogs(ctx context.Context) error {
	pod, err := lf.Clientset.CoreV1().Pods(lf.Namespace).Get(ctx, lf.PodName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error fetching pod details: %w", err)
	}
	names := podContainerNames(pod)
	if len(names) == 0 {
		return fmt.Errorf("no containers found in pod %s", lf.PodName)
	}

	streams := make([]prefixedStream, len(names))
	for i, name := range names {
		streams[i] = prefixedStream{
			namespace: lf.Namespace,
			pod:       lf.PodName,
			container: name,
			prefix:    logging.ColorizePrefix(name, i) + " ",
		}
	}
	return lf.fanInStreams(ctx, streams)
}

// GetSelectedPodLogs streams logs from every pod matching the label selector,
// across each pod's containers, merging them onto the writer with a color-coded
// prefix. The prefix is "pod", "pod/container", "ns/pod", or "ns/pod/container"
// depending on whether multiple containers and AllNamespaces are in play. Streams
// from the same pod share a color.
func (lf *LogFetcher) GetSelectedPodLogs(ctx context.Context, selector string) error {
	listNamespace := lf.Namespace
	if lf.AllNamespaces {
		listNamespace = metav1.NamespaceAll
	}

	pods, err := lf.Clientset.CoreV1().Pods(listNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("error listing pods for selector %q: %w", selector, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods match selector %q in %s", selector, namespaceScope(lf.AllNamespaces, lf.Namespace))
	}

	var streams []prefixedStream
	for podIdx := range pods.Items {
		pod := &pods.Items[podIdx]
		containers := lf.containersToStream(pod)
		for _, c := range containers {
			streams = append(streams, prefixedStream{
				namespace: pod.Namespace,
				pod:       pod.Name,
				container: c,
				prefix:    logging.ColorizePrefix(lf.streamLabel(pod, c, len(containers)), podIdx) + " ",
			})
		}
	}
	if len(streams) == 0 {
		return fmt.Errorf("no matching containers to stream for selector %q", selector)
	}
	return lf.fanInStreams(ctx, streams)
}

// streamLabel builds the human-facing prefix label for a stream, including the
// namespace only when reading across namespaces and the container only when the
// pod contributes more than one.
func (lf *LogFetcher) streamLabel(pod *corev1.Pod, container string, containerCount int) string {
	parts := make([]string, 0, 3)
	if lf.AllNamespaces {
		parts = append(parts, pod.Namespace)
	}
	parts = append(parts, pod.Name)
	if containerCount > 1 {
		parts = append(parts, container)
	}
	return strings.Join(parts, "/")
}

// namespaceScope describes the search scope for error messages.
func namespaceScope(allNamespaces bool, namespace string) string {
	if allNamespaces {
		return "any namespace"
	}
	return fmt.Sprintf("namespace %q", namespace)
}

// containersToStream picks which of a pod's containers to read: just the named
// one when --container is set (skipping pods that lack it), otherwise all of them.
func (lf *LogFetcher) containersToStream(pod *corev1.Pod) []string {
	if lf.ContainerName != "" {
		if podHasContainer(pod, lf.ContainerName) {
			return []string{lf.ContainerName}
		}
		return nil
	}
	return podContainerNames(pod)
}

// prefixedStream describes one container log stream (in a named pod and
// namespace) and the prefix to tag its lines with in the merged output.
type prefixedStream struct {
	namespace string
	pod       string
	container string
	prefix    string
}

// fanInStreams runs each stream concurrently, serializing writes to the shared
// writer, and returns the first error encountered.
func (lf *LogFetcher) fanInStreams(ctx context.Context, streams []prefixedStream) error {
	var (
		mu       sync.Mutex // serializes writes to lf.Writer
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	for _, s := range streams {
		wg.Add(1)
		go func(s prefixedStream) {
			defer wg.Done()
			if err := lf.streamPrefixed(ctx, s, &mu); err != nil {
				errOnce.Do(func() { firstErr = err })
			}
		}(s)
	}
	wg.Wait()
	return firstErr
}

// streamPrefixed reads one container's logs through a private pipeline and writes
// each emitted line, prefixed, to w under mu. Each stream needs its own pipeline
// because the pipeline is stateful (multi-line grouping).
func (lf *LogFetcher) streamPrefixed(ctx context.Context, s prefixedStream, mu *sync.Mutex) error {
	opts := lf.podLogOptions(s.container)
	req := lf.Clientset.CoreV1().Pods(s.namespace).GetLogs(s.pod, &opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("error opening log stream for %s/%s: %w", s.pod, s.container, err)
	}
	defer func() { _ = stream.Close() }()

	pipeline := lf.newPipeline()
	scanner := logging.NewLineReader(stream)
	for scanner.Scan() {
		out, ok := pipeline.ProcessLine(scanner.Text())
		if !ok {
			continue
		}
		if err := writeLine(mu, lf.Writer, s.prefix, out); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// writeLine writes "prefix + line + newline" as a single guarded write so
// concurrent streams never interleave within a line.
func writeLine(mu *sync.Mutex, w io.Writer, prefix, line string) error {
	mu.Lock()
	defer mu.Unlock()
	_, err := fmt.Fprintf(w, "%s%s\n", prefix, line)
	return err
}
