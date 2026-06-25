package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8skubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

// withClientFactory swaps the package-level client seam for the duration of a
// test, returning a restore function.
func withClientFactory(t *testing.T, fn func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error)) {
	t.Helper()
	orig := newKubernetesClient
	newKubernetesClient = fn
	t.Cleanup(func() { newKubernetesClient = orig })
}

func testPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "repo/app:1.0"},
				{Name: "sidecar", Image: "repo/sidecar:2.0"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "sidecar", Ready: false, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
			},
		},
	}
}

// executeContainers builds a standalone containers command (so persistent kube
// flags merge during execution) and runs it with the given args.
func executeContainers(buf *bytes.Buffer, args ...string) error {
	cmd := &cobra.Command{
		Use:           "containers",
		Args:          cobra.ExactArgs(1),
		RunE:          runContainers,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	addKubeFlags(cmd)
	cmd.Flags().StringP("output", "o", "", "")
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	return cmd.Execute()
}

// executeLogs builds a standalone logs command and runs it with the given args.
func executeLogs(buf *bytes.Buffer, args ...string) error {
	cmd := &cobra.Command{
		Use:           "logs",
		Args:          cobra.ExactArgs(1),
		RunE:          runLogs,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	addKubeFlags(cmd)
	addLogFlags(cmd)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestRunContainersTableOutput(t *testing.T) {
	client := fake.NewSimpleClientset(testPod())
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	var buf bytes.Buffer
	if err := executeContainers(&buf, "test-pod"); err != nil {
		t.Fatalf("runContainers() error = %v", err)
	}

	out := buf.String()
	for _, want := range []string{"test-pod", "app", "sidecar", "repo/app:1.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q: %q", want, out)
		}
	}
}

func TestRunContainersJSONOutput(t *testing.T) {
	client := fake.NewSimpleClientset(testPod())
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	var buf bytes.Buffer
	if err := executeContainers(&buf, "test-pod", "-o", "json"); err != nil {
		t.Fatalf("runContainers() error = %v", err)
	}

	out := buf.String()
	for _, want := range []string{`"pod": "test-pod"`, `"name": "app"`, `"image": "repo/app:1.0"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json output missing %q: %q", want, out)
		}
	}
}

func TestRunContainersClientError(t *testing.T) {
	wantErr := errors.New("boom")
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return nil, "", wantErr
	})

	var buf bytes.Buffer
	err := executeContainers(&buf, "test-pod")
	if err == nil {
		t.Fatal("runContainers() error = nil, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("runContainers() error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestRunContainersMissingPod(t *testing.T) {
	client := fake.NewSimpleClientset() // no pods
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	var buf bytes.Buffer
	if err := executeContainers(&buf, "test-pod"); err == nil {
		t.Fatal("runContainers() error = nil, want error for missing pod")
	}
}

func TestRunLogsSuccess(t *testing.T) {
	client := fake.NewSimpleClientset(testPod())
	client.PrependReactor("get", "pods/log", func(clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("2026-05-15T00:38:04Z ERROR boom")}, nil
	})
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	var buf bytes.Buffer
	if err := executeLogs(&buf, "test-pod", "--container", "app"); err != nil {
		t.Fatalf("runLogs() error = %v", err)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("logs output missing message: %q", buf.String())
	}
}

func TestRunLogsClientError(t *testing.T) {
	wantErr := errors.New("no cluster")
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return nil, "", wantErr
	})

	var buf bytes.Buffer
	err := executeLogs(&buf, "test-pod")
	if err == nil {
		t.Fatal("runLogs() error = nil, want error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("runLogs() error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestRunLogsRejectsInvalidLevel(t *testing.T) {
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		t.Error("client factory should not be called when the level is invalid")
		return nil, "", nil
	})

	var buf bytes.Buffer
	if err := executeLogs(&buf, "test-pod", "--level", "NOPE"); err == nil {
		t.Fatal("runLogs() error = nil, want error for invalid level")
	}
}
