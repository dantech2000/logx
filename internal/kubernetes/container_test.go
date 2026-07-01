package kubernetes

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func TestListContainersFromPodFixtures(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		want      []ContainerInfo
		notWanted []string
	}{
		{
			name:    "single running container",
			fixture: "testdata/pods/single-container.yaml",
			want: []ContainerInfo{
				{Name: "app", Ready: true, Status: "Running", Image: "ghcr.io/example/app:v1"},
			},
		},
		{
			name:    "multi container statuses with an init container",
			fixture: "testdata/pods/multi-container-statuses.yaml",
			want: []ContainerInfo{
				{Name: "app", Ready: true, Status: "Running", Image: "ghcr.io/example/app:v2"},
				{Name: "sidecar", Ready: false, Status: "Waiting (CrashLoopBackOff)", Image: "ghcr.io/example/sidecar:v1"},
				{Name: "worker", Ready: false, Status: "Terminated (Error)", Image: "ghcr.io/example/worker:v3"},
				{Name: "migrate", Ready: false, Status: "Unknown", Image: "ghcr.io/example/migrate:v1", Kind: "init"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := readPodFixture(t, tt.fixture)
			clientset := fake.NewSimpleClientset(pod)

			got, err := ListContainers(context.Background(), clientset, pod.Namespace, pod.Name)
			if err != nil {
				t.Fatalf("ListContainers(context.Background(), ) error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("container count = %d, want %d: %#v", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Fatalf("container[%d] = %#v, want %#v", i, got[i], want)
				}
			}
			for _, name := range tt.notWanted {
				for _, container := range got {
					if container.Name == name {
						t.Fatalf("ListContainers(context.Background(), ) included init container %q", name)
					}
				}
			}
		})
	}
}

func readPodFixture(t *testing.T, path string) *corev1.Pod {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pod fixture: %v", err)
	}
	var pod corev1.Pod
	if err := yaml.Unmarshal(data, &pod); err != nil {
		t.Fatalf("unmarshal pod fixture: %v", err)
	}
	if pod.Namespace == "" {
		pod.Namespace = metav1.NamespaceDefault
	}
	if pod.Name == "" {
		t.Fatal("pod fixture missing metadata.name")
	}

	return &pod
}

func TestListContainersMissingPod(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	if _, err := ListContainers(context.Background(), clientset, "default", "missing-pod"); err == nil {
		t.Fatal("ListContainers(context.Background(), ) error = nil, want error")
	}
}

func TestListContainersUnknownStatus(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "unknown-status-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app-image"}},
		},
	}
	clientset := fake.NewSimpleClientset(pod)

	got, err := ListContainers(context.Background(), clientset, "default", "unknown-status-pod")
	if err != nil {
		t.Fatalf("ListContainers(context.Background(), ) error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("container count = %d, want 1", len(got))
	}
	want := ContainerInfo{Name: "app", Ready: false, Status: "Unknown", Image: "app-image"}
	if got[0] != want {
		t.Fatalf("container[0] = %#v, want %#v", got[0], want)
	}
}

func TestFormatContainerInfo(t *testing.T) {
	ready := FormatContainerInfo(ContainerInfo{Name: "app", Ready: true, Status: "Running", Image: "repo/app:1.0"})
	if !strings.Contains(ready, "app") || !strings.Contains(ready, "Running") || !strings.Contains(ready, "repo/app:1.0") {
		t.Fatalf("ready row missing fields: %q", ready)
	}
	if !strings.Contains(ready, "✓") {
		t.Fatalf("ready row should use the ready symbol: %q", ready)
	}

	notReady := FormatContainerInfo(ContainerInfo{Name: "app", Ready: false, Status: "Waiting", Image: "img"})
	if !strings.Contains(notReady, "✗") {
		t.Fatalf("not-ready row should use the not-ready symbol: %q", notReady)
	}
}

func TestFormatContainerInfoSanitizes(t *testing.T) {
	got := FormatContainerInfo(ContainerInfo{Name: "app", Ready: true, Status: "Running", Image: "img\x1b[31m"})
	if strings.Contains(got, "\x1b") {
		t.Fatalf("FormatContainerInfo leaked an escape byte: %q", got)
	}
}

func TestGetLogsAcceptsInitContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "migrate", Image: "img"}},
			Containers:     []corev1.Container{{Name: "app", Image: "img"}},
		},
	}
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("2026-06-24T10:00:00Z INFO migrating schema")}, nil
	})

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.ContainerName = "migrate" // an init container

	if err := fetcher.GetLogs(context.Background()); err != nil {
		t.Fatalf("GetLogs() for init container error = %v", err)
	}
	if !strings.Contains(buf.String(), "migrating schema") {
		t.Fatalf("init container logs missing: %q", buf.String())
	}
}

func TestGetLogsRejectsUnknownContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "img"}}},
	}
	clientset := fake.NewSimpleClientset(pod)

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "p", false, false, &buf)
	fetcher.ContainerName = "nope"

	err := fetcher.GetLogs(context.Background())
	if err == nil {
		t.Fatal("GetLogs() with unknown container = nil error, want error")
	}
	// %q quoting pins that untrusted container/pod names are escaped in the
	// error text rather than interpolated raw.
	if !strings.Contains(err.Error(), `container "nope" not found in pod "p"`) {
		t.Fatalf("GetLogs() error = %v, want %%q-quoted container/pod names", err)
	}
}

func TestSelectContainerNameSingle(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "solo", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "only", Image: "img"}}},
	}
	clientset := fake.NewSimpleClientset(pod)
	fetcher := NewLogFetcher(clientset, "default", "solo", false, false, nil)

	fetched, err := fetcher.getPod(context.Background())
	if err != nil {
		t.Fatalf("getPod() error = %v", err)
	}
	name, err := fetcher.selectContainerName(fetched)
	if err != nil {
		t.Fatalf("selectContainerName() error = %v", err)
	}
	if name != "only" {
		t.Fatalf("name = %q, want %q", name, "only")
	}
}

func TestSelectContainerNameNoContainers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec:       corev1.PodSpec{},
	}
	clientset := fake.NewSimpleClientset(pod)
	fetcher := NewLogFetcher(clientset, "default", "empty", false, false, nil)

	fetched, err := fetcher.getPod(context.Background())
	if err != nil {
		t.Fatalf("getPod() error = %v", err)
	}
	if _, err := fetcher.selectContainerName(fetched); err == nil {
		t.Fatal("selectContainerName() error = nil, want error for pod with no containers")
	}
}

func TestGetPodMissingPod(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fetcher := NewLogFetcher(clientset, "default", "ghost", false, false, nil)

	if _, err := fetcher.getPod(context.Background()); err == nil {
		t.Fatal("getPod() error = nil, want error for missing pod")
	}
}

func TestListContainersUsesPodNamespace(t *testing.T) {
	pod := readPodFixture(t, "testdata/pods/single-container.yaml")
	clientset := fake.NewSimpleClientset()
	if _, err := clientset.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	if _, err := ListContainers(context.Background(), clientset, "other", pod.Name); err == nil {
		t.Fatal("ListContainers(context.Background(), ) error = nil, want namespace lookup error")
	}
}
