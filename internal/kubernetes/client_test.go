package kubernetes

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.Clusters["dev"] = &clientcmdapi.Cluster{Server: "https://dev.example.invalid"}
	config.Clusters["prod"] = &clientcmdapi.Cluster{Server: "https://prod.example.invalid"}
	config.AuthInfos["user"] = &clientcmdapi.AuthInfo{Token: "test-token"}
	config.Contexts["dev"] = &clientcmdapi.Context{Cluster: "dev", AuthInfo: "user", Namespace: "dev-ns"}
	config.Contexts["prod"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "user", Namespace: "prod-ns"}
	config.Contexts["empty"] = &clientcmdapi.Context{Cluster: "dev", AuthInfo: "user"}
	config.CurrentContext = "dev"

	path := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	return path
}

func TestNewClientConfigResolvesCurrentContextNamespace(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)

	namespace, _, err := NewClientConfig(ClientOptions{Kubeconfig: kubeconfig}).Namespace()
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	if namespace != "dev-ns" {
		t.Fatalf("namespace = %q, want %q", namespace, "dev-ns")
	}
}

func TestNewClientConfigAppliesContextOverride(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)

	clientConfig := NewClientConfig(ClientOptions{Kubeconfig: kubeconfig, Context: "prod"})
	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	if namespace != "prod-ns" {
		t.Fatalf("namespace = %q, want %q", namespace, "prod-ns")
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		t.Fatalf("resolve client config: %v", err)
	}
	if restConfig.Host != "https://prod.example.invalid" {
		t.Fatalf("host = %q, want prod host", restConfig.Host)
	}
}

func TestNewClientConfigAppliesNamespaceOverride(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)

	namespace, _, err := NewClientConfig(ClientOptions{
		Kubeconfig: kubeconfig,
		Context:    "prod",
		Namespace:  "override-ns",
	}).Namespace()
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	if namespace != "override-ns" {
		t.Fatalf("namespace = %q, want %q", namespace, "override-ns")
	}
}

func TestGetKubernetesClientDefaultsEmptyNamespace(t *testing.T) {
	kubeconfig := writeTestKubeconfig(t)

	_, namespace, err := GetKubernetesClient(ClientOptions{Kubeconfig: kubeconfig, Context: "empty"})
	if err != nil {
		t.Fatalf("get kubernetes client: %v", err)
	}

	if namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want %q", namespace, DefaultNamespace)
	}
}
