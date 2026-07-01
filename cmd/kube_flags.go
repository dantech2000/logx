package cmd

import (
	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
	k8skubernetes "k8s.io/client-go/kubernetes"
)

func addKubeFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP(flagNamespace, "n", "", "Kubernetes namespace (defaults to current context's namespace)")
	cmd.PersistentFlags().String(flagContext, "", "Kubernetes context to use")
	cmd.PersistentFlags().String(flagKubeconfig, "", "Path to the kubeconfig file")
}

func kubeOptionsFromFlags(cmd *cobra.Command) (kubernetes.ClientOptions, error) {
	namespace, err := getStringFlag(cmd, flagNamespace)
	if err != nil {
		return kubernetes.ClientOptions{}, err
	}

	contextName, err := getStringFlag(cmd, flagContext)
	if err != nil {
		return kubernetes.ClientOptions{}, err
	}

	kubeconfig, err := getStringFlag(cmd, flagKubeconfig)
	if err != nil {
		return kubernetes.ClientOptions{}, err
	}

	return kubernetes.ClientOptions{
		Context:    contextName,
		Namespace:  namespace,
		Kubeconfig: kubeconfig,
	}, nil
}

// newKubernetesClient builds a Kubernetes client from resolved options. It is a
// package-level variable so tests can substitute a fake clientset in place of a
// real cluster connection.
var newKubernetesClient = func(opts kubernetes.ClientOptions) (k8skubernetes.Interface, string, error) {
	clientset, namespace, err := kubernetes.GetKubernetesClient(opts)
	if err != nil {
		// Return an untyped nil interface on error so callers that compare the
		// result to nil are not surprised by a typed-nil *Clientset.
		return nil, "", err
	}
	return clientset, namespace, nil
}

func kubernetesClientFromFlags(cmd *cobra.Command) (k8skubernetes.Interface, string, error) {
	opts, err := kubeOptionsFromFlags(cmd)
	if err != nil {
		return nil, "", err
	}

	return newKubernetesClient(opts)
}
