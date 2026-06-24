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
