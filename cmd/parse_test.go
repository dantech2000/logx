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

func TestRunParseWhereAndFields(t *testing.T) {
	const sample = `{"level":"info","status":200,"path":"/ok","msg":"served"}
{"level":"error","status":503,"path":"/api","msg":"upstream down"}
{"level":"warn","status":404,"path":"/missing","msg":"not found"}
`
	// --where status>=500 keeps only the 503 line; --fields projects columns.
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(sample), &buf)
	cmd.SetArgs([]string{"--where", "status>=500", "--fields", "level,status,msg"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse --where --fields error = %v", err)
	}
	got := strings.TrimRight(buf.String(), "\n")
	if strings.Contains(got, "200") || strings.Contains(got, "404") {
		t.Fatalf("--where leaked non-matching rows:\n%s", got)
	}
	if got != `level=ERROR status=503 msg="upstream down"` {
		t.Fatalf("projected output = %q", got)
	}
}

func TestRunParseRejectsBadWhere(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader("INFO x\n"), &buf)
	cmd.SetArgs([]string{"--where", "noop"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a --where expression with no operator")
	}
}

func TestRunParseStats(t *testing.T) {
	const sample = "INFO ok\nERROR boom 1\nERROR boom 2\n"
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(sample), &buf)
	cmd.SetArgs([]string{"--stats"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse --stats error = %v", err)
	}
	out := buf.String()
	// Stats mode replaces the per-line output with a digest.
	if strings.Contains(out, "[INFO]") {
		t.Fatalf("--stats should suppress per-line output:\n%s", out)
	}
	for _, want := range []string{"logx stats", "lines: 3", "ERROR 2", "INFO 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats output missing %q:\n%s", want, out)
		}
	}
}

func TestRunParseRejectsBadOutputFormat(t *testing.T) {
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader("INFO x\n"), &buf)
	cmd.SetArgs([]string{"-o", "xml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an unknown --output format")
	}
}

func TestRunParseBinaryAndMalformedInputDoesNotError(t *testing.T) {
	// Garbage in must not crash or error the command; it should render safely.
	bad := "\x00\x01\x02 binary\n{\"level\":\"error\",\"msg\":\n\xff\xfe invalid utf8\n"
	var buf bytes.Buffer
	cmd := newParseCmd(strings.NewReader(bad), &buf)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parse of malformed input errored: %v", err)
	}
	if strings.ContainsRune(buf.String(), 0x1b) {
		t.Fatalf("malformed input leaked a raw ESC byte: %q", buf.String())
	}
}
