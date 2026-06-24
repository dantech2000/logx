// Package kubernetes provides functionality for interacting with Kubernetes clusters
package kubernetes

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultNamespace is the namespace used when none is specified.
const DefaultNamespace = "default"

// ClientOptions describes kubeconfig overrides used to create a Kubernetes client.
type ClientOptions struct {
	Context    string
	Namespace  string
	Kubeconfig string
}

// NewClientConfig creates a deferred kubeconfig loader using kubectl-compatible overrides.
func NewClientConfig(opts ClientOptions) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: opts.Context,
	}
	if opts.Namespace != "" {
		configOverrides.Context.Namespace = opts.Namespace
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// GetKubernetesClient creates a new Kubernetes client using kubeconfig and explicit overrides.
// It returns the clientset, the resolved namespace, and any error encountered.
func GetKubernetesClient(opts ClientOptions) (*kubernetes.Clientset, string, error) {
	kubeConfig := NewClientConfig(opts)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	namespace, _, err := kubeConfig.Namespace()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get namespace from config: %w", err)
	}
	if namespace == "" {
		namespace = DefaultNamespace
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, namespace, nil
}
