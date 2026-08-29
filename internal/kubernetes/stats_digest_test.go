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

// TestGetLogsWritesStatsDigestOnStreamError pins that a single-stream --stats
// run still writes its digest when the log stream fails. GetLogs used to return
// the stream error before writing, so `logx logs pod --stats` on a container
// that died mid-fetch printed nothing — while the multi-stream path
// (fanInStreams) deliberately writes the aggregate over what succeeded.
func TestGetLogsWritesStatsDigestOnStreamError(t *testing.T) {
	logging.ApplyColorMode(logging.ColorNever)
	t.Cleanup(func() { logging.ApplyColorMode(logging.ColorAuto) })

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded // simulate a failed fetch
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
	fetcher.Filters = logging.PipelineOptions{CollectStats: true}

	if err := fetcher.GetLogs(context.Background()); err == nil {
		t.Fatal("GetLogs should report the stream error")
	}
	if out := buf.String(); !strings.Contains(out, "logx stats") {
		t.Fatalf("stats digest missing on stream failure, got:\n%s", out)
	}
}

// TestGetLogsWritesStatsDigestAfterSuccess pins the happy path: with --stats and
// a successful stream the digest is written exactly once.
func TestGetLogsWritesStatsDigestAfterSuccess(t *testing.T) {
	logging.ApplyColorMode(logging.ColorNever)
	t.Cleanup(func() { logging.ApplyColorMode(logging.ColorAuto) })

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("INFO hello\nERROR boom")}, nil
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
	fetcher.Filters = logging.PipelineOptions{CollectStats: true}

	if err := fetcher.GetLogs(context.Background()); err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "logx stats") != 1 {
		t.Fatalf("expected exactly one digest, got:\n%s", out)
	}
	if !strings.Contains(out, "lines: 2") {
		t.Fatalf("digest did not count both lines:\n%s", out)
	}
}
