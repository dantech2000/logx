package logging

import "testing"

func TestPayloadIndented(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"flush-left plain", "java.lang.RuntimeException: boom", false},
		{"tab-indented plain", "\tat Client.call(Client.java:42)", true},
		{"space-indented plain", "  more detail", true},
		{"kubelet prefix flush-left payload", "2026-05-15T00:38:02Z ERROR boom", false},
		{"kubelet prefix indented payload", "2026-05-15T00:38:02Z \tat Client.call", true},
		{"kubelet prefix with nanos, indented", "2026-05-15T00:38:02.123456789Z   stack frame", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := payloadIndented(tt.line); got != tt.want {
				t.Fatalf("payloadIndented(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestLevelTrackerGroupsContinuations(t *testing.T) {
	var tr LevelTracker

	// A flush-left unleveled line before any parent keeps its own default.
	if got := tr.Effective(LogEntry{Level: DEBUG}, "preamble"); got != DEBUG {
		t.Fatalf("orphan line = %v, want DEBUG", got)
	}

	// A detected level becomes the parent.
	if got := tr.Effective(LogEntry{Level: ERROR, LevelDetected: true}, "ERROR boom"); got != ERROR {
		t.Fatalf("leveled line = %v, want ERROR", got)
	}

	// An indented unleveled line inherits the parent's level.
	if got := tr.Effective(LogEntry{Level: DEBUG}, "\tat frame"); got != ERROR {
		t.Fatalf("indented continuation = %v, want inherited ERROR", got)
	}

	// A flush-left unleveled line is independent and does NOT inherit.
	if got := tr.Effective(LogEntry{Level: DEBUG}, "unrelated note"); got != DEBUG {
		t.Fatalf("flush-left line = %v, want DEBUG (independent)", got)
	}

	// The parent is unchanged by the independent line: a later indented line
	// still inherits the last detected level.
	if got := tr.Effective(LogEntry{Level: DEBUG}, "\tmore frame"); got != ERROR {
		t.Fatalf("indented after independent line = %v, want inherited ERROR", got)
	}

	// A new detected level replaces the parent.
	if got := tr.Effective(LogEntry{Level: WARN, LevelDetected: true}, "WARN retry"); got != WARN {
		t.Fatalf("new leveled line = %v, want WARN", got)
	}
	if got := tr.Effective(LogEntry{Level: DEBUG}, "\tretry detail"); got != WARN {
		t.Fatalf("indented continuation = %v, want inherited WARN", got)
	}
}
