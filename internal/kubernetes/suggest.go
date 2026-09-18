package kubernetes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// suggestPodName returns the name of the pod in namespace closest to the
// misspelled want, or "" when nothing is close enough to suggest. It exists
// for the typo case (`logx logs api-7d9f8b6c4-xv2q1`): a raw "not found" names
// the problem but not the fix. A list failure yields no suggestion rather than
// masking the original error, so callers simply skip the hint.
func suggestPodName(ctx context.Context, clientset kubernetes.Interface, namespace, want string) string {
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	best := ""
	bestDistance := maxSuggestionDistance(want)
	for _, pod := range pods.Items {
		if d := editDistance(want, pod.Name); d < bestDistance {
			best, bestDistance = pod.Name, d
		}
	}
	return best
}

// maxSuggestionDistance bounds how far a suggestion may stray: small names get
// an absolute budget, long generated names (deploy-abc123-xyz) a proportional
// one. Without a budget every typo "suggests" something, which is worse than
// silence.
func maxSuggestionDistance(want string) int {
	if n := len(want) / 3; n > 2 {
		return n
	}
	return 2
}

// editDistance is the Levenshtein distance over runes. The namespace pod list
// is small (tens, not millions), so the quadratic table is cheaper than a
// dependency.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	previous := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current := make([]int, len(br)+1)
		current[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(br)]
}
