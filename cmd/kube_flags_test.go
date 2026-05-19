package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestKubeOptionsFromInheritedPersistentFlags(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	addKubeFlags(root)

	var gotContext, gotNamespace, gotKubeconfig string
	child := &cobra.Command{
		Use: "child",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := kubeOptionsFromFlags(cmd)
			if err != nil {
				return err
			}
			gotContext = opts.Context
			gotNamespace = opts.Namespace
			gotKubeconfig = opts.Kubeconfig
			return nil
		},
	}
	root.AddCommand(child)
	root.SetArgs([]string{"--context", "dev", "child", "--namespace", "app", "--kubeconfig", "/tmp/kubeconfig"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}

	if gotContext != "dev" {
		t.Fatalf("context = %q, want %q", gotContext, "dev")
	}
	if gotNamespace != "app" {
		t.Fatalf("namespace = %q, want %q", gotNamespace, "app")
	}
	if gotKubeconfig != "/tmp/kubeconfig" {
		t.Fatalf("kubeconfig = %q, want %q", gotKubeconfig, "/tmp/kubeconfig")
	}
}
