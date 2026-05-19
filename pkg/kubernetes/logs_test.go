package kubernetes

import (
	"bytes"
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func TestLogFetcher_GetLogs(t *testing.T) {
	// Create a fake clientset
	clientset := fake.NewSimpleClientset()
	clientset.Fake.PrependReactor("get", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "log" {
			return true, &runtime.Unknown{Raw: []byte("fake logs")}, nil
		}
		return false, nil, nil
	})

	// Create a test pod with a single container
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "test-container",
					Ready: true,
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 1,
							Reason:   "Error",
						},
					},
					RestartCount: 1,
				},
			},
		},
	}

	// Create the pod in the fake clientset
	_, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Error creating test pod: %v", err)
	}

	tests := []struct {
		name          string
		containerName string
		follow        bool
		previous      bool
		wantError     bool
	}{
		{
			name:          "Get logs from single container",
			containerName: "test-container",
			follow:        false,
			previous:      false,
			wantError:     false,
		},
		{
			name:          "Get logs with follow",
			containerName: "test-container",
			follow:        true,
			previous:      false,
			wantError:     false,
		},
		{
			name:          "Get previous logs",
			containerName: "test-container",
			follow:        false,
			previous:      true,
			wantError:     false,
		},
		{
			name:          "Invalid container name",
			containerName: "nonexistent-container",
			follow:        false,
			previous:      false,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			fetcher := NewLogFetcher(clientset, "default", "test-pod", tt.follow, tt.previous, &buf)
			fetcher.ContainerName = tt.containerName

			err := fetcher.GetLogs()
			if (err != nil) != tt.wantError {
				t.Errorf("GetLogs() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestLogFetcher_hasPreviousContainer(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Create a test pod with a container that has previous instances
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "test-container",
					Ready:        true,
					RestartCount: 1,
				},
			},
		},
	}

	_, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Error creating test pod: %v", err)
	}

	tests := []struct {
		name          string
		containerName string
		want          bool
		wantError     bool
	}{
		{
			name:          "Container with previous instances",
			containerName: "test-container",
			want:          true,
			wantError:     false,
		},
		{
			name:          "Nonexistent container",
			containerName: "nonexistent",
			want:          false,
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, nil)
			got, err := fetcher.hasPreviousContainer(tt.containerName)
			if (err != nil) != tt.wantError {
				t.Errorf("hasPreviousContainer() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("hasPreviousContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogWriter_Write(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLogs string
	}{
		{
			name:     "Plain text log",
			input:    "2024-03-15T12:19:57Z DEBUG test message",
			wantLogs: "[2024-03-15 12:19:57] [DEBUG] 2024-03-15T12:19:57Z DEBUG test message\n",
		},
		{
			name:     "JSON log",
			input:    `{"level":"info","ts":"2024-03-15T12:19:57Z","msg":"test message"}`,
			wantLogs: "[2024-03-15 12:19:57] [INFO] [logrus] test message ts=2024-03-15T12:19:57Z\n",
		},
		{
			name:     "Empty log line",
			input:    "",
			wantLogs: "",
		},
		{
			name:     "Log with extra whitespace",
			input:    "  2024-03-15T12:19:57Z DEBUG test message  ",
			wantLogs: "[2024-03-15 12:19:57] [DEBUG] 2024-03-15T12:19:57Z DEBUG test message\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := NewLogWriter(&buf)

			n, err := writer.Write([]byte(tt.input))
			if err != nil {
				t.Errorf("Write() error = %v", err)
				return
			}

			if n != len([]byte(tt.input)) {
				t.Errorf("Write() wrote %v bytes, want %v", n, len([]byte(tt.input)))
			}

			if got := buf.String(); got != tt.wantLogs {
				t.Errorf("Write() output = %q, want %q", got, tt.wantLogs)
			}
		})
	}
}
