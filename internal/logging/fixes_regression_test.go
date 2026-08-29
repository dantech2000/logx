package logging

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Regression tests for the adversarial-review fixes. Each test pins one fix so
// a revert shows up as a named failure.

// TestWhereEqualsAcceptsDecimalLiteral pins that an equality predicate with a
// decimal literal ("404.0") matches an integral field value. The integer
// fast-path in FieldPredicate.equals used to return early and skip the float
// comparison, so `status==404.0` matched nothing at all.
func TestWhereEqualsAcceptsDecimalLiteral(t *testing.T) {
	pred, err := ParseFieldPredicate("status==404.0")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	entry := ParseLogEntry(`{"status": 404, "msg": "not found"}`)
	if !pred.Eval(entry) {
		t.Errorf("status==404.0 did not match field value 404")
	}

	neg, err := ParseFieldPredicate("status==404.5")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	if neg.Eval(entry) {
		t.Errorf("status==404.5 matched field value 404")
	}
}

// TestWhereEqualsKeepsIntegerPrecision pins that two integer literals are still
// compared as integers (not through float64), so a 19-digit span ID beyond the
// 53-bit mantissa does not collide with its neighbor.
func TestWhereEqualsKeepsIntegerPrecision(t *testing.T) {
	pred, err := ParseFieldPredicate("span_id==1234567890123456789")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	entry := ParseLogEntry(`{"span_id": 1234567890123456789, "msg": "x"}`)
	if !pred.Eval(entry) {
		t.Errorf("exact 19-digit integer equality failed")
	}
	other := ParseLogEntry(`{"span_id": 1234567890123456000, "msg": "x"}`)
	if pred.Eval(other) {
		t.Errorf("19-digit integer equality matched a float64-collapsed neighbor")
	}
}

// TestLogfmtUnescapeValues pins that quoted logfmt values collapse escaped
// backslashes and quotes while keeping every other escape verbatim. Dropping
// the backslash unconditionally corrupted Windows paths ("C:\temp" → "C:temp").
// Control-character escapes are deliberately left as text: decoding \n would
// smuggle a newline into what must stay a single output line, and a viewer is
// best served by showing the producer's bytes.
func TestLogfmtUnescapeValues(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantValue string
	}{
		{
			name:      "windows path keeps its backslashes",
			line:      `level=info msg="C:\temp\dir"`,
			wantValue: `C:\temp\dir`,
		},
		{
			name:      "escaped backslash collapses",
			line:      `level=info msg="a\\b"`,
			wantValue: `a\b`,
		},
		{
			name:      "escaped quote collapses",
			line:      `level=info msg="say \"hi\""`,
			wantValue: `say "hi"`,
		},
		{
			name:      "newline escape stays textual",
			line:      `level=info msg="line1\nline2"`,
			wantValue: `line1\nline2`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := levelOf(t, tc.line)
			got, ok := entry.Fields["msg"]
			if !ok {
				t.Fatalf("no msg field in %v", entry.Fields)
			}
			if got != tc.wantValue {
				t.Errorf("msg = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

// TestJSONRejectsTrailingContent pins that content after the closing brace is
// not silently swallowed. json.Decoder reads only the first value, so
// "{...}{...}" used to drop the second object entirely (its level and message
// with it) and "{...} garbage" parsed as if the garbage were not there.
func TestJSONRejectsTrailingContent(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantFormat LogFormat
		wantLevel  LogLevel
		wantMsg    string
	}{
		{
			name:       "second concatenated object is not dropped",
			line:       `{"msg":"two","level":"warn"}{"msg":"other","level":"error"}`,
			wantFormat: FormatPlainText,
			wantLevel:  WARN, // mid-line fallback scan finds WARN before ERROR
			wantMsg:    "",
		},
		{
			name:       "trailing garbage falls back to plain text",
			line:       `{"level":"info","msg":"hi"} trailing garbage}`,
			wantFormat: FormatPlainText,
			// The whole line becomes content; the fallback scan reads "info"
			// from it, which is the documented over-inclusion tradeoff.
			wantLevel: INFO,
		},
		{
			name:       "valid single object still parses as JSON",
			line:       `{"level":"info","msg":"hi"}`,
			wantFormat: FormatJSON,
			wantLevel:  INFO,
		},
		{
			name:       "trailing whitespace is allowed",
			line:       `{"level":"info","msg":"hi"}   `,
			wantFormat: FormatJSON,
			wantLevel:  INFO,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := levelOf(t, tc.line)
			if entry.Format != tc.wantFormat {
				t.Errorf("format = %v, want %v", entry.Format, tc.wantFormat)
			}
			if entry.Level != tc.wantLevel {
				t.Errorf("level = %v, want %v", entry.Level, tc.wantLevel)
			}
			if tc.wantMsg != "" && entry.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", entry.Message, tc.wantMsg)
			}
		})
	}
}

// TestCompactDateStringIsNotEpoch pins that a YYYYMMDD string timestamp is read
// as a date, not as epoch seconds. Interpreting "20260624" numerically always
// lands in 1970–1973, stamping the entry half a century into the past.
func TestCompactDateStringIsNotEpoch(t *testing.T) {
	entry := levelOf(t, `{"time":"20260624","msg":"compact date"}`)
	want := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	if !entry.Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", entry.Timestamp.UTC(), want)
	}

	// Real epoch values keep working, including ones of the same length that do
	// not form a valid calendar date.
	for _, tc := range []struct {
		value string
		want  time.Time
	}{
		{"1750759200", time.Unix(1750759200, 0).UTC()},
		{"20261399", time.Unix(20261399, 0).UTC()}, // month 13: not a date, is epoch
	} {
		entry := levelOf(t, `{"time":"`+tc.value+`","msg":"epoch"}`)
		if got := entry.Timestamp.UTC(); !got.Equal(tc.want) {
			t.Errorf("time %q: timestamp = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestDuplicateTrailingFieldsLastWins pins that repeated trailing fields resolve
// to their last occurrence, matching the logfmt parser's forward semantics.
// Backward iteration used to overwrite in reverse, so the first occurrence won.
func TestDuplicateTrailingFieldsLastWins(t *testing.T) {
	entry := levelOf(t, "[2026-06-24 10:00:00] [ERROR] [svc] request failed id=7 id=8")
	got, ok := entry.Fields["id"]
	if !ok {
		t.Fatalf("no id field in %v", entry.Fields)
	}
	if got != "8" {
		t.Errorf("id = %v, want 8", got)
	}
	if msg := entry.Message; strings.Contains(msg, "id=") {
		t.Errorf("message still carries field tokens: %q", msg)
	}
}

// TestXMLFormatLabel pins that XML entries carry FormatXML so --output json
// reports format "xml". They reused FormatLogfmt, mislabeling every XML line.
func TestXMLFormatLabel(t *testing.T) {
	entry := levelOf(t, `<log level="ERROR" ts="2026-06-24T10:00:00Z">boom</log>`)
	if entry.Format != FormatXML {
		t.Fatalf("format = %v, want %v", entry.Format, FormatXML)
	}
	var obj struct {
		Format string `json:"format"`
		Level  string `json:"level"`
	}
	if err := json.Unmarshal([]byte(MarshalEntryJSON(entry)), &obj); err != nil {
		t.Fatalf("MarshalEntryJSON: %v", err)
	}
	if obj.Format != "xml" {
		t.Errorf("json format label = %q, want %q", obj.Format, "xml")
	}
	if obj.Level != "ERROR" {
		t.Errorf("json level = %q, want ERROR", obj.Level)
	}
}

// TestYAMLFlowIntTimestamp pins that an integer epoch scalar in a flow-style
// YAML map produces a timestamp. yaml.v2 decodes integers as int, which the
// float64/json.Number/string switch used to skip entirely.
func TestYAMLFlowIntTimestamp(t *testing.T) {
	entry := levelOf(t, `{level: info, msg: hi, ts: 1750759200}`)
	want := time.Unix(1750759200, 0).UTC()
	if got := entry.Timestamp.UTC(); !got.Equal(want) {
		t.Errorf("timestamp = %v, want %v", got, want)
	}
}

// TestLeadingTimestampRequiresSeparator pins that a timestamp candidate glued
// to following text ("...Zerror") is neither extracted as the entry's time nor
// left behind in the message after the [timestamp] prefix was rendered from it.
func TestLeadingTimestampRequiresSeparator(t *testing.T) {
	line := "2026-06-24T10:00:00Zerror boom"
	entry := levelOf(t, line)
	if !entry.Timestamp.IsZero() {
		t.Errorf("glued timestamp was extracted: %v", entry.Timestamp)
	}
	if entry.Message != line {
		t.Errorf("message = %q, want the untouched line %q", entry.Message, line)
	}

	// The leading-level regex shares the terminated-timestamp shape, so the same
	// glued prefix is not read as <time><level> either: "Zerror" must not be
	// trusted as a marker (the fallback scan's alphanumeric guard rejects it,
	// and there is no other level token on the line).
	glued := levelOf(t, "2026-06-24T10:00:00Zerror something broke")
	if glued.LevelDetected {
		t.Errorf("glued prefix was classified as %v via a detected level", glued.Level)
	}
	if glued.Message != "2026-06-24T10:00:00Zerror something broke" {
		t.Errorf("glued message = %q", glued.Message)
	}

	// A properly separated timestamp is still extracted and stripped.
	ok := levelOf(t, "2026-06-24T10:00:00Z ERROR boom")
	want := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	if !ok.Timestamp.Equal(want) {
		t.Errorf("separated timestamp = %v, want %v", ok.Timestamp.UTC(), want)
	}
	if ok.Level != ERROR || !ok.LevelDetected {
		t.Errorf("level = %v (detected=%v), want ERROR", ok.Level, ok.LevelDetected)
	}
	if ok.Message != "boom" {
		t.Errorf("message = %q, want %q", ok.Message, "boom")
	}

	// Bracketed timestamps still work, with and without a leading level token.
	for _, line := range []string{
		"[2026-06-24 10:00:00] [ERROR] svc failed",
		"[2026-06-24 10:00:00] ERROR svc failed",
		"2026-06-24T10:00:00Z\tERROR boom",
	} {
		bracketed := levelOf(t, line)
		if !bracketed.Timestamp.Equal(want) {
			t.Errorf("%q: timestamp = %v, want %v", line, bracketed.Timestamp.UTC(), want)
		}
		if bracketed.Level != ERROR {
			t.Errorf("%q: level = %v, want ERROR", line, bracketed.Level)
		}
	}
}
