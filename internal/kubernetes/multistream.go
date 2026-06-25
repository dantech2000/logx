package kubernetes

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/dantech2000/logx/internal/logging"
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
			container: name,
			prefix:    logging.ColorizePrefix(name, i) + " ",
		}
	}
	return lf.fanInStreams(ctx, streams)
}

// prefixedStream describes one container log stream and the prefix to tag its
// lines with in the merged output.
type prefixedStream struct {
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
	req := lf.Clientset.CoreV1().Pods(lf.Namespace).GetLogs(lf.PodName, &opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("error opening log stream for container %q: %w", s.container, err)
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
