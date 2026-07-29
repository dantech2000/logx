package cmd

import (
	"strings"
	"testing"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

func TestGetLogOptionsParsesLevelFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantPod   string
		wantLevel logging.LogLevel
	}{
		{
			name:      "root command short level flag",
			args:      []string{"test-pod", "-l", "WARN"},
			wantPod:   "test-pod",
			wantLevel: logging.WARN,
		},
		{
			name:      "logs subcommand long level flag",
			args:      []string{"logs", "test-pod", "--level", "ERROR"},
			wantPod:   "test-pod",
			wantLevel: logging.ERROR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotOptions *logOptions
			var gotLevel logging.LogLevel
			capture := func(cmd *cobra.Command, args []string) error {
				options, err := getLogOptions(cmd, args)
				if err != nil {
					return err
				}
				level, err := effectiveLogLevel(cmd)
				if err != nil {
					return err
				}
				gotOptions, gotLevel = options, level
				return nil
			}
			root := &cobra.Command{
				Use:  "logx [pod-name]",
				Args: cobra.MaximumNArgs(1),
				RunE: capture,
			}
			addLogFlags(root)
			logs := &cobra.Command{
				Use:  "logs [pod-name]",
				Args: cobra.ExactArgs(1),
				RunE: capture,
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
			if gotLevel != tt.wantLevel {
				t.Fatalf("level = %v, want %v", gotLevel, tt.wantLevel)
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

func TestEffectiveLogLevelRejectsInvalidLevel(t *testing.T) {
	cmd := &cobra.Command{Use: "logs [pod-name]"}
	addLogFlags(cmd)
	cmd.SetArgs([]string{"test-pod", "--level", "NOPE"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}

	if _, err := effectiveLogLevel(cmd); err == nil {
		t.Fatal("effectiveLogLevel() error = nil, want error")
	}
}

func TestValidateLogOptions(t *testing.T) {
	// The real command always supplies --max-concurrency (default 10); set a valid
	// value on every fixture so these flag-combination cases are realistic, then
	// exercise the max-concurrency bound explicitly below.
	const okConc = 10
	tests := []struct {
		name    string
		opts    logOptions
		wantErr bool
	}{
		{"plain", logOptions{podName: "p", maxConcurrency: okConc}, false},
		{"all-containers alone", logOptions{podName: "p", allContainers: true, maxConcurrency: okConc}, false},
		{"selector alone", logOptions{selector: "app=api", maxConcurrency: okConc}, false},
		{"stats+all-containers", logOptions{podName: "p", allContainers: true, stats: true, maxConcurrency: okConc}, false},
		{"stats+selector", logOptions{selector: "app=api", stats: true, maxConcurrency: okConc}, false},
		{"neither pod nor selector", logOptions{maxConcurrency: okConc}, true},
		{"timeline+follow", logOptions{podName: "p", timeline: true, follow: true, maxConcurrency: okConc}, true},
		{"all-containers+container", logOptions{podName: "p", allContainers: true, container: "app", maxConcurrency: okConc}, true},
		{"all-containers+timeline", logOptions{podName: "p", allContainers: true, timeline: true, maxConcurrency: okConc}, true},
		{"selector+timeline", logOptions{selector: "app=api", timeline: true, maxConcurrency: okConc}, true},
		{"selector+podname", logOptions{selector: "app=api", podName: "p", maxConcurrency: okConc}, true},
		{"stats+timeline", logOptions{podName: "p", stats: true, timeline: true, maxConcurrency: okConc}, true},
		{"max-concurrency zero", logOptions{podName: "p", maxConcurrency: 0}, true},
		{"max-concurrency negative", logOptions{podName: "p", maxConcurrency: -1}, true},
		{"max-concurrency one", logOptions{podName: "p", maxConcurrency: 1}, false},
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
	if err := validateLogOptions(&logOptions{allNamespaces: true, maxConcurrency: 10}); err == nil {
		t.Fatal("--all-namespaces without --selector should error")
	}
	if err := validateLogOptions(&logOptions{allNamespaces: true, selector: "app=api", maxConcurrency: 10}); err != nil {
		t.Fatalf("--all-namespaces with --selector should be valid, got %v", err)
	}
}

// TestLevelFlagHelpListsAllLevels pins that the shared --level help text names
// every level the parser accepts, guarding against the drift this flag once had
// (the logs help listed only four of the six levels).
func TestLevelFlagHelpListsAllLevels(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	addLevelFlag(cmd)

	flag := cmd.Flags().Lookup(flagLevel)
	if flag == nil {
		t.Fatal("--level flag not registered")
	}
	for _, name := range logging.LevelNames() {
		if !strings.Contains(flag.Usage, name) {
			t.Errorf("--level help %q missing level %s", flag.Usage, name)
		}
	}
}

// TestValidateLogOptionsRejectsUnsupportedCombinations pins the flag
// combinations that used to be accepted and then either failed with a raw API
// error or silently did nothing.
func TestValidateLogOptionsRejectsUnsupportedCombinations(t *testing.T) {
	tests := []struct {
		name string
		opts logOptions
	}{
		// -p targets one container's prior instance, but the fan-out paths never
		// run the -p precondition check, so every stream was stamped Previous:true
		// and any container that had not restarted failed the whole command.
		{"previous with all-containers", logOptions{podName: "p", previous: true, allContainers: true, maxConcurrency: 10}},
		{"previous with selector", logOptions{selector: "app=api", previous: true, maxConcurrency: 10}},
		// --timeline renders two record types in one fixed layout, so the flags
		// that replace that layout have nothing to act on. They used to be
		// accepted and silently ignored, exiting 0 with unfiltered output.
		{"timeline with fields", logOptions{podName: "p", timeline: true, fields: []string{"msg"}, maxConcurrency: 10}},
		{"timeline with json output", logOptions{podName: "p", timeline: true, output: "json", maxConcurrency: 10}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateLogOptions(&tc.opts); err == nil {
				t.Fatalf("validateLogOptions(%+v) accepted an unsupported combination", tc.opts)
			}
		})
	}

	// Combinations that must keep working.
	valid := []logOptions{
		{podName: "p", previous: true, container: "app", maxConcurrency: 10},
		{podName: "p", timeline: true, output: "text", maxConcurrency: 10},
		{podName: "p", timeline: true, maxConcurrency: 10},
		{podName: "p", allContainers: true, maxConcurrency: 10},
	}
	for _, opts := range valid {
		if err := validateLogOptions(&opts); err != nil {
			t.Errorf("validateLogOptions(%+v) rejected a valid combination: %v", opts, err)
		}
	}
}

// TestBuildPipelineOptionsRejectsStatsRenderingFlags pins that --stats rejects
// the flags that shape per-line rendering instead of ignoring them. `logx parse
// -o json --stats | jq` used to print the box-drawing text digest to stdout and
// exit 0, breaking the pipeline with no explanation.
func TestBuildPipelineOptionsRejectsStatsRenderingFlags(t *testing.T) {
	newCmd := func(args ...string) *cobra.Command {
		c := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		addFilterFlags(c)
		if err := c.ParseFlags(args); err != nil {
			t.Fatalf("ParseFlags(%v): %v", args, err)
		}
		return c
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"json output", []string{"--stats", "--output", "json"}},
		{"field projection", []string{"--stats", "--fields", "level,msg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildPipelineOptions(newCmd(tc.args...), logging.DEBUG); err == nil {
				t.Fatalf("buildPipelineOptions(%v) accepted a combination --stats cannot honor", tc.args)
			}
		})
	}

	// --stats on its own, and the rendering flags without --stats, still work.
	for _, args := range [][]string{{"--stats"}, {"--output", "json"}, {"--fields", "level"}} {
		if _, err := buildPipelineOptions(newCmd(args...), logging.DEBUG); err != nil {
			t.Errorf("buildPipelineOptions(%v) rejected a valid combination: %v", args, err)
		}
	}
}
