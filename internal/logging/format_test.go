package logging

import (
	"strings"
	"testing"
)

type ansiStringer struct{}

func (ansiStringer) String() string {
	return "\x1b[31mred\x1b[0m"
}

func TestFormatLogEntryDetailsDeterministicJSONFields(t *testing.T) {
	entry := ParseLogEntry(`{"message":"done","zeta":1,"alpha":"first","nested":{"z":2,"a":1},"level":"info","loglevel":"ignored","time":"2026-05-20T20:37:54Z","ts":1779309485}`)

	first := FormatLogEntryDetails(entry)
	for i := 0; i < 20; i++ {
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
		Fields: map[string]interface{}{
			"message": "batch complete",
			"ids":     []interface{}{float64(3), "two", true},
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
