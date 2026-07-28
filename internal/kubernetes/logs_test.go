package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dantech2000/logx/internal/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

type logFilterCase struct {
	name     string
	level    logging.LogLevel
	hidden   []string
	expected []string
}

func TestLogFetcher_GetLogs(t *testing.T) {
	// Create a fake clientset
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
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

			err := fetcher.GetLogs(context.Background())
			if (err != nil) != tt.wantError {
				t.Errorf("GetLogs() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestLogFetcher_GetLogsFiltersByLevel(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		cases   []logFilterCase
	}{
		{
			name:    "Traefik bracketed logs",
			fixture: "testdata/logs/traefik.txt",
			cases: []logFilterCase{
				{
					name:     "DEBUG includes all parsed levels",
					level:    logging.DEBUG,
					expected: []string{"encoded characters", "Traefik version", "Starting provider", "Provider failed", "Cross-namespace reference"},
				},
				{
					name:     "INFO hides lower levels",
					level:    logging.INFO,
					expected: []string{"encoded characters", "Traefik version", "Starting provider", "Provider failed", "Cross-namespace reference"},
				},
				{
					name:     "WARN hides info",
					level:    logging.WARN,
					hidden:   []string{"Traefik version", "Starting provider"},
					expected: []string{"encoded characters", "Provider failed", "Cross-namespace reference"},
				},
				{
					name:     "ERROR only includes errors",
					level:    logging.ERROR,
					hidden:   []string{"encoded characters", "Traefik version", "Starting provider", "Cross-namespace reference"},
					expected: []string{"Provider failed"},
				},
			},
		},
		{
			name:    "JSON logs",
			fixture: "testdata/logs/json-mixed.txt",
			cases:   logLevelFilterCases("cache warmed", "server started", "request latency high", "request failed"),
		},
		{
			name:    "Logfmt logs",
			fixture: "testdata/logs/logfmt-mixed.txt",
			cases:   logLevelFilterCases("queue poll started", "job completed", "retry scheduled", "job failed"),
		},
		{
			name:    "Plain text logs",
			fixture: "testdata/logs/plaintext-mixed.txt",
			cases: []logFilterCase{
				{
					name:     "DEBUG includes all parsed levels",
					level:    logging.DEBUG,
					expected: []string{"scheduler tick", "request accepted", "upstream latency high", "upstream unavailable", "line without an explicit level"},
				},
				{
					name:     "INFO hides debug and unknown",
					level:    logging.INFO,
					hidden:   []string{"scheduler tick", "line without an explicit level"},
					expected: []string{"request accepted", "upstream latency high", "upstream unavailable"},
				},
				{
					name:     "WARN hides info debug and unknown",
					level:    logging.WARN,
					hidden:   []string{"scheduler tick", "request accepted", "line without an explicit level"},
					expected: []string{"upstream latency high", "upstream unavailable"},
				},
				{
					name:     "ERROR only includes errors",
					level:    logging.ERROR,
					hidden:   []string{"scheduler tick", "request accepted", "upstream latency high", "line without an explicit level"},
					expected: []string{"upstream unavailable"},
				},
			},
		},
		{
			name:    "Multiline stack trace logs",
			fixture: "testdata/logs/multiline-stacktrace.txt",
			cases: []logFilterCase{
				{
					name:     "DEBUG includes continuation lines",
					level:    logging.DEBUG,
					expected: []string{"request accepted", "request failed", "RuntimeException", "Client.call", "Handler.handle", "retry scheduled", "retry attempt 2"},
				},
				{
					// Indented continuation frames inherit their parent's level, so
					// the ERROR stack frames and the WARN continuation stay visible at
					// INFO; only the flush-left exception-class line (which has no
					// level and no indentation) is treated as an independent DEBUG
					// line and hidden.
					name:     "INFO keeps indented frames, hides flush-left continuation",
					level:    logging.INFO,
					hidden:   []string{"RuntimeException"},
					expected: []string{"request accepted", "request failed", "Client.call", "Handler.handle", "retry scheduled", "retry attempt 2"},
				},
				{
					name:     "WARN hides info and the flush-left continuation",
					level:    logging.WARN,
					hidden:   []string{"request accepted", "RuntimeException"},
					expected: []string{"request failed", "Client.call", "Handler.handle", "retry scheduled", "retry attempt 2"},
				},
				{
					// The whole ERROR stack trace (the indented frames) is visible at
					// ERROR, which is the key win of continuation grouping.
					name:     "ERROR keeps the indented stack frames",
					level:    logging.ERROR,
					hidden:   []string{"request accepted", "RuntimeException", "retry scheduled", "retry attempt 2"},
					expected: []string{"request failed", "Client.call", "Handler.handle"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			for _, tc := range tt.cases {
				t.Run(tc.name, func(t *testing.T) {
					var buf bytes.Buffer
					fetcher := newTestLogFetcher(t, string(logs), &buf)
					fetcher.FilterLevel = tc.level

					if err := fetcher.GetLogs(context.Background()); err != nil {
						t.Fatalf("GetLogs() error = %v", err)
					}

					assertOutputContains(t, buf.String(), tc.expected, tc.hidden)
				})
			}
		})
	}
}

func logLevelFilterCases(debugMsg, infoMsg, warnMsg, errorMsg string) []logFilterCase {
	return []logFilterCase{
		{
			name:     "DEBUG includes all parsed levels",
			level:    logging.DEBUG,
			expected: []string{debugMsg, infoMsg, warnMsg, errorMsg},
		},
		{
			name:     "INFO hides debug",
			level:    logging.INFO,
			hidden:   []string{debugMsg},
			expected: []string{infoMsg, warnMsg, errorMsg},
		},
		{
			name:     "WARN hides info and debug",
			level:    logging.WARN,
			hidden:   []string{debugMsg, infoMsg},
			expected: []string{warnMsg, errorMsg},
		},
		{
			name:     "ERROR only includes errors",
			level:    logging.ERROR,
			hidden:   []string{debugMsg, infoMsg, warnMsg},
			expected: []string{errorMsg},
		},
	}
}

func assertOutputContains(t *testing.T, got string, expected []string, hidden []string) {
	t.Helper()

	for _, hiddenText := range hidden {
		if strings.Contains(got, hiddenText) {
			t.Fatalf("output included filtered log %q: %q", hiddenText, got)
		}
	}
	for _, expectedText := range expected {
		if !strings.Contains(got, expectedText) {
			t.Fatalf("output missing expected log %q: %q", expectedText, got)
		}
	}
}

func TestLogFetcher_GetLogsPassesPodLogOptions(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	var gotOptions *corev1.PodLogOptions
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		genericAction, ok := action.(clientgotesting.GenericAction)
		if !ok {
			t.Fatalf("expected GenericAction, got %T", action)
		}
		options, ok := genericAction.GetValue().(*corev1.PodLogOptions)
		if !ok {
			t.Fatalf("expected *corev1.PodLogOptions, got %T", genericAction.GetValue())
		}
		gotOptions = options
		return true, &runtime.Unknown{Raw: []byte("2026-05-15T00:38:04Z ERROR request failed")}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "app-image"},
				{Name: "sidecar", Image: "sidecar-image"},
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", RestartCount: 1},
				{Name: "sidecar", RestartCount: 0},
			},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Error creating test pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "test-pod", true, true, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.ERROR
	fetcher.Timestamps = true
	tail := int64(25)
	fetcher.TailLines = &tail
	since := int64(600)
	fetcher.SinceSeconds = &since

	if err := fetcher.GetLogs(context.Background()); err != nil {
		t.Fatalf("GetLogs() error = %v", err)
	}
	if gotOptions == nil {
		t.Fatal("GetLogs() did not request pod logs")
	}
	if gotOptions.Container != "app" {
		t.Fatalf("Container = %q, want %q", gotOptions.Container, "app")
	}
	if !gotOptions.Follow {
		t.Fatal("Follow = false, want true")
	}
	if !gotOptions.Previous {
		t.Fatal("Previous = false, want true")
	}
	if !gotOptions.Timestamps {
		t.Fatal("Timestamps = false, want true")
	}
	if gotOptions.TailLines == nil || *gotOptions.TailLines != 25 {
		t.Fatalf("TailLines = %v, want 25", gotOptions.TailLines)
	}
	if gotOptions.SinceSeconds == nil || *gotOptions.SinceSeconds != 600 {
		t.Fatalf("SinceSeconds = %v, want 600", gotOptions.SinceSeconds)
	}
}

func TestLogFetcher_GetLogsFormatsDeterministicFixtureOutput(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		expected string
		level    logging.LogLevel
	}{
		{
			name:     "plain text WARN output",
			fixture:  "testdata/logs/plaintext-mixed.txt",
			expected: "testdata/expected/plaintext-warn.txt",
			level:    logging.WARN,
		},
		{
			name:     "bracketed WARN output",
			fixture:  "testdata/logs/traefik.txt",
			expected: "testdata/expected/traefik-warn.txt",
			level:    logging.WARN,
		},
		{
			name:     "multiline WARN output",
			fixture:  "testdata/logs/multiline-stacktrace.txt",
			expected: "testdata/expected/multiline-warn.txt",
			level:    logging.WARN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			want, err := os.ReadFile(tt.expected)
			if err != nil {
				t.Fatalf("read expected output: %v", err)
			}

			var buf bytes.Buffer
			fetcher := newTestLogFetcher(t, string(logs), &buf)
			fetcher.FilterLevel = tt.level

			if err := fetcher.GetLogs(context.Background()); err != nil {
				t.Fatalf("GetLogs() error = %v", err)
			}
			if got := buf.String(); got != string(want) {
				t.Fatalf("output = %q, want %q", got, string(want))
			}
		})
	}
}

func TestLogFetcher_GetTimelineSortsLogsAndEvents(t *testing.T) {
	logs := strings.Join([]string{
		"2026-05-15T00:38:02Z INFO application started",
		"2026-05-15T00:38:04Z ERROR request failed",
	}, "\n")
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte(logs)}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app-image"}},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Error creating test pod: %v", err)
	}

	events := []*corev1.Event{
		testPodEvent("test-pod", "Normal", "Scheduled", "Successfully assigned default/test-pod", "2026-05-15T00:38:01Z"),
		testPodEvent("test-pod", "Warning", "Unhealthy", "Readiness probe failed", "2026-05-15T00:38:03Z"),
		testPodEvent("other-pod", "Warning", "BackOff", "Back-off restarting failed container", "2026-05-15T00:38:00Z"),
		testEvent("targetgroupbinding", "k8s-sample-edge-04b4cdfbf4", "Normal", "SuccessfullyReconciled", "Successfully reconciled", "2026-05-15T00:38:05Z"),
	}
	for _, event := range events {
		if _, err := clientset.CoreV1().Events("default").Create(context.Background(), event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.INFO

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	// Only the target pod's own events should appear, interleaved with its logs.
	assertInOrder(t, got, []string{
		"[2026-05-15 00:38:01] [EVENT] [Normal] pod/test-pod Scheduled: Successfully assigned default/test-pod",
		"[2026-05-15 00:38:02] [LOG] [INFO] application started",
		"[2026-05-15 00:38:03] [EVENT] [Warning] pod/test-pod Unhealthy: Readiness probe failed",
		"[2026-05-15 00:38:04] [LOG] [ERROR] request failed",
	})
	// Events belonging to other objects in the namespace must be excluded.
	for _, unwanted := range []string{"other-pod", "targetgroupbinding"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("timeline leaked unrelated object %q: %q", unwanted, got)
		}
	}
}

func TestLogFetcher_GetTimelineMatchesGoldenFixture(t *testing.T) {
	logs, err := os.ReadFile("testdata/timeline/logs.txt")
	if err != nil {
		t.Fatalf("read timeline logs fixture: %v", err)
	}
	want, err := os.ReadFile("testdata/timeline/expected-warn.txt")
	if err != nil {
		t.Fatalf("read timeline expected output: %v", err)
	}

	pod := readPodFixture(t, "testdata/pods/single-container.yaml")
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: logs}, nil
	})
	for _, event := range readEventListFixture(t, "testdata/timeline/events.yaml").Items {
		if _, err := clientset.CoreV1().Events(event.Namespace).Create(context.Background(), &event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, pod.Namespace, pod.Name, false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.WARN

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}
	if got := buf.String(); got != string(want) {
		t.Fatalf("timeline output = %q, want %q", got, string(want))
	}
}

// TestLogFetcher_GetTimelineExcludesForeignObjectEvents verifies that a pod's
// timeline shows only that pod's own events. The real-traefik fixture's events
// all belong to TargetGroupBinding objects (not the pod), so none of them should
// surface — only the pod's logs appear. This guards against leaking unrelated
// namespace activity into a single pod's timeline.
func TestLogFetcher_GetTimelineExcludesForeignObjectEvents(t *testing.T) {
	logs, err := os.ReadFile("testdata/real-traefik/logs.txt")
	if err != nil {
		t.Fatalf("read real timeline logs fixture: %v", err)
	}

	pod := readPodFixture(t, "testdata/real-traefik/pod.yaml")
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: logs}, nil
	})
	for _, event := range readEventListFixture(t, "testdata/real-traefik/events.yaml").Items {
		if _, err := clientset.CoreV1().Events(event.Namespace).Create(context.Background(), &event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, pod.Namespace, pod.Name, false, false, &buf)
	fetcher.ContainerName = "sample-proxy"
	fetcher.FilterLevel = logging.WARN

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	// The pod's own logs still appear.
	assertInOrder(t, got, []string{
		"[2026-05-15 00:38:04] [LOG] [WARN] Traefik can reject some encoded characters",
		"[2026-05-15 00:38:05] [LOG] [WARN] Cross-namespace reference between IngressRoutes and resources is enabled",
	})
	// None of the TargetGroupBinding events should be present.
	if strings.Contains(got, "targetgroupbinding") {
		t.Fatalf("timeline leaked foreign-object events: %q", got)
	}
	if count := strings.Count(got, "[EVENT]"); count != 0 {
		t.Fatalf("timeline event count = %d, want 0 (no pod events in fixture): %q", count, got)
	}
}

// TestLogFetcher_GetTimelineAggregatesRepeatedEvents verifies that an event that
// has fired multiple times is annotated with its occurrence count.
func TestLogFetcher_GetTimelineAggregatesRepeatedEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("")}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app-image"}},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create test pod: %v", err)
	}

	event := testPodEvent("test-pod", "Warning", "BackOff", "Back-off restarting failed container", "2026-05-15T00:38:05Z")
	event.Count = 7
	if _, err := clientset.CoreV1().Events("default").Create(context.Background(), event, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.WARN

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	if got := buf.String(); !strings.Contains(got, "Back-off restarting failed container (x7)") {
		t.Fatalf("timeline missing aggregated count: %q", got)
	}
}

func TestLogFetcher_GetTimelineOrdersRealPlainFrameworkLogs(t *testing.T) {
	logs, err := os.ReadFile("testdata/real-portal/logs.txt")
	if err != nil {
		t.Fatalf("read real portal logs fixture: %v", err)
	}

	pod := readPodFixture(t, "testdata/real-portal/pod.yaml")
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: logs}, nil
	})
	for _, event := range readEventListFixture(t, "testdata/real-portal/events.yaml").Items {
		if _, err := clientset.CoreV1().Events(event.Namespace).Create(context.Background(), &event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, pod.Namespace, pod.Name, false, false, &buf)
	fetcher.ContainerName = "web"
	fetcher.FilterLevel = logging.DEBUG

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	assertInOrder(t, got, []string{
		"[2026-05-20 20:37:54] [LOG] [DEBUG] ▲ Next.js 14.2.35",
		"[2026-05-20 20:37:54] [LOG] [DEBUG] - Local:        http://localhost:3100",
		"[2026-05-20 20:38:03] [LOG] [DEBUG] ✓ Ready in 9.9s",
		"[2026-05-20 20:38:05] [LOG] [INFO] [statsig] running in k8s, using forward proxy",
		"[2026-05-20 21:00:07] [LOG] [WARN] Cannot execute the operation on ended Span",
	})
	if strings.Contains(got, "[no timestamp]") {
		t.Fatalf("timeline output contains no-timestamp log lines: %q", got)
	}
}

func TestLogFetcher_GetTimelineParsesRealRailsLogs(t *testing.T) {
	logs, err := os.ReadFile("testdata/real-rails/logs.txt")
	if err != nil {
		t.Fatalf("read real rails logs fixture: %v", err)
	}

	pod := readPodFixture(t, "testdata/real-rails/pod.yaml")
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: logs}, nil
	})
	for _, event := range readEventListFixture(t, "testdata/real-rails/events.yaml").Items {
		if _, err := clientset.CoreV1().Events(event.Namespace).Create(context.Background(), &event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, pod.Namespace, pod.Name, false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.DEBUG

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	assertInOrder(t, got, []string{
		"[2026-05-20 13:26:38] [LOG] [WARN] Warning: You specified code to run in a `on_worker_boot` block",
		"[2026-05-20 13:27:22] [LOG] [DEBUG] Instrumentation: OpenTelemetry::Instrumentation::Mongo failed to install",
		"[2026-05-20 13:27:46] [LOG] [DEBUG]",
		"[2026-05-20 16:29:11] [LOG] [DEBUG] Failed to GeoIP the ip_address . (invalid address: )",
	})
	if strings.Contains(got, "[no timestamp]") {
		t.Fatalf("timeline output contains no-timestamp log lines: %q", got)
	}
	if strings.Contains(got, "[LOG] [ERROR]") {
		t.Fatalf("timeline classified successful Rails request logs as errors: %q", got)
	}
	if !strings.Contains(got, "controller=") || !strings.Contains(got, "status=200") {
		t.Fatalf("timeline did not format Rails JSON request fields: %q", got)
	}
}

func TestLogFetcher_GetTimelineShowsEventsWhenLogsUnavailable(t *testing.T) {
	pod := readPodFixture(t, "testdata/real-imagepull/pod.yaml")
	clientset := fake.NewSimpleClientset(pod)
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("container %q in pod %q is waiting to start: trying and failing to pull image", "sample-imagepull-app", pod.Name)
	})
	for _, event := range readEventListFixture(t, "testdata/real-imagepull/events.yaml").Items {
		if _, err := clientset.CoreV1().Events(event.Namespace).Create(context.Background(), &event, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, pod.Namespace, pod.Name, false, false, &buf)
	fetcher.ContainerName = "sample-imagepull-app"
	fetcher.FilterLevel = logging.DEBUG

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	// The user is told logs were unavailable, then sees the explanatory events.
	assertInOrder(t, got, []string{
		"[notice] container logs unavailable:",
		"[2026-05-20 23:42:03] [EVENT] [Normal] pod/sample-imagepull-app-59dd994c59-bhtlt BackOff: Back-off pulling image",
		"[2026-05-20 23:42:03] [EVENT] [Warning] pod/sample-imagepull-app-59dd994c59-bhtlt Failed: Error: ImagePullBackOff",
	})
	if strings.Contains(got, "[LOG]") {
		t.Fatalf("timeline output contains log entries even though logs were unavailable: %q", got)
	}
}

func TestLogFetcher_GetTimelineNotesTruncatedEvents(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("")}, nil
	})
	// Simulate a server-side truncated event list by returning a Continue token.
	clientset.PrependReactor("list", "events", func(clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.EventList{
			ListMeta: metav1.ListMeta{Continue: "more-events"},
			Items: []corev1.Event{
				*testPodEvent("test-pod", "Warning", "BackOff", "Back-off restarting failed container", "2026-05-15T00:38:05Z"),
			},
		}, nil
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "app-image"}},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create test pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.WARN

	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "[notice] event list truncated") {
		t.Fatalf("timeline missing truncation notice: %q", got)
	}
	if !strings.Contains(got, "Back-off restarting failed container") {
		t.Fatalf("timeline missing the events it did fetch: %q", got)
	}
}

func TestEventTimestampPrecedence(t *testing.T) {
	eventT := time.Date(2026, 5, 15, 1, 0, 0, 0, time.UTC)
	lastT := time.Date(2026, 5, 15, 2, 0, 0, 0, time.UTC)
	firstT := time.Date(2026, 5, 15, 3, 0, 0, 0, time.UTC)
	createdT := time.Date(2026, 5, 15, 4, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event corev1.Event
		want  time.Time
	}{
		{
			name: "EventTime wins over all others",
			event: corev1.Event{
				EventTime:      metav1.NewMicroTime(eventT),
				LastTimestamp:  metav1.NewTime(lastT),
				FirstTimestamp: metav1.NewTime(firstT),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdT)},
			},
			want: eventT,
		},
		{
			name: "LastTimestamp when EventTime is zero",
			event: corev1.Event{
				LastTimestamp:  metav1.NewTime(lastT),
				FirstTimestamp: metav1.NewTime(firstT),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdT)},
			},
			want: lastT,
		},
		{
			name: "FirstTimestamp when EventTime and LastTimestamp are zero",
			event: corev1.Event{
				FirstTimestamp: metav1.NewTime(firstT),
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdT)},
			},
			want: firstT,
		},
		{
			name: "CreationTimestamp as the final fallback",
			event: corev1.Event{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdT)},
			},
			want: createdT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventTimestamp(tt.event); !got.Equal(tt.want) {
				t.Fatalf("eventTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventCount(t *testing.T) {
	tests := []struct {
		name  string
		event corev1.Event
		want  int32
	}{
		{
			name:  "legacy Count",
			event: corev1.Event{Count: 5},
			want:  5,
		},
		{
			name:  "Series.Count preferred over legacy Count",
			event: corev1.Event{Count: 5, Series: &corev1.EventSeries{Count: 9}},
			want:  9,
		},
		{
			name:  "falls back to legacy Count when Series.Count is zero",
			event: corev1.Event{Count: 3, Series: &corev1.EventSeries{Count: 0}},
			want:  3,
		},
		{
			name:  "zero when nothing is set",
			event: corev1.Event{},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventCount(tt.event); got != tt.want {
				t.Fatalf("eventCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatTimelineEventHandlesUnknownType(t *testing.T) {
	event := newTimelineEvent(*testEvent("Pod", "test-pod", "Critical", "NodePressure", "node is under pressure", "2026-05-15T00:38:07Z"))

	got := formatTimelineEvent(event)
	want := "[2026-05-15 00:38:07] [EVENT] [Critical] pod/test-pod NodePressure: node is under pressure"
	if got != want {
		t.Fatalf("formatTimelineEvent() = %q, want %q", got, want)
	}
}

func readEventListFixture(t *testing.T, path string) *corev1.EventList {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read event fixture: %v", err)
	}
	var events corev1.EventList
	if err := yaml.Unmarshal(data, &events); err != nil {
		t.Fatalf("unmarshal event fixture: %v", err)
	}
	for i := range events.Items {
		if events.Items[i].Namespace == "" {
			events.Items[i].Namespace = metav1.NamespaceDefault
		}
	}
	return &events
}

func testPodEvent(podName, eventType, reason, message, timestamp string) *corev1.Event {
	return testEvent("Pod", podName, eventType, reason, message, timestamp)
}

func testEvent(kind, name, eventType, reason, message, timestamp string) *corev1.Event {
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		panic(err)
	}
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "." + reason,
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      kind,
			Name:      name,
			Namespace: "default",
		},
		Type:           eventType,
		Reason:         reason,
		Message:        message,
		FirstTimestamp: metav1.NewTime(ts),
		LastTimestamp:  metav1.NewTime(ts),
	}
}

func assertInOrder(t *testing.T, got string, expected []string) {
	t.Helper()

	lastIndex := -1
	for _, value := range expected {
		index := strings.Index(got, value)
		if index == -1 {
			t.Fatalf("output missing %q: %q", value, got)
		}
		if index < lastIndex {
			t.Fatalf("output has %q out of order: %q", value, got)
		}
		lastIndex = index
	}
}

func newTestLogFetcher(t *testing.T, logs string, writer *bytes.Buffer) *LogFetcher {
	t.Helper()

	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "log" {
			return true, &runtime.Unknown{Raw: []byte(logs)}, nil
		}
		return false, nil, nil
	})

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
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Error creating test pod: %v", err)
	}

	fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, writer)
	fetcher.ContainerName = "test-container"
	return fetcher
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
			fetchedPod, err := fetcher.getPod(context.Background())
			if err != nil {
				t.Fatalf("getPod() error = %v", err)
			}
			got, err := fetcher.previousContainerTerminated(fetchedPod, tt.containerName)
			if (err != nil) != tt.wantError {
				t.Errorf("previousContainerTerminated() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if got != tt.want {
				t.Errorf("previousContainerTerminated() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPipelineRendersKubeletLines pins how the pipeline (which GetLogs drives
// line by line) renders representative kubelet-prefixed log lines.
func TestPipelineRendersKubeletLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLogs string
	}{
		{
			name:     "Plain text log",
			input:    "2024-03-15T12:19:57Z DEBUG test message",
			wantLogs: "[2024-03-15 12:19:57] [DEBUG] test message\n",
		},
		{
			name:     "JSON log",
			input:    `{"level":"info","ts":"2024-03-15T12:19:57Z","msg":"test message"}`,
			wantLogs: "[2024-03-15 12:19:57] [INFO] [logrus] test message\n",
		},
		{
			name:     "Empty log line",
			input:    "",
			wantLogs: "",
		},
		{
			name:     "Log with extra whitespace",
			input:    "  2024-03-15T12:19:57Z DEBUG test message  ",
			wantLogs: "[2024-03-15 12:19:57] [DEBUG] test message\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := logging.NewPipeline(logging.PipelineOptions{MinLevel: logging.DEBUG})

			got := ""
			if out, ok := pipeline.ProcessLine(tt.input); ok {
				got = out + "\n"
			}
			if got != tt.wantLogs {
				t.Errorf("ProcessLine() output = %q, want %q", got, tt.wantLogs)
			}
		})
	}
}

func TestPipelineFiltersByLevel(t *testing.T) {
	pipeline := logging.NewPipeline(logging.PipelineOptions{MinLevel: logging.WARN})

	if out, ok := pipeline.ProcessLine("2024-03-15T12:19:57Z INFO hidden"); ok {
		t.Fatalf("ProcessLine() kept filtered log: %q", out)
	}
	out, ok := pipeline.ProcessLine("2024-03-15T12:19:57Z ERROR shown")
	if !ok {
		t.Fatal("ProcessLine() dropped log above the filter level")
	}
	if !strings.Contains(out, "shown") {
		t.Fatalf("ProcessLine() missing expected log: %q", out)
	}
}

func TestLogFetcher_GetTimelineShowsTerminationDetails(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("2026-05-15T00:38:02Z INFO application started")}, nil
	})

	finished := metav1.Date(2026, 5, 15, 0, 38, 10, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "app-image"}}},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode:   137,
							Reason:     "OOMKilled",
							FinishedAt: finished,
						},
					},
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "test-pod", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.INFO
	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}

	got := buf.String()
	want := `[2026-05-15 00:38:10] [TERM] (previous) container "app" exited with code 137 — OOMKilled`
	if !strings.Contains(got, want) {
		t.Fatalf("timeline missing termination detail.\nwant substring: %s\ngot:\n%s", want, got)
	}
	// The termination (00:38:10) sorts after the earlier log line (00:38:02).
	assertInOrder(t, got, []string{"application started", "OOMKilled"})
}

// TestLogFetcher_GetTimelineAppliesContentFilters pins that --grep/--exclude/
// --where reach the log portion of the timeline. collectLogTimelineItems used to
// consult only FilterLevel and ignore lf.Filters entirely, so every content
// filter was silently discarded and the command exited 0 having shown lines the
// user explicitly filtered out.
func TestLogFetcher_GetTimelineAppliesContentFilters(t *testing.T) {
	logs := strings.Join([]string{
		"2026-05-15T00:38:02Z ERROR keep me",
		"2026-05-15T00:38:03Z ERROR drop me",
		"2026-05-15T00:38:04Z ERROR keep me too",
	}, "\n")

	newFetcher := func(buf *bytes.Buffer) *LogFetcher {
		clientset := fake.NewSimpleClientset()
		clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, &runtime.Unknown{Raw: []byte(logs)}, nil
		})
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		}
		if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pod: %v", err)
		}
		f := NewLogFetcher(clientset, "default", "test-pod", false, false, buf)
		f.ContainerName = "app"
		f.FilterLevel = logging.DEBUG
		return f
	}

	t.Run("grep keeps only matching lines", func(t *testing.T) {
		var buf bytes.Buffer
		f := newFetcher(&buf)
		f.Filters = logging.PipelineOptions{
			MinLevel: logging.DEBUG,
			Include:  []*regexp.Regexp{regexp.MustCompile("keep me")},
		}
		if err := f.GetTimeline(context.Background()); err != nil {
			t.Fatalf("GetTimeline error: %v", err)
		}
		if strings.Contains(buf.String(), "drop me") {
			t.Fatalf("--grep was ignored by --timeline:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "keep me") {
			t.Fatalf("--grep dropped the matching line:\n%s", buf.String())
		}
	})

	t.Run("exclude removes matching lines", func(t *testing.T) {
		var buf bytes.Buffer
		f := newFetcher(&buf)
		f.Filters = logging.PipelineOptions{
			MinLevel: logging.DEBUG,
			Exclude:  []*regexp.Regexp{regexp.MustCompile("drop me")},
		}
		if err := f.GetTimeline(context.Background()); err != nil {
			t.Fatalf("GetTimeline error: %v", err)
		}
		if strings.Contains(buf.String(), "drop me") {
			t.Fatalf("--exclude was ignored by --timeline:\n%s", buf.String())
		}
	})

	t.Run("where predicate filters entries", func(t *testing.T) {
		var buf bytes.Buffer
		f := newFetcher(&buf)
		pred, err := logging.ParseFieldPredicate("level>=FATAL")
		if err != nil {
			t.Fatalf("ParseFieldPredicate: %v", err)
		}
		f.Filters = logging.PipelineOptions{MinLevel: logging.DEBUG, Where: []logging.FieldPredicate{pred}}
		if err := f.GetTimeline(context.Background()); err != nil {
			t.Fatalf("GetTimeline error: %v", err)
		}
		if strings.Contains(buf.String(), "[LOG]") {
			t.Fatalf("--where was ignored by --timeline; no ERROR line satisfies level>=FATAL:\n%s", buf.String())
		}
	})
}

// TestPreviousContainerTerminatedCoversInitAndEphemeral pins that the -p
// precondition check sees the same set of containers the rest of the tool does.
// It scanned only pod.Status.ContainerStatuses, while podHasContainer and
// allContainerStatuses both include init and ephemeral containers — so
// `logx logs pod -c init-db -p` was accepted as a valid container and then
// rejected as nonexistent by a self-contradicting error. Reading a crashlooping
// init container's prior logs is the single most common use of -p.
func TestPreviousContainerTerminatedCoversInitAndEphemeral(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers:     []corev1.Container{{Name: "app"}},
			InitContainers: []corev1.Container{{Name: "init-db"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses:     []corev1.ContainerStatus{{Name: "app", RestartCount: 0}},
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "init-db", RestartCount: 7}},
			EphemeralContainerStatuses: []corev1.ContainerStatus{
				{Name: "debugger", RestartCount: 2},
			},
		},
	}

	lf := NewLogFetcher(fake.NewSimpleClientset(), "default", "p", false, true, &bytes.Buffer{})

	tests := []struct {
		container     string
		wantRestarted bool
		wantErr       bool
	}{
		{container: "init-db", wantRestarted: true},
		{container: "debugger", wantRestarted: true},
		{container: "app", wantRestarted: false},
		{container: "nope", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.container, func(t *testing.T) {
			got, err := lf.previousContainerTerminated(pod, tc.container)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for unknown container %q", tc.container)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.container, err)
			}
			if got != tc.wantRestarted {
				t.Fatalf("restarted = %v, want %v", got, tc.wantRestarted)
			}
		})
	}
}

// TestLogFetcher_GetTimelineExcludesOtherObjectKinds pins that the timeline
// shows only the pod's own events. The server-side field selector matches on
// involvedObject.name alone and the client-side guard compared only Name, so a
// Service, Deployment, or PVC sharing the pod's name — a common naming
// convention — leaked its events into a view documented as the pod's own.
func TestLogFetcher_GetTimelineExcludesOtherObjectKinds(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "pods/log", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &runtime.Unknown{Raw: []byte("2026-05-15T00:38:02Z INFO hello")}, nil
	})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
	if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	podEvent := testPodEvent("web", "Normal", "Scheduled", "assigned to node", "2026-05-15T00:38:01Z")
	svcEvent := testEvent("Service", "web", "Warning", "UnrelatedServiceEvent", "this belongs to the Service", "2026-05-15T00:38:03Z")
	for _, e := range []*corev1.Event{podEvent, svcEvent} {
		if _, err := clientset.CoreV1().Events("default").Create(context.Background(), e, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create event: %v", err)
		}
	}

	var buf bytes.Buffer
	fetcher := NewLogFetcher(clientset, "default", "web", false, false, &buf)
	fetcher.ContainerName = "app"
	fetcher.FilterLevel = logging.DEBUG
	if err := fetcher.GetTimeline(context.Background()); err != nil {
		t.Fatalf("GetTimeline error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "UnrelatedServiceEvent") {
		t.Fatalf("a Service event leaked into the pod's timeline:\n%s", out)
	}
	if !strings.Contains(out, "Scheduled") {
		t.Fatalf("the pod's own event was dropped:\n%s", out)
	}
}

// TestSelectContainerNameNonInteractive pins that a multi-container pod fails
// with actionable guidance instead of attempting a prompt when stdin is not a
// terminal. survey was invoked unconditionally, so in a script or pipeline it
// reported a bare "selection failed: EOF" — and, because survey defaults to
// os.Stdout rather than the command's writer, it wrote the ANSI prompt into
// redirected output (`logx logs pod -o json > out.json` landed escape sequences
// and a menu in out.json). Go test runs with stdin not a char device, so this
// exercises the non-interactive path directly.
func TestSelectContainerNameNonInteractive(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "multi", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
		},
	}

	var buf bytes.Buffer
	lf := NewLogFetcher(fake.NewSimpleClientset(), "default", "multi", false, false, &buf)

	name, err := lf.selectContainerName(pod)
	if err == nil {
		t.Fatalf("expected an error for a multi-container pod with no TTY, got container %q", name)
	}
	for _, want := range []string{"--container", "app", "sidecar"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q so the user knows what to do", err, want)
		}
	}
	if strings.Contains(err.Error(), "EOF") {
		t.Errorf("error should explain the fix, not leak the prompt failure: %q", err)
	}
	if buf.Len() != 0 {
		t.Errorf("nothing should be written to the command writer: %q", buf.String())
	}

	// A single-container pod still resolves without prompting.
	single := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "one", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "only"}}},
	}
	got, err := lf.selectContainerName(single)
	if err != nil || got != "only" {
		t.Fatalf("single-container pod: got (%q, %v), want (\"only\", nil)", got, err)
	}
}
