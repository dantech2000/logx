package logging

import (
	"regexp"
	"strings"
	"testing"
)

// drain runs every line through a fresh pipeline and returns the kept outputs.
func drain(t *testing.T, opts PipelineOptions, lines []string) []string {
	t.Helper()
	restoreColor(t)
	ApplyColorMode(ColorNever)
	p := NewPipeline(opts)
	var kept []string
	for _, line := range lines {
		if out, ok := p.ProcessLine(line); ok {
			kept = append(kept, out)
		}
	}
	return kept
}

var traceCorpus = []string{
	`{"level":"info","msg":"request handled"}`,
	`{"level":"error","msg":"payment failed"}`,
	"Traceback (most recent call last):",
	`  File "/app/pay.py", line 42, in charge`,
	"PaymentError: insufficient funds",
	`{"level":"info","msg":"shutdown complete"}`,
}

// A --grep matching the anchor keeps the whole entry: header, frames, footer.
// Before group-aware filtering only the parent and the frames literally
// containing the pattern survived.
func TestPipelineGrepKeepsWholeGroup(t *testing.T) {
	kept := drain(t, PipelineOptions{
		MinLevel: TRACE,
		Include:  []*regexp.Regexp{regexp.MustCompile("payment failed")},
	}, traceCorpus)
	if len(kept) != 4 {
		t.Fatalf("grep kept %d lines, want the 4-line entry:\n%s", len(kept), strings.Join(kept, "\n"))
	}
}

// A --grep matching only the footer still rescues that line, as before —
// group awareness adds lines, it never removes a direct match.
func TestPipelineGrepStillRescuesFooterMatch(t *testing.T) {
	kept := drain(t, PipelineOptions{
		MinLevel: TRACE,
		Include:  []*regexp.Regexp{regexp.MustCompile("PaymentError")},
	}, traceCorpus)
	if len(kept) != 1 || !strings.Contains(kept[0], "PaymentError") {
		t.Fatalf("footer-only grep kept %q, want just the footer", kept)
	}
}

// An --exclude matching the anchor hides the whole entry; an --exclude
// matching only one frame hides just that frame.
func TestPipelineExcludeHidesGroups(t *testing.T) {
	kept := drain(t, PipelineOptions{
		MinLevel: TRACE,
		Exclude:  []*regexp.Regexp{regexp.MustCompile("payment failed")},
	}, traceCorpus)
	for _, out := range kept {
		for _, hidden := range []string{"payment failed", "Traceback", "pay.py", "PaymentError"} {
			if strings.Contains(out, hidden) {
				t.Fatalf("excluded entry leaked %q in %q", hidden, out)
			}
		}
	}
	if len(kept) != 2 {
		t.Fatalf("exclude kept %d lines, want the two info lines", len(kept))
	}

	frameOnly := drain(t, PipelineOptions{
		MinLevel: TRACE,
		Exclude:  []*regexp.Regexp{regexp.MustCompile("pay.py")},
	}, traceCorpus)
	if len(frameOnly) != len(traceCorpus)-1 {
		t.Fatalf("frame exclude kept %d lines, want all but the frame", len(frameOnly))
	}
}

// Grep and exclude with the same pattern partition every line exactly: the
// group join rules are mirror images by construction. Markers stand in for
// corpus lines because rendering reformats structured lines.
func TestPipelineGrepExcludePartitionGroups(t *testing.T) {
	pattern := regexp.MustCompile("payment|Traceback")
	inc := drain(t, PipelineOptions{MinLevel: TRACE, Include: []*regexp.Regexp{pattern}}, traceCorpus)
	exc := drain(t, PipelineOptions{MinLevel: TRACE, Exclude: []*regexp.Regexp{pattern}}, traceCorpus)
	// Each marker is unique to one corpus line.
	markers := []string{"request handled", "payment failed", "Traceback", "pay.py", "PaymentError", "shutdown complete"}
	onSide := func(kept []string, marker string) bool {
		for _, out := range kept {
			if strings.Contains(out, marker) {
				return true
			}
		}
		return false
	}
	joined := func(kept []string) string { return strings.Join(kept, "\n") }
	for _, marker := range markers {
		if onSide(inc, marker) == onSide(exc, marker) {
			t.Errorf("marker %q: grep-kept=%v exclude-kept=%v; must partition\ngrep:\n%s\nexclude:\n%s",
				marker, onSide(inc, marker), onSide(exc, marker), joined(inc), joined(exc))
		}
	}
}

// Field predicates evaluate against the anchor's entry, so a predicate that
// keeps an error keeps its field-less frames too instead of shredding them.
func TestPipelineWhereKeepsWholeGroup(t *testing.T) {
	pred, err := ParseFieldPredicate("status>=500")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	kept := drain(t, PipelineOptions{MinLevel: TRACE, Where: []FieldPredicate{pred}}, []string{
		`{"level":"info","msg":"ok","status":200}`,
		`{"level":"error","msg":"boom","status":500}`,
		"  at fail.go:1",
		`{"level":"info","msg":"later","status":200}`,
	})
	if len(kept) != 2 {
		t.Fatalf("where kept %d lines, want the error entry whole: %q", len(kept), kept)
	}
	if !strings.Contains(kept[0], "boom") || !strings.Contains(kept[1], "fail.go") {
		t.Fatalf("where split the entry: %q", kept)
	}
}

// An entry whose anchor fails the predicate stays hidden as a unit, frames
// and all.
func TestPipelineWhereDropsWholeGroup(t *testing.T) {
	pred, err := ParseFieldPredicate("status>=500")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	kept := drain(t, PipelineOptions{MinLevel: TRACE, Where: []FieldPredicate{pred}}, []string{
		`{"level":"error","msg":"no status here"}`,
		"  at fail.go:1",
		`{"level":"error","msg":"loud","status":500}`,
	})
	for _, out := range kept {
		if strings.Contains(out, "no status") || strings.Contains(out, "fail.go") {
			t.Fatalf("predicate-free entry leaked: %q", out)
		}
	}
	if len(kept) != 1 {
		t.Fatalf("where kept %d lines, want just the status=500 entry", len(kept))
	}
}

// --tail counts entries after filtering: with a WARN floor over mixed lines,
// tail 1 yields the last matching entry (whole, with frames), not the last
// raw line and not an empty page.
func TestPipelineTailWindowsAfterFiltering(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	p := NewPipeline(PipelineOptions{MinLevel: WARN})
	p.EnableTail(1)
	lines := []string{
		"INFO early noise",
		"WARN first warning",
		"ERROR late failure",
		"  at fail.go:1",
	}
	for _, line := range lines {
		if _, ok := p.ProcessLine(line); ok {
			t.Fatalf("windowed pipeline emitted %q before flush", line)
		}
	}
	flushed := p.FlushRetained()
	if len(flushed) != 2 {
		t.Fatalf("tail window holds %d lines, want the 2-line error entry: %q", len(flushed), flushed)
	}
	if !strings.Contains(flushed[0], "late failure") || !strings.Contains(flushed[1], "fail.go") {
		t.Fatalf("tail window split the entry: %q", flushed)
	}
}

// --stats over a tail window digests exactly the window shown.
func TestPipelineTailStatsCoverWindowOnly(t *testing.T) {
	p := NewPipeline(PipelineOptions{MinLevel: TRACE, CollectStats: true})
	p.EnableTail(1)
	for _, line := range []string{"INFO a", "INFO b", "INFO c"} {
		p.ProcessLine(line)
	}
	// Windowed stats record at flush time, over exactly the retained window.
	p.FlushRetained()
	if got := p.Stats().Total(); got != 1 {
		t.Fatalf("windowed stats counted %d, want 1", got)
	}
	if lines := p.FlushRetained(); len(lines) != 0 {
		t.Fatalf("stats flush emitted lines: %q", lines)
	}
}
