package kubernetes

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
			name:    "multi container statuses",
			fixture: "testdata/pods/multi-container-statuses.yaml",
			want: []ContainerInfo{
				{Name: "app", Ready: true, Status: "Running", Image: "ghcr.io/example/app:v2"},
				{Name: "sidecar", Ready: false, Status: "Waiting (CrashLoopBackOff)", Image: "ghcr.io/example/sidecar:v1"},
				{Name: "worker", Ready: false, Status: "Terminated (Error)", Image: "ghcr.io/example/worker:v3"},
			},
			notWanted: []string{"migrate"},
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
