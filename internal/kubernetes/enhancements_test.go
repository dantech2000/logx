package kubernetes

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dantech2000/logx/internal/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

// TestFanInStreamsAggregatesStats verifies that --stats across a multi-container
// fan-out records every stream into a single shared accumulator and writes one
// aggregated digest (per-line output suppressed).
func TestFanInStreamsAggregatesStats(t *testing.T) {
	logging.ApplyColorMode(logging.ColorNever)
	t.Cleanup(func() { logging.ApplyColorMode(logging.ColorAuto) })

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		// Two lines per container (one INFO, one ERROR) so totals must aggregate.
		ga := action.(clientgotesting.GenericAction)
		opts := ga.GetValue().(*corev1.PodLogOptions)
		body := "INFO serving request from " + opts.Container + "\nERROR upstream timeout from " + opts.Container
		return true, &runtime.Unknown{Raw: []byte(body)}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}, {Name: "proxy"}},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	fetcher.Filters = logging.PipelineOptions{CollectStats: true}
	if err := fetcher.GetAllContainerLogs(context.Background()); err != nil {
		t.Fatalf("GetAllContainerLogs error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "logx stats") {
		t.Fatalf("expected a stats digest, got:\n%s", out)
	}
	// 3 containers × 2 lines each, aggregated into one digest.
	if !strings.Contains(out, "lines: 6") {
		t.Fatalf("stats did not aggregate across streams (want lines: 6):\n%s", out)
	}
	if !strings.Contains(out, "ERROR 3") || !strings.Contains(out, "INFO 3") {
		t.Fatalf("level counts not aggregated across streams:\n%s", out)
	}
	// In stats mode the per-line output is suppressed; only the digest is written,
	// so no container-prefixed log line should appear.
	if strings.Contains(out, "[LEVEL]") || strings.Contains(out, "app INFO") {
		t.Fatalf("per-line output leaked in stats mode:\n%s", out)
	}
}

// TestFanInStreamsBoundedPoolStreamsAll verifies that with a small worker pool
// (fewer workers than streams) every stream is still drained — the pool processes
// all jobs, not just the first --max-concurrency of them. It also counts that the
// pool never opens more than the configured number of streams concurrently.
//
// The client-go fake serializes its reactor chain under a global lock, so the
// blocking part of a real stream (the network read) cannot overlap here; the hard
// guarantee that no more than N stream goroutines run at once is exercised by
// TestEffectiveMaxConcurrency, which pins the worker count the pool uses.
func TestFanInStreamsBoundedPoolStreamsAll(t *testing.T) {
	const limit = 2
	const containers = 6

	var opened int32
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&opened, 1)
		ga := action.(clientgotesting.GenericAction)
		opts := ga.GetValue().(*corev1.PodLogOptions)
		return true, &runtime.Unknown{Raw: []byte("INFO line from " + opts.Container)}, nil
	})

	cs := make([]corev1.Container, containers)
	for i := range cs {
		cs[i] = corev1.Container{Name: "c" + string(rune('0'+i))}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: cs},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	fetcher.MaxConcurrency = limit
	if err := fetcher.GetAllContainerLogs(context.Background()); err != nil {
		t.Fatalf("GetAllContainerLogs error: %v", err)
	}

	if got := atomic.LoadInt32(&opened); int(got) != containers {
		t.Fatalf("opened %d streams, want all %d drained through the pool", got, containers)
	}
	if got := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n") + 1; got != containers {
		t.Fatalf("expected %d merged lines, got %d:\n%s", containers, got, buf.String())
	}
}

func TestEffectiveMaxConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		configured  int
		streamCount int
		want        int
	}{
		{"default capped by streams", 0, 5, 5},
		{"default applies", 0, 20, defaultMaxConcurrency},
		{"explicit below streams", 3, 20, 3},
		{"explicit above streams clamps", 3, 2, 2},
		{"negative falls back to default", -1, 5, 5},
		{"one", 1, 1, 1},
		{"zero streams floors at one", 5, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := &LogFetcher{MaxConcurrency: tt.configured}
			if got := lf.effectiveMaxConcurrency(tt.streamCount); got != tt.want {
				t.Fatalf("effectiveMaxConcurrency(%d) with MaxConcurrency=%d = %d, want %d",
					tt.streamCount, tt.configured, got, tt.want)
			}
		})
	}
}

// TestGetTimelineAppliesSinceAndTail verifies that --since/--tail bound the log
// portion of the timeline request.
func TestGetTimelineAppliesSinceAndTail(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	var gotOptions *corev1.PodLogOptions
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga := action.(clientgotesting.GenericAction)
		gotOptions = ga.GetValue().(*corev1.PodLogOptions)
		return true, &runtime.Unknown{Raw: []byte("2026-05-15T00:38:04Z INFO hello")}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.DEBUG
	tail := int64(40)
	fetcher.TailLines = &tail
	since := int64(900)
	fetcher.SinceSeconds = &since

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline error: %v", err)
	}
	if gotOptions == nil {
		t.Fatal("timeline did not request pod logs")
	}
	if gotOptions.TailLines == nil || *gotOptions.TailLines != 40 {
		t.Fatalf("timeline TailLines = %v, want 40", gotOptions.TailLines)
	}
	if gotOptions.SinceSeconds == nil || *gotOptions.SinceSeconds != 900 {
		t.Fatalf("timeline SinceSeconds = %v, want 900", gotOptions.SinceSeconds)
	}
	if !gotOptions.Timestamps {
		t.Fatal("timeline must keep Timestamps on for sorting")
	}
}
