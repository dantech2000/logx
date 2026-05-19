package cmd

import (
	"fmt"

	"github.com/dantech2000/logx/pkg/kubernetes"
	"github.com/spf13/cobra"
	k8skubernetes "k8s.io/client-go/kubernetes"
)

func addKubeFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP("namespace", "n", "", "Kubernetes namespace (defaults to current context's namespace)")
	cmd.PersistentFlags().String("context", "", "Kubernetes context to use")
	cmd.PersistentFlags().String("kubeconfig", "", "Path to the kubeconfig file")
}

func kubeOptionsFromFlags(cmd *cobra.Command) (kubernetes.ClientOptions, error) {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return kubernetes.ClientOptions{}, fmt.Errorf("error getting namespace flag: %w", err)
	}

	contextName, err := cmd.Flags().GetString("context")
	if err != nil {
		return kubernetes.ClientOptions{}, fmt.Errorf("error getting context flag: %w", err)
	}

	kubeconfig, err := cmd.Flags().GetString("kubeconfig")
	if err != nil {
		return kubernetes.ClientOptions{}, fmt.Errorf("error getting kubeconfig flag: %w", err)
	}

	return kubernetes.ClientOptions{
		Context:    contextName,
		Namespace:  namespace,
		Kubeconfig: kubeconfig,
	}, nil
}

func kubernetesClientFromFlags(cmd *cobra.Command) (k8skubernetes.Interface, string, error) {
	opts, err := kubeOptionsFromFlags(cmd)
	if err != nil {
		return nil, "", err
	}

	clientset, namespace, err := kubernetes.GetKubernetesClient(opts)
	if err != nil {
		return nil, "", err
	}

	return clientset, namespace, nil
}
