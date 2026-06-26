package logging

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// adversarialLines is a grab-bag of malformed, hostile, and degenerate inputs:
// truncated structured logs, control/escape bytes, invalid UTF-8, oversized
// tokens, and prose that flirts with each format's trigger characters.
var adversarialLines = []string{
	"",
	"   ",
	"\x00\x01\x02 binary prefix then text",
	"plain line with an \x1b[31m embedded ANSI escape",
	"invalid utf8: \xff\xfe\xfa end",
	`{"level":"error","msg":`,                             // truncated JSON
	`{"level":"error","msg":"ok","nested":{"a":{"b":[1,2`, // truncated nested
	`{"level":12345678901234567890123,"msg":"big"}`,       // huge number
	`{"level":null,"msg":null,"arr":[null,true,{"x":1}]}`,
	`{level: [not, a, scalar], msg: x}`, // YAML-ish with weird types
	`{: : :}`,
	"<log level=\"ERROR\"><nested>oops</nested></log>", // XML with inner markup
	"<log level=\"\x1b[31m\">escape in attr</log>",
	",,,,,,,,",                      // all commas
	"2026-06-24T10:00:00Z,,,,",      // timestamp then empty CSV fields
	strings.Repeat("A", 2_000_000),  // over the 1MB line cap
	strings.Repeat("k=v ", 100_000), // pathological logfmt
	"level=ERROR " + strings.Repeat("x", 50_000),
	"status=999999999999999999999999999",
	"\t\t\t  indented only",
	"🔥💥 emoji and 日本語 unicode ok",
}

// runAllFeatures pushes a line through a pipeline with every feature stacked on,
// returning the rendered output (if emitted).
func runAllFeatures(t *testing.T, opts PipelineOptions, line string) (string, bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on input %q: %v", line, r)
		}
	}()
	return NewPipeline(opts).ProcessLine(line)
}

func TestPipelineSurvivesAdversarialInputText(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever) // keep highlight from emitting escapes we'd flag

	opts := PipelineOptions{
		MinLevel:  TRACE,
		Include:   []*regexp.Regexp{regexp.MustCompile(`(?i)error|x`)},
		Exclude:   []*regexp.Regexp{regexp.MustCompile(`nope`)},
		Where:     []FieldPredicate{mustPredicate(t, "level>=DEBUG")},
		Highlight: true,
	}
	for _, line := range adversarialLines {
		out, ok := runAllFeatures(t, opts, line)
		if !ok {
			continue
		}
		if !utf8.ValidString(out) {
			t.Fatalf("text output is not valid UTF-8 for input %q", line)
		}
		if strings.ContainsRune(out, 0x1b) {
			t.Fatalf("text output leaked a raw ESC byte for input %q: %q", line, out)
		}
	}
}

func TestPipelineSurvivesAdversarialInputJSON(t *testing.T) {
	opts := PipelineOptions{
		MinLevel: TRACE,
		Output:   OutputJSON,
		Where:    []FieldPredicate{mustPredicate(t, "level>=TRACE")},
	}
	for _, line := range adversarialLines {
		out, ok := runAllFeatures(t, opts, line)
		if !ok {
			continue
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("JSON output is not valid for input %q: %q", line, out)
		}
	}
}

func TestPipelineSurvivesAdversarialInputProjection(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	opts := PipelineOptions{MinLevel: TRACE, Fields: []string{"ts", "level", "status", "msg", "nested", "arr"}}
	for _, line := range adversarialLines {
		// The assertion is simply that projection rendering never panics or hangs
		// on hostile input; runAllFeatures fails the test on a panic.
		runAllFeatures(t, opts, line)
	}
}

func TestStatsSurvivesAdversarialInput(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	s := NewStats()
	for _, line := range adversarialLines {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("stats panicked on %q: %v", line, r)
				}
			}()
			s.Record(ParseLogEntry(line))
		}()
	}
	var b strings.Builder
	if err := s.Write(&b); err != nil {
		t.Fatalf("stats Write error: %v", err)
	}
	if !utf8.ValidString(b.String()) {
		t.Fatal("stats summary is not valid UTF-8")
	}
}

// FuzzPipelineAllFeatures stacks grep, where, projection-free text and JSON
// rendering over arbitrary input and asserts the output stays terminal-safe
// (text) or valid JSON, and that nothing panics.
func FuzzPipelineAllFeatures(f *testing.F) {
	for _, l := range adversarialLines {
		f.Add(l, true)
		f.Add(l, false)
	}
	f.Add("{\"level\":\"error\",\"status\":500}", false)
	f.Fuzz(func(t *testing.T, data string, asJSON bool) {
		ApplyColorMode(ColorNever)
		opts := PipelineOptions{
			MinLevel: TRACE,
			Include:  []*regexp.Regexp{regexp.MustCompile(`.`)},
			Where:    []FieldPredicate{{key: "level", op: opGte, val: "TRACE"}},
		}
		if asJSON {
			opts.Output = OutputJSON
		}
		// One line at a time (the scanner guarantees no embedded newlines).
		for _, line := range strings.Split(data, "\n") {
			out, ok := NewPipeline(opts).ProcessLine(line)
			if !ok {
				continue
			}
			if asJSON {
				if !json.Valid([]byte(out)) {
					t.Fatalf("invalid JSON for %q: %q", line, out)
				}
				continue
			}
			if !utf8.ValidString(out) {
				t.Fatalf("invalid UTF-8 for %q", line)
			}
			if strings.ContainsRune(out, 0x1b) {
				t.Fatalf("raw ESC leaked for %q: %q", line, out)
			}
		}
	})
}

// FuzzParseFieldPredicate ensures the --where parser never panics on arbitrary
// expressions, and that any predicate it accepts can be evaluated safely.
func FuzzParseFieldPredicate(f *testing.F) {
	for _, seed := range []string{
		"status>=500", "level==ERROR", "a~=(", "", ">=", "x", "a=b=c",
		"path~=/v2>=1", "  msg != hi ", "n<=", "level~=.*",
	} {
		f.Add(seed)
	}
	sample := ParseLogEntry(`{"level":"warn","status":404,"path":"/x","msg":"nope"}`)
	f.Fuzz(func(t *testing.T, expr string) {
		fp, err := ParseFieldPredicate(expr)
		if err != nil {
			return // rejecting bad input is the expected outcome
		}
		_ = fp.Eval(sample) // must not panic
		_ = fp.Eval(ParseLogEntry("plain text line"))
	})
}
