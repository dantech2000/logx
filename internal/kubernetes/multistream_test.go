package kubernetes

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
