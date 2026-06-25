package cmd

import (
	"testing"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

func TestGetLogOptionsParsesLevelFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPod   string
		wantLevel string
	}{
		{
			name:      "root command short level flag",
			args:      []string{"test-pod", "-l", "WARN"},
			wantPod:   "test-pod",
			wantLevel: "WARN",
		},
		{
			name:      "logs subcommand long level flag",
			args:      []string{"logs", "test-pod", "--level", "ERROR"},
			wantPod:   "test-pod",
			wantLevel: "ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotOptions *logOptions
			root := &cobra.Command{
				Use:  "logx [pod-name]",
				Args: cobra.MaximumNArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					options, err := getLogOptions(cmd, args)
					if err != nil {
						return err
					}
					gotOptions = options
					return nil
				},
			}
			addLogFlags(root)
			logs := &cobra.Command{
				Use:  "logs [pod-name]",
				Args: cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					options, err := getLogOptions(cmd, args)
					if err != nil {
						return err
					}
					gotOptions = options
					return nil
				},
			}
			addLogFlags(logs)
			root.AddCommand(logs)
			root.SetArgs(tt.args)

			if err := root.Execute(); err != nil {
				t.Fatalf("execute command: %v", err)
			}
			if gotOptions == nil {
				t.Fatal("getLogOptions was not called")
			}
			if gotOptions.podName != tt.wantPod {
				t.Fatalf("podName = %q, want %q", gotOptions.podName, tt.wantPod)
			}
			if gotOptions.level != tt.wantLevel {
				t.Fatalf("level = %q, want %q", gotOptions.level, tt.wantLevel)
			}
			if _, err := logging.ParseLogLevel(gotOptions.level); err != nil {
				t.Fatalf("ParseLogLevel(%q) error = %v", gotOptions.level, err)
			}
		})
	}
}

func TestGetLogOptionsParsesTimelineFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "logs [pod-name]"}
	addLogFlags(cmd)
	cmd.SetArgs([]string{"test-pod", "--timeline"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}

	options, err := getLogOptions(cmd, []string{"test-pod"})
	if err != nil {
		t.Fatalf("getLogOptions() error = %v", err)
	}
	if !options.timeline {
		t.Fatal("timeline = false, want true")
	}
}

func TestGetLogOptionsRejectsInvalidLevel(t *testing.T) {
	cmd := &cobra.Command{Use: "logs [pod-name]"}
	addLogFlags(cmd)
	cmd.SetArgs([]string{"test-pod", "--level", "NOPE"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}

	options, err := getLogOptions(cmd, []string{"test-pod"})
	if err != nil {
		t.Fatalf("getLogOptions() error = %v", err)
	}
	if _, err := logging.ParseLogLevel(options.level); err == nil {
		t.Fatal("ParseLogLevel() error = nil, want error")
	}
}

func TestValidateLogOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    logOptions
		wantErr bool
	}{
		{"plain", logOptions{podName: "p"}, false},
		{"all-containers alone", logOptions{podName: "p", allContainers: true}, false},
		{"selector alone", logOptions{selector: "app=api"}, false},
		{"neither pod nor selector", logOptions{}, true},
		{"timeline+follow", logOptions{podName: "p", timeline: true, follow: true}, true},
		{"all-containers+container", logOptions{podName: "p", allContainers: true, container: "app"}, true},
		{"all-containers+timeline", logOptions{podName: "p", allContainers: true, timeline: true}, true},
		{"selector+timeline", logOptions{selector: "app=api", timeline: true}, true},
		{"selector+podname", logOptions{selector: "app=api", podName: "p"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLogOptions(&tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateLogOptions(%+v) error = %v, wantErr %v", tt.opts, err, tt.wantErr)
			}
		})
	}
}

func TestValidateLogOptionsAllNamespaces(t *testing.T) {
	if err := validateLogOptions(&logOptions{allNamespaces: true}); err == nil {
		t.Fatal("--all-namespaces without --selector should error")
	}
	if err := validateLogOptions(&logOptions{allNamespaces: true, selector: "app=api"}); err != nil {
		t.Fatalf("--all-namespaces with --selector should be valid, got %v", err)
	}
}
