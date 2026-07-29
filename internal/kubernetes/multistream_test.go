package kubernetes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dantech2000/logx/internal/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestLogFetcher_GetAllContainerLogs(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(clientgotesting.GenericAction)
		if !ok {
			t.Fatalf("expected GenericAction, got %T", action)
		}
		opts, ok := ga.GetValue().(*corev1.PodLogOptions)
		if !ok {
			t.Fatalf("expected *corev1.PodLogOptions, got %T", ga.GetValue())
		}
		return true, &runtime.Unknown{Raw: []byte("INFO line from " + opts.Container)}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers:     []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
			InitContainers: []corev1.Container{{Name: "init-db"}},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	if err := fetcher.GetAllContainerLogs(context.Background()); err != nil {
		t.Fatalf("GetAllContainerLogs error: %v", err)
	}

	out := buf.String()
	for _, name := range []string{"app", "sidecar", "init-db"} {
		if !strings.Contains(out, "line from "+name) {
			t.Fatalf("missing logs for container %q:\n%s", name, out)
		}
	}
	if n := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; n != 3 {
		t.Fatalf("expected 3 merged lines, got %d:\n%s", n, out)
	}
}

func TestLogFetcher_GetAllContainerLogsNoContainers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"}}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "empty", false, false, &buf)
	if err := fetcher.GetAllContainerLogs(context.Background()); err == nil {
		t.Fatal("expected error for a pod with no containers")
	}
}

func TestLogFetcher_GetSelectedPodLogs(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga := action.(clientgotesting.GenericAction)
		opts := ga.GetValue().(*corev1.PodLogOptions)
		// The pod name isn't on PodLogOptions; encode container so we can assert
		// that every matched pod/container was streamed.
		return true, &runtime.Unknown{Raw: []byte("INFO from container " + opts.Container)}, nil
	})

	mkPod := func(name, app string, containers ...string) *corev1.Pod {
		cs := make([]corev1.Container, len(containers))
		for i, c := range containers {
			cs[i] = corev1.Container{Name: c}
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: map[string]string{"app": app}},
			Spec:       corev1.PodSpec{Containers: cs},
		}
	}
	for _, p := range []*corev1.Pod{
		mkPod("api-1", "api", "web"),
		mkPod("api-2", "api", "web", "sidecar"),
		mkPod("worker-1", "worker", "job"),
	} {
		if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pod %s: %v", p.Name, err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	if err := fetcher.GetSelectedPodLogs(context.Background(), "app=api"); err != nil {
		t.Fatalf("GetSelectedPodLogs error: %v", err)
	}

	out := buf.String()
	// api-1/web, api-2/web, api-2/sidecar => 3 streams; worker excluded by selector.
	if strings.Contains(out, "container job") {
		t.Fatalf("selector leaked a non-matching pod's logs:\n%s", out)
	}
	if got := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; got != 3 {
		t.Fatalf("expected 3 merged streams, got %d:\n%s", got, out)
	}
	// api-2 has multiple containers, so its lines are prefixed pod/container.
	if !strings.Contains(out, "api-2/sidecar") || !strings.Contains(out, "api-2/web") {
		t.Fatalf("multi-container pod not prefixed pod/container:\n%s", out)
	}
	// api-1 has a single container, so it is prefixed by pod name only.
	if !strings.Contains(out, "api-1 ") {
		t.Fatalf("single-container pod not prefixed by pod name:\n%s", out)
	}
}

func TestLogFetcher_GetSelectedPodLogsNoMatch(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "", false, false, &buf)
	if err := fetcher.GetSelectedPodLogs(context.Background(), "app=missing"); err == nil {
		t.Fatal("expected error when no pods match the selector")
	}
}

func TestLogFetcher_GetSelectedPodLogsAllNamespaces(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga := action.(clientgotesting.GenericAction)
		opts := ga.GetValue().(*corev1.PodLogOptions)
		return true, &runtime.Unknown{Raw: []byte("INFO from " + opts.Container)}, nil
	})

	mk := func(ns, name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": "api"}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web"}}},
		}
	}
	for _, p := range []*corev1.Pod{mk("team-a", "api-1"), mk("team-b", "api-2")} {
		if _, err := clientset.CoreV1().Pods(p.Namespace).Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pod: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	fetcher.AllNamespaces = true
	if err := fetcher.GetSelectedPodLogs(context.Background(), "app=api"); err != nil {
		t.Fatalf("GetSelectedPodLogs error: %v", err)
	}

	out := buf.String()
	// Prefix must be namespace-qualified across namespaces.
	if !strings.Contains(out, "team-a/api-1") || !strings.Contains(out, "team-b/api-2") {
		t.Fatalf("expected namespace-qualified prefixes, got:\n%s", out)
	}
	if got := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; got != 2 {
		t.Fatalf("expected 2 streams across namespaces, got %d:\n%s", got, out)
	}
}

// TestLogFetcher_StatsWrittenWhenAStreamFails pins that a partial failure still
// produces a digest. fanInStreams deliberately lets a failing stream run
// alongside its siblings rather than cancelling them, so the aggregate over the
// streams that *did* succeed is exactly what --stats is for. Returning early on
// the first error would throw that away — with a wide --selector, one pod stuck
// in ContainerCreating would blank the digest for every healthy pod.
func TestLogFetcher_StatsWrittenWhenAStreamFails(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(clientgotesting.GenericAction)
		if !ok {
			t.Fatalf("expected GenericAction, got %T", action)
		}
		opts, ok := ga.GetValue().(*corev1.PodLogOptions)
		if !ok {
			t.Fatalf("expected *corev1.PodLogOptions, got %T", ga.GetValue())
		}
		if opts.Container == "broken" {
			return true, nil, errors.New("container is waiting to start: ContainerCreating")
		}
		return true, &runtime.Unknown{Raw: []byte("ERROR boom from " + opts.Container)}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "healthy"}, {Name: "broken"}}},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.FilterLevel = logging.DEBUG
	fetcher.Filters = logging.PipelineOptions{MinLevel: logging.DEBUG, CollectStats: true}

	// The failing stream is still reported.
	if err := fetcher.GetAllContainerLogs(context.Background()); err == nil {
		t.Fatal("expected the broken stream to surface an error")
	}

	out := buf.String()
	if !strings.Contains(out, "logx stats") {
		t.Fatalf("stats digest was dropped because a sibling stream failed:\n%q", out)
	}
	if !strings.Contains(out, "ERROR 1") {
		t.Fatalf("digest should still count the healthy stream's line:\n%q", out)
	}
}

// followBlockingWriter blocks every write until released, standing in for the
// --follow case where a stream goroutine never returns because scanner.Scan()
// waits forever on the next line.
type followBlockingWriter struct {
	release chan struct{}
	hit     chan struct{}
	once    atomic.Bool
}

func (w *followBlockingWriter) Write(p []byte) (int, error) {
	if w.once.CompareAndSwap(false, true) {
		close(w.hit)
	}
	<-w.release
	return len(p), nil
}

// TestLogFetcher_FollowOpensEveryStream pins that --follow opens every stream
// regardless of --max-concurrency.
//
// errgroup's SetLimit blocks the dispatch loop until a worker slot frees, and a
// followed stream never returns — so a pool smaller than the stream count does
// not throttle, it starves. Streams past the limit were never opened at all, for
// the life of the command: `logx logs -f --selector app=api` against a
// 20-replica Deployment tailed 10 pods and silently ignored the other 10, with no
// error and no diagnostic. validateLogOptions explicitly permits that
// combination, so it was fully reachable.
func TestLogFetcher_FollowOpensEveryStream(t *testing.T) {
	const containers, limit = 6, 2

	var opened atomic.Int32
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(clientgotesting.Action) (bool, runtime.Object, error) {
		opened.Add(1)
		return true, &runtime.Unknown{Raw: []byte("INFO hello\n")}, nil
	})

	list := make([]corev1.Container, containers)
	for i := range list {
		list[i] = corev1.Container{Name: fmt.Sprintf("c%d", i)}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: list},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	w := &followBlockingWriter{release: make(chan struct{}), hit: make(chan struct{})}
	lf := NewLogFetcher(clientset, "default", "p", true /* follow */, false, w)
	lf.FilterLevel = logging.DEBUG
	lf.MaxConcurrency = limit

	done := make(chan error, 1)
	go func() { done <- lf.GetAllContainerLogs(context.Background()) }()

	select {
	case <-w.hit:
	case <-time.After(10 * time.Second):
		t.Fatal("no stream produced output")
	}
	// Give the pool every opportunity to dispatch the remaining streams.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && opened.Load() < containers {
		time.Sleep(20 * time.Millisecond)
	}

	if got := opened.Load(); got != containers {
		t.Fatalf("--follow with --max-concurrency=%d opened %d/%d streams; the rest are starved forever",
			limit, got, containers)
	}

	close(w.release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fanInStreams never returned")
	}
}

// TestEffectiveMaxConcurrencyStillCapsWithoutFollow pins that the cap keeps
// doing its job for a finite fetch, where streams do return and bounding the
// burst of simultaneous API requests is exactly right.
func TestEffectiveMaxConcurrencyStillCapsWithoutFollow(t *testing.T) {
	lf := &LogFetcher{MaxConcurrency: 3}
	if got := lf.effectiveMaxConcurrency(10); got != 3 {
		t.Errorf("non-follow cap = %d, want 3", got)
	}
	lf.Follow = true
	if got := lf.effectiveMaxConcurrency(10); got != 10 {
		t.Errorf("follow must open every stream, got %d, want 10", got)
	}
}
