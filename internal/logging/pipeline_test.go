package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestPipelineProcessLineEmitsAndDrops(t *testing.T) {
	tests := []struct {
		name     string
		minLevel LogLevel
		line     string
		wantOK   bool
	}{
		{"blank dropped", DEBUG, "   \t ", false},
		{"empty dropped", DEBUG, "", false},
		{"info kept at info", INFO, "INFO ready", true},
		{"debug dropped at info", INFO, "DEBUG noisy", false},
		{"trace dropped at default debug", DEBUG, "TRACE chatty", false},
		{"trace kept at trace", TRACE, "TRACE chatty", true},
		{"fatal kept at error", ERROR, "FATAL boom", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPipeline(PipelineOptions{MinLevel: tt.minLevel})
			out, ok := p.ProcessLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ProcessLine(%q) ok = %v, want %v (out=%q)", tt.line, ok, tt.wantOK, out)
			}
			if ok && out == "" {
				t.Fatalf("ProcessLine(%q) emitted an empty line", tt.line)
			}
		})
	}
}

// A multi-line entry (error header + indented stack frames) must be kept or
// dropped as a unit: at --level ERROR the indented frames inherit ERROR and are
// shown; at the same level a following independent INFO line is dropped.
func TestPipelineGroupsMultiLineEntry(t *testing.T) {
	p := NewPipeline(PipelineOptions{MinLevel: ERROR})
	lines := []string{
		"ERROR boom",
		"  at foo.go:10",
		"  at bar.go:20",
		"INFO recovered",
	}
	var kept []string
	for _, l := range lines {
		if out, ok := p.ProcessLine(l); ok {
			kept = append(kept, out)
		}
	}
	if len(kept) != 3 {
		t.Fatalf("kept %d lines, want 3 (error header + 2 frames): %q", len(kept), kept)
	}
	if !strings.Contains(kept[1], "foo.go:10") || !strings.Contains(kept[2], "bar.go:20") {
		t.Fatalf("indented frames were not carried with their parent: %q", kept)
	}
}

func TestPipelineRunMatchesFilterAndFormat(t *testing.T) {
	input := "DEBUG a\nINFO b\nWARN c\n"
	var viaPipeline, viaWrapper bytes.Buffer
	if err := NewPipeline(PipelineOptions{MinLevel: INFO}).Run(strings.NewReader(input), &viaPipeline); err != nil {
		t.Fatalf("Pipeline.Run error = %v", err)
	}
	if err := FilterAndFormatLogs(strings.NewReader(input), &viaWrapper, INFO); err != nil {
		t.Fatalf("FilterAndFormatLogs error = %v", err)
	}
	if viaPipeline.String() != viaWrapper.String() {
		t.Fatalf("Pipeline.Run and FilterAndFormatLogs diverged:\n pipeline=%q\n wrapper =%q", viaPipeline.String(), viaWrapper.String())
	}
	if n := strings.Count(viaPipeline.String(), "\n"); n != 2 {
		t.Fatalf("got %d output lines, want 2 (INFO+WARN): %q", n, viaPipeline.String())
	}
}

func TestPipelineHonorsKubeletTimestampPrefix(t *testing.T) {
	out, ok := NewPipeline(PipelineOptions{MinLevel: DEBUG}).
		ProcessLine("2024-03-15T12:19:57Z DEBUG hello")
	if !ok {
		t.Fatal("expected line to be emitted")
	}
	if !strings.Contains(out, "[2024-03-15 12:19:57]") || !strings.Contains(out, "hello") {
		t.Fatalf("kubelet timestamp prefix not applied: %q", out)
	}
}
