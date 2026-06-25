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
