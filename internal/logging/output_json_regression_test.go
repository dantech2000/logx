package logging

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// decodeJSONLine decodes one NDJSON record, failing on anything invalid.
func decodeJSONLine(t *testing.T, s string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	return obj
}

// TestJSONOutputKeepsUnstructuredMessage pins that --output json carries the
// content of a line with no structure. MarshalEntryJSON blanked Message whenever
// it equalled RawLine — but jsonEntry has no raw field, so the text was emitted
// nowhere and every unstructured line, the most common pod-log shape, became
// {"level":"DEBUG","format":"text"}. Access-log lines lost client, path, and
// status the same way, since that parser also sets Message to the whole line.
func TestJSONOutputKeepsUnstructuredMessage(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "plain unstructured line",
			line: "Segmentation fault in worker 3",
			want: "Segmentation fault in worker 3",
		},
		{
			name: "combined access log",
			line: `127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /admin HTTP/1.0" 500 2326`,
			want: "GET /admin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := decodeJSONLine(t, MarshalEntryJSON(ParseLogEntry(tc.line)))
			msg, _ := obj["message"].(string)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("message = %q, want it to contain %q (whole line: %v)", msg, tc.want, obj)
			}
		})
	}
}

// TestJSONOutputSanitizesBeyondJSONEscaping pins that NDJSON is terminal-safe.
// json.Marshal escapes only code points below 0x20, so DEL, the C1 controls, and
// bidi overrides passed through raw — and a UTF-8 terminal treats U+009B as CSI,
// a working escape introducer. NDJSON is read in a terminal as often as by jq.
func TestJSONOutputSanitizesBeyondJSONEscaping(t *testing.T) {
	// Written as escapes rather than literal characters: a raw RLO in source is
	// itself a Trojan-Source hazard, and control bytes are invisible in a diff.
	const hostile = "x\x7fy \u009b31m \u202ez"

	entry := ParseLogEntry(`{"level":"info","msg":"` + hostile + `","f\u202eield":"` + hostile + `"}`)
	out := MarshalEntryJSON(entry)

	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON: %q", out)
	}
	for _, bad := range []struct {
		name string
		r    rune
	}{
		{"DEL", 0x7f},
		{"C1 CSI", 0x9b},
		{"bidi RLO", 0x202e},
		{"ESC", 0x1b},
	} {
		if strings.ContainsRune(out, bad.r) {
			t.Errorf("JSON output leaked a raw %s (%#U): %q", bad.name, bad.r, out)
		}
	}
}

// TestJSONOutputHandlesNonFiniteNumbers pins that a value json.Marshal cannot
// encode does not take the whole entry with it. A YAML-flow line carrying `.nan`
// made Marshal fail, and marshalLine swallowed the error and returned "{}" —
// losing level, message, timestamp, and every field at once.
func TestJSONOutputHandlesNonFiniteNumbers(t *testing.T) {
	entry := ParseLogEntry(`{level: error, msg: "disk full", ratio: .nan}`)

	out := MarshalEntryJSON(entry)
	if out == "{}" {
		t.Fatal("a non-finite field erased the whole entry")
	}
	obj := decodeJSONLine(t, out)
	if obj["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", obj["level"])
	}
	if obj["message"] != "disk full" {
		t.Errorf("message = %v, want %q", obj["message"], "disk full")
	}

	// Directly exercise the value normalization for both NaN and infinities.
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		got := sanitizeJSONValue(v)
		if _, isString := got.(string); !isString {
			t.Errorf("sanitizeJSONValue(%v) = %v (%T), want a string form", v, got, got)
		}
	}
	if got := sanitizeJSONValue(float64(1.5)); got != 1.5 {
		t.Errorf("ordinary floats must pass through unchanged, got %v", got)
	}
}

// TestProjectionSkipsEntriesWithNoRequestedKeys pins that --fields naming a key
// no entry carries skips the entry instead of emitting a content-free record —
// a bare "{}" per line in JSON, or a blank line in text.
func TestProjectionSkipsEntriesWithNoRequestedKeys(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	for _, output := range []OutputFormat{OutputText, OutputJSON} {
		p := NewPipeline(PipelineOptions{
			MinLevel: TRACE,
			Fields:   []string{"user"},
			Output:   output,
		})
		if out, ok := p.ProcessLine("hello world"); ok {
			t.Errorf("output=%v: entry with no requested key was emitted as %q", output, out)
		}
	}

	// An entry that does carry the key is still emitted.
	p := NewPipeline(PipelineOptions{
		MinLevel: TRACE,
		Fields:   []string{"user"},
		Output:   OutputJSON,
	})
	out, ok := p.ProcessLine(`{"level":"info","user":"bob"}`)
	if !ok {
		t.Fatal("entry carrying the requested key must be emitted")
	}
	if obj := decodeJSONLine(t, out); obj["user"] != "bob" {
		t.Fatalf("user = %v, want bob", obj["user"])
	}
}

// TestMessageAliasSkipsEmptyValues pins that an empty first alias does not
// suppress a later one that has content. parseJSONMessage took the first alias
// merely present, and the remaining aliases were then excluded from the printed
// fields as already-surfaced — so {"message":"","msg":"..."} lost its content
// entirely. Log enrichers (Fluent Bit, Vector) produce exactly this shape when
// they add a normalized key alongside the original.
//
// It also covers a null value, which fmt rendered as the literal "<nil>" and
// presented as if it were the log's own text.
func TestMessageAliasSkipsEmptyValues(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	tests := []struct {
		name string
		line string
		want string
	}{
		{"empty message then msg", `{"level":"error","message":"","msg":"database connection refused"}`, "database connection refused"},
		{"empty msg then log", `{"level":"info","msg":"","log":"container started"}`, "container started"},
		{"null message then msg", `{"level":"info","message":null,"msg":"real message"}`, "real message"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := ParseLogEntry(tc.line)
			if entry.Message != tc.want {
				t.Errorf("Message = %q, want %q", entry.Message, tc.want)
			}
			// Text and JSON must agree; they resolved the message independently and
			// disagreed, with text rendering blank while JSON rendered correctly.
			if text := FormatLogEntry(entry); !strings.Contains(text, tc.want) {
				t.Errorf("text output %q missing %q", text, tc.want)
			}
			obj := decodeJSONLine(t, MarshalEntryJSON(entry))
			if obj["message"] != tc.want {
				t.Errorf("json message = %v, want %q", obj["message"], tc.want)
			}
		})
	}

	if got := stringValue(nil); got != "" {
		t.Errorf("stringValue(nil) = %q, want empty (must not fabricate <nil>)", got)
	}
}
