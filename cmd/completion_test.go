package cmd

import (
	"errors"
	"slices"
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
