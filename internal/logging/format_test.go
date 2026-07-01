package logging

import (
	"strings"
	"testing"
	"time"
)

type ansiStringer struct{}

func (ansiStringer) String() string {
	return "\x1b[31mred\x1b[0m"
}

func TestFormatLogEntryDetailsDeterministicJSONFields(t *testing.T) {
	entry := ParseLogEntry(`{"message":"done","zeta":1,"alpha":"first","nested":{"z":2,"a":1},"level":"info","loglevel":"ignored","time":"2026-05-20T20:37:54Z","ts":1779309485}`)

	first := FormatLogEntryDetails(entry)
	for range 20 {
		if got := FormatLogEntryDetails(entry); got != first {
			t.Fatalf("FormatLogEntryDetails changed between runs: first=%q got=%q", first, got)
		}
	}

	assertInOrder(t, first, []string{
		"done",
		"alpha=first",
		"nested={a=1 z=2}",
		"zeta=1",
	})
	for _, excluded := range []string{"level=", "loglevel=", "time=", "ts="} {
		if strings.Contains(first, excluded) {
			t.Fatalf("formatted details include excluded field %q: %q", excluded, first)
		}
	}
}

func TestFormatLogEntryDetailsFormatsArraysAndNumbers(t *testing.T) {
	entry := LogEntry{
		Format: FormatJSON,
		Fields: map[string]any{
			"message": "batch complete",
			"ids":     []any{float64(3), "two", true},
			"count":   int64(7),
			"ratio":   float32(1.5),
		},
		RawLine: `{"message":"batch complete"}`,
	}

	got := FormatLogEntryDetails(entry)
	assertInOrder(t, got, []string{
		"batch complete",
		"count=7",
		"ids=[3 two true]",
		"ratio=1.50",
	})
}

func TestFormatLogEntryDetailsRendersLogfmtStructured(t *testing.T) {
	entry := ParseLogEntry(`time=2026-06-24T10:05:00Z level=info component=worker msg="job started" job_id=j-1`)
	if entry.Format != FormatLogfmt {
		t.Fatalf("Format = %v, want FormatLogfmt", entry.Format)
	}

	got := FormatLogEntryDetails(entry)
	// The reconstructed details must be the message + remaining fields, not the
	// raw line (which would duplicate the time/level logx already prints).
	assertInOrder(t, got, []string{"job started", "component=worker", "job_id=j-1"})
	for _, excluded := range []string{"time=", "level=", "msg=", "2026-06-24T10:05:00Z"} {
		if strings.Contains(got, excluded) {
			t.Fatalf("logfmt details leaked %q (raw line not reconstructed?): %q", excluded, got)
		}
	}
}

func TestFormatLogEntryDetailsLogfmtWithoutMessage(t *testing.T) {
	// A logfmt line with no msg field must render its fields once, not the raw
	// line followed by the fields again.
	entry := ParseLogEntry(`time=2026-06-24T10:00:00Z level=info component=db rows=5`)
	if entry.Format != FormatLogfmt {
		t.Fatalf("Format = %v, want FormatLogfmt", entry.Format)
	}

	got := FormatLogEntryDetails(entry)
	assertInOrder(t, got, []string{"component=db", "rows=5"})
	if strings.Count(got, "component=db") != 1 {
		t.Fatalf("fields duplicated: %q", got)
	}
	for _, leaked := range []string{"time=", "level=", "2026-06-24T10:00:00Z"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("raw line / excluded field leaked: %q", got)
		}
	}
}

func TestFormatLogEntryDoesNotDuplicateTimestampAndLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		// fragment that must appear exactly once (the message), and the leading
		// timestamp/level text that must NOT appear in the details.
		message string
		dupes   []string
	}{
		{
			name:    "bare timestamp + level",
			input:   "2026-06-24 10:06:00 INFO  Starting application v1.4.2",
			message: "Starting application v1.4.2",
			dupes:   []string{"2026-06-24 10:06:00", "INFO "},
		},
		{
			name:    "two-bracket form",
			input:   "[2026-06-24 10:00:00] [ERROR] disk full on /data",
			message: "disk full on /data",
			dupes:   []string{"[ERROR]", "[2026-06-24 10:00:00]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLogEntry(ParseLogEntry(tt.input))
			details := FormatLogEntryDetails(ParseLogEntry(tt.input))
			if !strings.Contains(got, tt.message) {
				t.Fatalf("output missing message %q: %q", tt.message, got)
			}
			for _, d := range tt.dupes {
				if strings.Contains(details, d) {
					t.Fatalf("details duplicated leading %q: %q", d, details)
				}
			}
		})
	}
}

func TestFormatLogEntryDetailsPreservesLargeIntegers(t *testing.T) {
	entry := ParseLogEntry(`{"level":"info","msg":"n","big":99999999999999999999,"id":17592186044416,"ratio":0.5,"sci":1.5e10}`)
	got := FormatLogEntryDetails(entry)
	assertInOrder(t, got, []string{
		"big=99999999999999999999", // beyond int64, kept verbatim, no ".00"
		"id=17592186044416",
		"ratio=0.50",
		"sci=15000000000.00",
	})
}

func TestFormatLogEntryNormalizesTimestampToUTC(t *testing.T) {
	// A timestamp in a non-UTC zone (as epoch values parse to local time) must be
	// displayed in UTC for consistency with the timeline output.
	loc := time.FixedZone("UTC+5", 5*3600)
	entry := LogEntry{Level: INFO, Timestamp: time.Date(2026, 6, 24, 15, 0, 0, 0, loc)}

	got := FormatLogEntry(entry)
	if !strings.Contains(got, "[2026-06-24 10:00:00]") {
		t.Fatalf("timestamp not normalized to UTC (15:00 +05:00 -> 10:00 UTC): %q", got)
	}
}

func TestFormatLogEntryDetailsRendersBracketedStructured(t *testing.T) {
	entry := ParseLogEntry(`[2026-06-24 10:07:00] [INFO] [api] request accepted method=GET status=200`)
	if entry.Format != FormatBracketed {
		t.Fatalf("Format = %v, want FormatBracketed", entry.Format)
	}

	got := FormatLogEntryDetails(entry)
	assertInOrder(t, got, []string{"request accepted", "method=GET", "status=200"})
	// No duplicated bracketed prefix in the reconstructed details.
	if strings.Contains(got, "[INFO]") || strings.Contains(got, "[api]") {
		t.Fatalf("bracketed details leaked the raw prefix: %q", got)
	}
}

func TestFormatLogEntryHandlesUnknownLevel(t *testing.T) {
	entry := LogEntry{
		Level:   LogLevel(99),
		Message: "custom level",
		Format:  FormatPlainText,
		RawLine: "custom level",
	}

	got := FormatLogEntry(entry)
	if !strings.Contains(got, "[UNKNOWN]") || !strings.Contains(got, "custom level") {
		t.Fatalf("FormatLogEntry() = %q, want unknown level and message", got)
	}
}

func TestFormatValueSanitizesFallbackValues(t *testing.T) {
	got := formatValue(ansiStringer{})
	if strings.Contains(got, "\x1b") {
		t.Fatalf("formatValue() did not sanitize fallback value: %q", got)
	}
}

func TestFormatProjectedEntry(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	entry := ParseLogEntry(`{"level":"error","msg":"db down","status":500,"time":"2026-06-24T10:00:00Z"}`)
	out := formatProjectedEntry(entry, classifyFields([]string{"level", "status", "msg", "missing"}))

	// Requested keys appear in order; the missing key is omitted.
	want := `level=ERROR status=500 msg="db down"`
	if out != want {
		t.Fatalf("formatProjectedEntry = %q, want %q", out, want)
	}
}

func TestFormatProjectedEntryTimestampVirtualKey(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	entry := ParseLogEntry(`{"msg":"hi","ts":"2026-06-24T10:00:00Z"}`)
	out := formatProjectedEntry(entry, classifyFields([]string{"ts", "msg"}))
	if out != `ts=2026-06-24 10:00:00 msg=hi` {
		t.Fatalf("formatProjectedEntry ts = %q", out)
	}
}
