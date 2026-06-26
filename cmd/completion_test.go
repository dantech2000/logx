package cmd

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8skubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// completionTestCmd builds a command with kube flags merged so the completion
// functions can read them without a full Execute.
func completionTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "logs"}
	addKubeFlags(cmd)
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func TestCompletePodNames(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-2", Namespace: "default"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-1", Namespace: "default"}},
	)
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	names, directive := completePodNames(completionTestCmd(t), nil, "api")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"api-1", "api-2"}) {
		t.Fatalf("names = %v, want [api-1 api-2]", names)
	}
}

func TestCompletePodNamesClientError(t *testing.T) {
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return nil, "", errors.New("no client")
	})
	_, directive := completePodNames(completionTestCmd(t), nil, "")
	if directive != cobra.ShellCompDirectiveError {
		t.Fatalf("directive = %v, want Error", directive)
	}
}

func TestCompleteContainerNames(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "img"},
				{Name: "agent", Image: "img"},
				{Name: "sidecar", Image: "img"},
			},
		},
	}
	client := fake.NewSimpleClientset(pod)
	withClientFactory(t, func(kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
		return client, "default", nil
	})

	names, directive := completeContainerNames(completionTestCmd(t), []string{"api-1"}, "a")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"agent", "app"}) {
		t.Fatalf("names = %v, want [agent app]", names)
	}
}

func TestCompleteContainerNamesNoArgs(t *testing.T) {
	_, directive := completeContainerNames(completionTestCmd(t), nil, "")
	if directive != cobra.ShellCompDirectiveError {
		t.Fatalf("directive = %v, want Error when no pod arg is given", directive)
	}
}

func TestStaticFlagCompletion(t *testing.T) {
	complete := staticFlagCompletion("dark", "light")

	all, directive := complete(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if !slices.Equal(all, []string{"dark", "light"}) {
		t.Fatalf("values = %v, want [dark light]", all)
	}

	filtered, _ := complete(nil, nil, "da")
	if !slices.Equal(filtered, []string{"dark"}) {
		t.Fatalf("prefix-filtered = %v, want [dark]", filtered)
	}

	if none, _ := complete(nil, nil, "zzz"); len(none) != 0 {
		t.Fatalf("non-matching prefix = %v, want empty", none)
	}
}

func TestCompleteFieldList(t *testing.T) {
	// A bare prefix completes a field name.
	got, directive := completeFieldList(nil, nil, "lev")
	if directive&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Fatalf("directive = %v, want NoSpace set", directive)
	}
	if !slices.Contains(got, "level") {
		t.Fatalf("completions = %v, want to contain level", got)
	}

	// A comma-separated list completes only the trailing element while preserving
	// the already-typed prefix.
	got, _ = completeFieldList(nil, nil, "ts,msg,lev")
	for _, v := range got {
		if !strings.HasPrefix(v, "ts,msg,") {
			t.Fatalf("completion %q dropped the existing fields prefix", v)
		}
	}
	if !slices.Contains(got, "ts,msg,level") {
		t.Fatalf("completions = %v, want to contain ts,msg,level", got)
	}
}

func TestCompleteWhereField(t *testing.T) {
	// Before any operator, offer field-name hints with NoSpace so the operator can
	// be appended.
	got, directive := completeWhereField(nil, nil, "stat")
	if directive&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Fatalf("directive = %v, want NoSpace set", directive)
	}
	if !slices.Contains(got, "status") || !slices.Contains(got, "status_code") {
		t.Fatalf("completions = %v, want status hints", got)
	}

	// Once an operator is typed there is a value we cannot predict, so stop hinting.
	if got, _ := completeWhereField(nil, nil, "status>="); len(got) != 0 {
		t.Fatalf("expected no hints once an operator is present, got %v", got)
	}
}

// TestRegisteredFlagCompletions checks the new flags are actually wired to their
// completion functions on the built commands (not just that the helpers exist).
func TestRegisteredFlagCompletions(t *testing.T) {
	cmd := &cobra.Command{Use: "logs"}
	addLogFlags(cmd)

	for _, flag := range []string{flagLevel, flagOutput, flagFields, flagWhere} {
		if _, ok := cmd.GetFlagCompletionFunc(flag); !ok {
			t.Fatalf("flag --%s has no registered completion function", flag)
		}
	}
}
