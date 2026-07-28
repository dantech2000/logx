package logging

import "testing"

// restoreFieldAliases snapshots and restores the parser's field-name globals so
// a registration test doesn't leak into others.
func restoreFieldAliases(t *testing.T) {
	t.Helper()
	level := append([]string(nil), jsonLevelFields...)
	message := append([]string(nil), jsonMessageFields...)
	timeF := append([]string(nil), jsonTimeFields...)
	excl := jsonFormattedFieldExclusions
	// jsonMessageFieldSet is rebuilt by RegisterFieldAliases too. Omitting it here
	// leaked registered message aliases into every later test in the package,
	// which showed up as a nondeterministic failure elsewhere.
	msgSet := jsonMessageFieldSet
	lfLevel := append([]string(nil), logfmtLevelFields...)
	lfMessage := append([]string(nil), logfmtMessageFields...)
	lfTime := append([]string(nil), logfmtTimeFields...)
	t.Cleanup(func() {
		jsonLevelFields = level
		jsonMessageFields = message
		jsonTimeFields = timeF
		jsonFormattedFieldExclusions = excl
		jsonMessageFieldSet = msgSet
		logfmtLevelFields = lfLevel
		logfmtMessageFields = lfMessage
		logfmtTimeFields = lfTime
	})
}

func TestRegisterFieldAliases(t *testing.T) {
	restoreFieldAliases(t)
	RegisterFieldAliases([]string{"lvl_custom"}, []string{"text_body"}, []string{"event_time"})

	entry := ParseLogEntry(`{"lvl_custom":"error","text_body":"db down","event_time":"2026-06-24T10:00:00Z"}`)
	if entry.Level != ERROR || !entry.LevelDetected {
		t.Errorf("custom level field not recognized: level=%v detected=%v", entry.Level, entry.LevelDetected)
	}
	if entry.Message != "db down" {
		t.Errorf("custom message field not recognized: %q", entry.Message)
	}
	if entry.Timestamp.IsZero() {
		t.Error("custom timestamp field not recognized")
	}

	// The custom level/timestamp keys must not also render as ordinary fields.
	restoreColor(t)
	ApplyColorMode(ColorNever)
	out := FormatLogEntry(entry)
	if got := countOccurrences(out, "lvl_custom="); got != 0 {
		t.Errorf("custom level key leaked as a field: %q", out)
	}
}

func TestRegisterFieldAliasesAppliesToLogfmt(t *testing.T) {
	restoreFieldAliases(t)
	RegisterFieldAliases([]string{"lvl_custom"}, []string{"text_body"}, []string{"event_time"})

	// A logfmt line using the custom keys must pick up level, message, and
	// timestamp from them, not just JSON lines.
	entry := ParseLogEntry(`lvl_custom=error text_body="db down" event_time=2026-06-24T10:00:00Z svc=api`)
	if entry.Format != FormatLogfmt {
		t.Fatalf("expected logfmt format, got %v", entry.Format)
	}
	if entry.Level != ERROR || !entry.LevelDetected {
		t.Errorf("custom logfmt level field not recognized: level=%v detected=%v", entry.Level, entry.LevelDetected)
	}
	if entry.Message != "db down" {
		t.Errorf("custom logfmt message field not recognized: %q", entry.Message)
	}
	if entry.Timestamp.IsZero() {
		t.Error("custom logfmt timestamp field not recognized")
	}

	// The custom level/timestamp keys must not also render as ordinary fields,
	// while a genuine field (svc) still shows.
	restoreColor(t)
	ApplyColorMode(ColorNever)
	out := FormatLogEntry(entry)
	if got := countOccurrences(out, "lvl_custom="); got != 0 {
		t.Errorf("custom logfmt level key leaked as a field: %q", out)
	}
	if got := countOccurrences(out, "event_time="); got != 0 {
		t.Errorf("custom logfmt timestamp key leaked as a field: %q", out)
	}
	if got := countOccurrences(out, "svc=api"); got != 1 {
		t.Errorf("genuine field dropped: %q", out)
	}
}

// TestLogfmtBuiltinLevelKeysUnchanged guards that wiring logfmt through the alias
// lists did not drop a built-in logfmt-only key ("lvl") that JSON never had.
func TestLogfmtBuiltinLevelKeysUnchanged(t *testing.T) {
	entry := ParseLogEntry(`lvl=warn msg="disk almost full"`)
	if entry.Format != FormatLogfmt {
		t.Fatalf("expected logfmt format, got %v", entry.Format)
	}
	if entry.Level != WARN || !entry.LevelDetected {
		t.Errorf("built-in logfmt lvl key not recognized: level=%v detected=%v", entry.Level, entry.LevelDetected)
	}
	if entry.Message != "disk almost full" {
		t.Errorf("logfmt message not parsed: %q", entry.Message)
	}
}

func TestRegisterFieldAliasesIsIdempotent(t *testing.T) {
	restoreFieldAliases(t)
	before := len(jsonLevelFields)
	RegisterFieldAliases([]string{"level"}, nil, nil) // "level" already present
	RegisterFieldAliases([]string{""}, nil, nil)      // empty ignored
	if len(jsonLevelFields) != before {
		t.Fatalf("duplicate/empty aliases changed the set size: %d -> %d", before, len(jsonLevelFields))
	}
}

func countOccurrences(s, sub string) int {
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}
