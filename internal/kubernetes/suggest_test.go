package kubernetes

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEditDistance(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"pod", "pod", 0},
		{"fgvw7", "fgvw6", 1},
		{"kitten", "sitting", 3},
		{"", "abc", 3},
	} {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func suggestFixture(t *testing.T, names ...string) *fake.Clientset {
	t.Helper()
	clientset := fake.NewSimpleClientset()
	for _, name := range names {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		if _, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pod %q: %v", name, err)
		}
	}
	return clientset
}

// A one-character typo in a generated pod name suggests the real pod.
func TestSuggestPodNameFindsTypo(t *testing.T) {
	clientset := suggestFixture(t, "noisy-api-5b68bb8584-fgvw6", "noisy-api-5b68bb8584-qpsjn")
	if got := suggestPodName(context.Background(), clientset, "default", "noisy-api-5b68bb8584-fgvw7"); got != "noisy-api-5b68bb8584-fgvw6" {
		t.Fatalf("suggestPodName = %q, want the fgvw6 pod", got)
	}
}

// Nothing close means silence, not a wild guess; an empty namespace too.
func TestSuggestPodNameStaysSilent(t *testing.T) {
	clientset := suggestFixture(t, "noisy-api-5b68bb8584-fgvw6")
	if got := suggestPodName(context.Background(), clientset, "default", "totally-different-workload"); got != "" {
		t.Fatalf("suggestPodName = %q, want no suggestion", got)
	}
	if got := suggestPodName(context.Background(), suggestFixture(t), "default", "noisy-api-5b68bb8584-fgvw7"); got != "" {
		t.Fatalf("suggestPodName on empty namespace = %q, want no suggestion", got)
	}
}

// The typo hint surfaces on the fetch error itself, where the user reads it.
func TestFetchPodSuggestsTypo(t *testing.T) {
	clientset := suggestFixture(t, "noisy-api-5b68bb8584-fgvw6")
	_, err := fetchPod(context.Background(), clientset, "default", "noisy-api-5b68bb8584-fgvw7")
	if err == nil {
		t.Fatal("fetchPod missing pod: got nil error")
	}
	if !strings.Contains(err.Error(), `did you mean "noisy-api-5b68bb8584-fgvw6"?`) {
		t.Fatalf("fetchPod error lacks hint: %v", err)
	}
}
