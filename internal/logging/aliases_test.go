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
	t.Cleanup(func() {
		jsonLevelFields = level
		jsonMessageFields = message
		jsonTimeFields = timeF
		jsonFormattedFieldExclusions = excl
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
