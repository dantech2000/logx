package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newParseCmd(in *strings.Reader, out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "parse [file]",
		Args:          cobra.MaximumNArgs(1),
		RunE:          runParse,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.Flags().StringP(flagLevel, "l", "DEBUG", "")
	addFilterFlags(cmd)
	if in != nil {
		cmd.SetIn(in)
	}
	cmd.SetOut(out)
	cmd.SetErr(out)
	return cmd
}

const parseSample = `{"level":"info","msg":"started","time":"2026-06-24T10:00:00Z"}
2026-06-24 10:00:01 ERROR boom
	at frame.go:10
`

func TestRunParseFromStdin(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(parseSample), &buf)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"started", "boom", "at frame.go:10"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %q", want, out)
		}
	}
}

func TestRunParseFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.log")
	if err := os.WriteFile(path, []byte(parseSample), 0o600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}

	var buf bytes.Buffer
	cmd := newParseCmd(nil, &buf)
	cmd.SetArgs([]string{path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse error = %v", err)
	}
	if !strings.Contains(buf.String(), "started") {
		t.Fatalf("file output missing content: %q", buf.String())
	}
}

func TestRunParseLevelFilterGroupsContinuation(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(parseSample), &buf)
	cmd.SetArgs([]string{"--level", "ERROR"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse error = %v", err)
	}
	out := buf.String()
	// The INFO line is filtered out; the ERROR line and its indented
	// continuation frame survive.
	if strings.Contains(out, "started") {
		t.Fatalf("INFO line should be filtered at ERROR: %q", out)
	}
	for _, want := range []string{"boom", "at frame.go:10"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q at ERROR: %q", want, out)
		}
	}
}

func TestRunParseInvalidLevel(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(""), &buf)
	cmd.SetArgs([]string{"--level", "NOPE"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("parse with invalid level = nil error, want error")
	}
}

func TestRunParseMissingFile(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(nil, &buf)
	cmd.SetArgs([]string{filepath.Join(t.TempDir(), "does-not-exist.log")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("parse of missing file = nil error, want error")
	}
}

func TestRunParseGrepAndExclude(t *testing.T) {
	const sample = "INFO order placed\nINFO user login\nWARN order cancelled\nINFO healthz ok\n"

	// --grep keeps only matching lines.
	var grepBuf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(sample), &grepBuf)
	cmd.SetArgs([]string{"--grep", "order"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse --grep error = %v", err)
	}
	if strings.Contains(grepBuf.String(), "login") || strings.Contains(grepBuf.String(), "healthz") {
		t.Fatalf("--grep leaked non-matching lines:\n%s", grepBuf.String())
	}
	if !strings.Contains(grepBuf.String(), "order placed") || !strings.Contains(grepBuf.String(), "order cancelled") {
		t.Fatalf("--grep dropped matching lines:\n%s", grepBuf.String())
	}

	// --grep + --exclude narrows further.
	var bothBuf bytes.Buffer
	cmd = newParseCmd(strings.NewReader(sample), &bothBuf)
	cmd.SetArgs([]string{"--grep", "order", "--exclude", "cancelled"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse --grep --exclude error = %v", err)
	}
	if strings.Contains(bothBuf.String(), "cancelled") {
		t.Fatalf("--exclude failed to drop line:\n%s", bothBuf.String())
	}
	if !strings.Contains(bothBuf.String(), "order placed") {
		t.Fatalf("--exclude dropped a wanted line:\n%s", bothBuf.String())
	}
}

func TestRunParseRejectsBadRegex(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader("INFO x\n"), &buf)
	cmd.SetArgs([]string{"--grep", "("})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an invalid --grep regex")
	}
}
