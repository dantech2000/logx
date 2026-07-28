package logging

import (
	"encoding/json"
	"strings"
	"testing"
)

// levelOf is a helper for the classification regressions below.
func levelOf(t *testing.T, line string) LogEntry {
	t.Helper()
	return ParseLogEntry(line)
}

// TestPlainTextTraceKeywordDoesNotHideLine covers the worst failure mode a log
// viewer can have: silently hiding a line the user needed.
//
// plainTextLevelRegex matched level names case-insensitively, so the word
// "trace" appearing in ordinary prose — as it does in "stack trace follows" —
// classified the line as TRACE. TRACE sits below the default --level DEBUG, so
// the line was dropped from default output, and LevelTracker then propagated
// TRACE to the indented frames beneath it, taking the whole stack trace with it.
//
// Misreading prose as ERROR or WARN is harmless over-inclusion: the line is
// still shown. Misreading it as TRACE is the one direction that loses data, so
// TRACE alone requires the conventional uppercase spelling of a real level
// marker.
func TestPlainTextTraceKeywordDoesNotHideLine(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		wantLevel     LogLevel
		wantDetected  bool
		wantVisibleAt LogLevel
	}{
		{
			name:          "java stack trace header stays visible",
			line:          "Uncaught exception, stack trace follows:",
			wantLevel:     DEBUG,
			wantDetected:  false,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "trace mentioned in prose",
			line:          "Enabled request trace for tenant 42",
			wantLevel:     DEBUG,
			wantDetected:  false,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "trace id reference",
			line:          "see trace id abc123 for details",
			wantLevel:     DEBUG,
			wantDetected:  false,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "uppercase TRACE prose does not hide the line",
			line:          "java.lang.NullPointerException: STACK TRACE follows",
			wantLevel:     DEBUG,
			wantDetected:  false,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "uppercase TRACE mid-sentence",
			line:          "Blocked TRACE request from 10.0.0.1 to /admin - potential XST attack",
			wantLevel:     DEBUG,
			wantDetected:  false,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "a real level later on the line beats a leading prose TRACE",
			line:          "TRACE-ID: 9f2b request failed with ERROR: payment declined",
			wantLevel:     ERROR,
			wantDetected:  true,
			wantVisibleAt: DEBUG,
		},
		{
			name:          "TRACE as a leading marker is still a real level",
			line:          "TRACE entering handler",
			wantLevel:     TRACE,
			wantDetected:  true,
			wantVisibleAt: TRACE,
		},
		{
			name:          "TRACE marker after a timestamp is still a real level",
			line:          "2026-06-24 10:00:00 TRACE entering handler",
			wantLevel:     TRACE,
			wantDetected:  true,
			wantVisibleAt: TRACE,
		},
		{
			name:          "bracketed TRACE marker is still a real level",
			line:          "[TRACE] entering handler",
			wantLevel:     TRACE,
			wantDetected:  true,
			wantVisibleAt: TRACE,
		},
		{
			name:          "prose error still upgrades (over-inclusion is safe)",
			line:          "an error occurred while connecting",
			wantLevel:     ERROR,
			wantDetected:  true,
			wantVisibleAt: DEBUG,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := levelOf(t, tc.line)
			if entry.Level != tc.wantLevel {
				t.Errorf("level = %v, want %v", entry.Level, tc.wantLevel)
			}
			if entry.LevelDetected != tc.wantDetected {
				t.Errorf("LevelDetected = %v, want %v", entry.LevelDetected, tc.wantDetected)
			}
			if entry.Level < tc.wantVisibleAt {
				t.Errorf("line is hidden at --level %v (classified %v): %q",
					tc.wantVisibleAt, entry.Level, tc.line)
			}
		})
	}
}

// TestStackTraceSurvivesDefaultLevel is the end-to-end version of the above: the
// whole multi-line entry must reach default output, frames included.
func TestStackTraceSurvivesDefaultLevel(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	lines := []string{
		"Uncaught exception, stack trace follows:",
		"\tat com.example.Main.run(Main.java:42)",
		"\tat com.example.Main.main(Main.java:10)",
	}

	p := NewPipeline(PipelineOptions{MinLevel: DEBUG})
	kept := 0
	for _, line := range lines {
		if _, ok := p.ProcessLine(line); ok {
			kept++
		}
	}
	if kept != len(lines) {
		t.Fatalf("kept %d/%d stack-trace lines at --level DEBUG; a stack trace must not vanish by default", kept, len(lines))
	}
}

// TestXMLEntryWithoutLevelDefaultsToDebug pins the XML parser to the same
// undetected-level default as every other parser. It built its LogEntry without
// setting Level, so the zero value (TRACE) applied and any XML log line lacking
// an explicit level attribute disappeared from default output.
func TestXMLEntryWithoutLevelDefaultsToDebug(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantLevel LogLevel
	}{
		{
			name:      "no level attribute",
			line:      `<entry time="2026-06-24T10:00:00Z" thread="main">Started server on :8080</entry>`,
			wantLevel: DEBUG,
		},
		{
			name:      "no attributes at all",
			line:      `<message>Database migration complete</message>`,
			wantLevel: DEBUG,
		},
		{
			name:      "explicit level attribute still wins",
			line:      `<log level="ERROR">boom</log>`,
			wantLevel: ERROR,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := levelOf(t, tc.line)
			if entry.Level != tc.wantLevel {
				t.Fatalf("level = %v, want %v (entry would be %s at --level DEBUG)",
					entry.Level, tc.wantLevel,
					map[bool]string{true: "hidden", false: "visible"}[entry.Level < DEBUG])
			}
		})
	}
}

// TestLogfmtRejectsNonIdentifierKeys pins that the logfmt scanner validates its
// keys the way splitTrailingFields already does. Without it, any line whose
// whitespace-separated tokens all contain '=' was claimed by the logfmt parser —
// and since logfmt is tried before klog/syslog/access/xml/csv, a bogus match
// blocked the parser that would have read the line correctly, losing its level.
func TestLogfmtRejectsNonIdentifierKeys(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantFormat LogFormat
		wantLevel  LogLevel
	}{
		{
			name:       "csv row with an equals sign in the message",
			line:       "2026-06-24T10:00:00Z,ERROR,payment-svc,upstream_timeout&code=504",
			wantFormat: FormatPlainText,
			wantLevel:  ERROR,
		},
		{
			name:       "bare url with a query string is not logfmt",
			line:       "https://api.example.com/v1/items?limit=10",
			wantFormat: FormatPlainText,
			wantLevel:  DEBUG,
		},
		{
			name:       "genuine logfmt still parses",
			line:       `level=error msg="db down" service=api`,
			wantFormat: FormatLogfmt,
			wantLevel:  ERROR,
		},
		{
			name:       "dotted and hyphenated keys are valid logfmt",
			line:       `level=warn http.status_code=503 request-id=abc`,
			wantFormat: FormatLogfmt,
			wantLevel:  WARN,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := levelOf(t, tc.line)
			if entry.Format != tc.wantFormat {
				t.Errorf("format = %v, want %v (fields: %v)", entry.Format, tc.wantFormat, entry.Fields)
			}
			if entry.Level != tc.wantLevel {
				t.Errorf("level = %v, want %v", entry.Level, tc.wantLevel)
			}
		})
	}
}

// TestYAMLFlowNestedMapIsJSONEncodable pins that a flow-style YAML map with a
// nested map produces the same result as the byte-identical JSON form. yaml.v2
// decodes nested maps as map[interface{}]interface{}, which json.Marshal cannot
// encode, so MarshalEntryJSON silently fell back to "{}" and dropped the entire
// entry from --output json. The same normalization makes dot-path --where and
// --fields reach nested keys.
func TestYAMLFlowNestedMapIsJSONEncodable(t *testing.T) {
	const yamlLine = `{'level': 'error', 'msg': 'checkout failed', 'ctx': {'user': 'bob', 'cart': 3}}`
	entry := ParseLogEntry(yamlLine)

	out := MarshalEntryJSON(entry)
	if out == "{}" {
		t.Fatalf("nested YAML map produced an empty JSON entry: %q", out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON: %q", out)
	}
	for _, want := range []string{"checkout failed", "bob"} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q: %s", want, out)
		}
	}

	// The dot path into the nested map must resolve, as it does for JSON input.
	pred, err := ParseFieldPredicate("ctx.user==bob")
	if err != nil {
		t.Fatalf("ParseFieldPredicate: %v", err)
	}
	if !pred.Eval(entry) {
		t.Error("ctx.user==bob did not match a nested YAML-flow field")
	}

	// Go's map[...] rendering must not leak into text output.
	restoreColor(t)
	ApplyColorMode(ColorNever)
	if text := FormatLogEntry(entry); strings.Contains(text, "map[") {
		t.Errorf("text output leaked Go map syntax: %q", text)
	}
}

// TestJSONFieldLookupIsCaseInsensitive pins that loggers which capitalize their
// well-known keys are understood. Serilog and most .NET stacks emit "Level",
// "Msg", and "Timestamp"; jsonLevelFields carried "level" and "LEVEL" but not
// "Level", so the level was never detected and the entry kept the DEBUG default.
// That is data loss, not just a filtering annoyance: a Serilog ERROR line did not
// appear at --level ERROR, and --where Level==debug matched a line whose Level
// was error while --where Level==error matched nothing.
func TestJSONFieldLookupIsCaseInsensitive(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantLevel LogLevel
		wantMsg   string
	}{
		{
			name:      "serilog capitalized keys",
			line:      `{"Level":"error","Msg":"payment gateway down","svc":"pay"}`,
			wantLevel: ERROR,
			wantMsg:   "payment gateway down",
		},
		{
			name:      "dotnet Timestamp and Message",
			line:      `{"Level":"Warning","Message":"retrying","Timestamp":"2026-06-24T10:00:00Z"}`,
			wantLevel: WARN,
			wantMsg:   "retrying",
		},
		{
			name:      "screaming case",
			line:      `{"LEVEL":"fatal","MESSAGE":"kernel panic"}`,
			wantLevel: FATAL,
			wantMsg:   "kernel panic",
		},
		{
			name:      "ordinary lowercase still works",
			line:      `{"level":"info","msg":"hello"}`,
			wantLevel: INFO,
			wantMsg:   "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := ParseLogEntry(tc.line)
			if entry.Level != tc.wantLevel {
				t.Errorf("level = %v, want %v", entry.Level, tc.wantLevel)
			}
			if entry.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", entry.Message, tc.wantMsg)
			}
		})
	}

	// An exact match must win over a case variant, and the choice must not depend
	// on Go's randomized map iteration order. Run it repeatedly to catch flapping.
	const both = `{"level":"info","LEVEL":"error","msg":"x"}`
	for range 50 {
		if got := ParseLogEntry(both).Level; got != INFO {
			t.Fatalf("exact key must win over a case variant: got %v, want INFO", got)
		}
	}
}

// TestWhereCaseInsensitiveFieldAccess is the --where half of the above: the
// predicate must agree with what the parser detected.
func TestWhereCaseInsensitiveFieldAccess(t *testing.T) {
	const line = `{"Level":"error","Msg":"payment gateway down","Svc":"pay"}`

	cases := []struct {
		expr string
		want bool
	}{
		{"Level==error", true},
		{"Level==debug", false},
		{"level>=WARN", true},
		{"Svc==pay", true},
		{"svc==pay", true}, // lowercase probe finds the capitalized field
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalWhere(t, tc.expr, line); got != tc.want {
				t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestPlainTextTimestampComesFromLineHead pins two coupled defects in plain-text
// timestamp handling.
//
// The timestamp regex searched the whole line, so a date mentioned in the message
// became the entry's timestamp — "Scheduled next backup for 2026-12-25" was
// stamped six months in the future, which corrupts --timeline ordering, `ts`
// predicates, and the .time field of --output json. And stripLeadingMeta removed
// the timestamp before the level, so a "LEVEL <time> msg" line kept its timestamp
// in the message and printed it twice.
func TestPlainTextTimestampComesFromLineHead(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	tests := []struct {
		name    string
		line    string
		wantTS  string // "" means no timestamp should be detected
		wantMsg string
	}{
		{
			name:    "date in prose is content, not the entry time",
			line:    "INFO Scheduled next backup for 2026-12-25 03:00:00",
			wantTS:  "",
			wantMsg: "Scheduled next backup for 2026-12-25 03:00:00",
		},
		{
			name:    "date after prose words is content",
			line:    "ERROR retry after 2026-01-01 00:00:00 deadline",
			wantTS:  "",
			wantMsg: "retry after 2026-01-01 00:00:00 deadline",
		},
		{
			name:    "timestamp first",
			line:    "2026-06-24 10:00:00 INFO server started",
			wantTS:  "2026-06-24 10:00:00",
			wantMsg: "server started",
		},
		{
			name:    "level first then timestamp",
			line:    "INFO 2026-06-24 10:00:00 server started",
			wantTS:  "2026-06-24 10:00:00",
			wantMsg: "server started",
		},
		{
			name:    "level then bracketed timestamp",
			line:    "WARN [2026-06-24 10:00:01] disk almost full",
			wantTS:  "2026-06-24 10:00:01",
			wantMsg: "disk almost full",
		},
		{
			name:    "bracketed timestamp first",
			line:    "[2026-06-24 10:00:02] ERROR boom",
			wantTS:  "2026-06-24 10:00:02",
			wantMsg: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := ParseLogEntry(tc.line)

			if tc.wantTS == "" {
				if !entry.Timestamp.IsZero() {
					t.Errorf("a date in the message body became the entry timestamp: %v",
						entry.Timestamp.UTC().Format(DisplayTimeLayout))
				}
			} else {
				got := entry.Timestamp.UTC().Format(DisplayTimeLayout)
				if got != tc.wantTS {
					t.Errorf("timestamp = %q, want %q", got, tc.wantTS)
				}
			}

			if entry.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", entry.Message, tc.wantMsg)
			}

			// The rendered line must not show the timestamp twice.
			if tc.wantTS != "" {
				rendered := FormatLogEntry(entry)
				if strings.Count(rendered, tc.wantTS) > 1 {
					t.Errorf("timestamp rendered twice: %q", rendered)
				}
			}
		})
	}
}

// TestJSONLoggerLabelPrefersExplicitField pins that the bracketed label is the
// application's own logger name when the line carries one.
//
// detectLoggerLabel only guesses which logging *library* produced the object
// from its shape, and that guess was displayed unconditionally. Meanwhile
// --where resolves a real field ahead of the virtual key, so the two disagreed
// in the most confusing way available: a line with logger="payment-service"
// rendered as [logrus], `--where logger==logrus` matched nothing, and the value
// that did match never appeared on screen.
func TestJSONLoggerLabelPrefersExplicitField(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	tests := []struct {
		name       string
		line       string
		wantLogger string
	}{
		{"explicit logger field", `{"level":"error","msg":"db down","logger":"payment-service"}`, "payment-service"},
		{"component field", `{"level":"error","msg":"db down","component":"checkout"}`, "checkout"},
		{"source field", `{"level":"error","msg":"db down","source":"kafka"}`, "kafka"},
		{"falls back to the library guess", `{"level":"error","msg":"db down"}`, "logrus"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := ParseLogEntry(tc.line)
			if entry.Logger != tc.wantLogger {
				t.Fatalf("Logger = %q, want %q", entry.Logger, tc.wantLogger)
			}

			// What is displayed must be what --where matches.
			rendered := FormatLogEntry(entry)
			if !strings.Contains(rendered, "["+tc.wantLogger+"]") {
				t.Errorf("rendered %q does not show [%s]", rendered, tc.wantLogger)
			}
			pred, err := ParseFieldPredicate("logger==" + tc.wantLogger)
			if err != nil {
				t.Fatalf("ParseFieldPredicate: %v", err)
			}
			if !pred.Eval(entry) {
				t.Errorf("--where logger==%s does not match the label shown on screen", tc.wantLogger)
			}

			// The field that produced the label must not also print as a field.
			if strings.Count(rendered, tc.wantLogger) > 1 {
				t.Errorf("logger rendered twice: %q", rendered)
			}
		})
	}

	// The logfmt path renders no logger bracket, so its logger-ish fields must
	// stay visible as ordinary fields.
	logfmt := FormatLogEntry(ParseLogEntry(`level=warn msg="disk full" component=storage`))
	if !strings.Contains(logfmt, "component=storage") {
		t.Errorf("logfmt lost its component field: %q", logfmt)
	}
}
