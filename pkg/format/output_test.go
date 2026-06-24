package format

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dantech2000/logx/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestOutputFormatterFormatsContainerFixtures(t *testing.T) {
	pod := readPodFixture(t, "../kubernetes/testdata/pods/multi-container-statuses.yaml")
	clientset := fake.NewSimpleClientset(pod)
	containers, err := kubernetes.ListContainers(context.Background(), clientset, pod.Namespace, pod.Name)
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}

	formatter := NewOutputFormatter(pod.Name, pod.Namespace, containers)
	tests := []struct {
		name     string
		format   string
		expected []string
		hidden   []string
	}{
		{
			name:   "default table",
			format: "",
			expected: []string{
				"Pod: multi-container-pod",
				"Namespace: default",
				"app [Running] (ghcr.io/example/app:v2)",
				"sidecar [Waiting (CrashLoopBackOff)] (ghcr.io/example/sidecar:v1)",
				"worker [Terminated (Error)] (ghcr.io/example/worker:v3)",
			},
			hidden: []string{"migrate"},
		},
		{
			name:   "json",
			format: "json",
			expected: []string{
				`"pod": "multi-container-pod"`,
				`"namespace": "default"`,
				`"name": "sidecar"`,
				`"status": "Waiting (CrashLoopBackOff)"`,
			},
			hidden: []string{"migrate"},
		},
		{
			name:   "yaml",
			format: "yaml",
			expected: []string{
				"pod: multi-container-pod",
				"namespace: default",
				"name: worker",
				"status: Terminated (Error)",
			},
			hidden: []string{"migrate"},
		},
		{
			name:     "posix",
			format:   "posix",
			expected: []string{"app\nsidecar\nworker"},
			hidden:   []string{"migrate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatter.FormatOutput(tt.format)
			if err != nil {
				t.Fatalf("FormatOutput(%q) error = %v", tt.format, err)
			}
			for _, expected := range tt.expected {
				if !strings.Contains(got, expected) {
					t.Fatalf("output missing %q: %q", expected, got)
				}
			}
			for _, hidden := range tt.hidden {
				if strings.Contains(got, hidden) {
					t.Fatalf("output included %q: %q", hidden, got)
				}
			}
		})
	}
}

func TestOutputFormatterJSONRoundTrips(t *testing.T) {
	formatter := NewOutputFormatter("mypod", "ns", []kubernetes.ContainerInfo{
		{Name: "app", Ready: true, Status: "Running", Image: "repo/app:1.0"},
	})
	out, err := formatter.FormatOutput("json")
	if err != nil {
		t.Fatalf("FormatOutput(json) error = %v", err)
	}

	var decoded containerListDTO
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("json output does not round-trip: %v\n%s", err, out)
	}
	if decoded.Pod != "mypod" || decoded.Namespace != "ns" || len(decoded.Containers) != 1 {
		t.Fatalf("decoded = %+v, want pod/ns/1 container", decoded)
	}
	c := decoded.Containers[0]
	if c.Name != "app" || !c.Ready || c.Status != "Running" || c.Image != "repo/app:1.0" {
		t.Fatalf("decoded container = %+v", c)
	}
}

func TestOutputFormatterSanitizesMachineOutput(t *testing.T) {
	// A crafted image reference carrying an ANSI escape must not survive into any
	// output format that may be printed to a terminal.
	evil := "repo/app:\x1b[31mred"
	formatter := NewOutputFormatter("pod", "ns", []kubernetes.ContainerInfo{
		{Name: "app", Ready: true, Status: "Running", Image: evil},
	})
	for _, format := range []string{"json", "yaml", "posix", "table"} {
		out, err := formatter.FormatOutput(format)
		if err != nil {
			t.Fatalf("FormatOutput(%q) error = %v", format, err)
		}
		if strings.Contains(out, "\x1b") {
			t.Fatalf("format %q leaked raw escape byte: %q", format, out)
		}
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

	return &pod
}
