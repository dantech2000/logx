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

// tailFetcher serves logsBody on pods/log and captures the request options,
// with a single-container pod so no picker interferes.
func tailFetcher(t *testing.T, logsBody string, writer *bytes.Buffer, gotOptions **corev1.PodLogOptions) *LogFetcher {
	t.Helper()
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		ga := action.(clientgotesting.GenericAction)
		*gotOptions = ga.GetValue().(*corev1.PodLogOptions)
		return true, &runtime.Unknown{Raw: []byte(logsBody)}, nil
	})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, writer)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.DEBUG
	return fetcher
}

// A finite --tail never reaches the server: the full window is read and the
// last N entries are kept after filtering and grouping. A live follow keeps
// server-side tailing.
func TestPodLogOptionsGatesTailLines(t *testing.T) {
	tail := int64(7)
	finite := &LogFetcher{Follow: false, TailLines: &tail}
	if got := finite.podLogOptions("app"); got.TailLines != nil {
		t.Fatalf("finite TailLines = %v, want nil (client-side window)", *got.TailLines)
	}
	if got := finite.clientTail(); got != 7 {
		t.Fatalf("finite clientTail = %d, want 7", got)
	}

	live := &LogFetcher{Follow: true, TailLines: &tail}
	if got := live.podLogOptions("app"); got.TailLines == nil || *got.TailLines != 7 {
		t.Fatalf("follow TailLines = %v, want 7", got.TailLines)
	}
	if got := live.clientTail(); got != -1 {
		t.Fatalf("follow clientTail = %d, want -1", got)
	}

	plain := &LogFetcher{}
	if got := plain.podLogOptions("app"); got.TailLines != nil {
		t.Fatalf("unset TailLines = %v, want nil", *got.TailLines)
	}
	if got := plain.clientTail(); got != -1 {
		t.Fatalf("unset clientTail = %d, want -1", got)
	}
}

// --tail 2 over five info lines shows the last two, and the server saw no
// TailLines.
func TestGetLogsTailKeepsLastEntries(t *testing.T) {
	var buf bytes.Buffer
	var gotOptions *corev1.PodLogOptions
	fetcher := tailFetcher(t, "INFO one\nINFO two\nINFO three\nINFO four\nINFO five\n", &buf, &gotOptions)
	tail := int64(2)
	fetcher.TailLines = &tail

	if err := fetcher.GetLogs(context.Background()); err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if gotOptions.TailLines != nil {
		t.Fatalf("server TailLines = %v, want nil", *gotOptions.TailLines)
	}
	out := buf.String()
	for _, want := range []string{"four", "five"} {
		if !strings.Contains(out, want) {
			t.Errorf("tail output missing %q:\n%s", want, out)
		}
	}
	for _, dropped := range []string{"INFO one\n", "two\n", "three\n"} {
		if strings.Contains(out, dropped) {
			t.Errorf("tail output should not contain %q:\n%s", dropped, out)
		}
	}
}

// The window applies after filtering: tail 2 with an ERROR floor over mostly
// info lines yields the error entries, not an empty page and not raw lines.
func TestGetLogsTailAppliesAfterFiltering(t *testing.T) {
	var buf bytes.Buffer
	var gotOptions *corev1.PodLogOptions
	body := "INFO one\nINFO two\nERROR boom\n  at fail.go:1\nINFO three\nERROR late\n"
	fetcher := tailFetcher(t, body, &buf, &gotOptions)
	fetcher.FilterLevel = logging.ERROR
	tail := int64(2)
	fetcher.TailLines = &tail

	if err := fetcher.GetLogs(context.Background()); err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	out := buf.String()
	// Two anchors retained (boom + late); boom's frame rides along whole.
	for _, want := range []string{"boom", "fail.go", "late"} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered tail output missing %q:\n%s", want, out)
		}
	}
	for _, dropped := range []string{"INFO one", "two\n", "three"} {
		if strings.Contains(out, dropped) {
			t.Errorf("filtered tail output should not contain %q:\n%s", dropped, out)
		}
	}
}
