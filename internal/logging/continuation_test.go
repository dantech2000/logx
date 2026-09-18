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
	// still inherits the last detected level. This is the intentional
	// over-inclusion tradeoff (see LevelTracker doc): resetting here would orphan
	// the indented frames of a Java/Go stack trace whose exception/panic header is
	// itself a flush-left line.
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

// classifyStep feeds one line through Classify and asserts the outcome.
func classifyStep(t *testing.T, tr *LevelTracker, entry LogEntry, raw string, wantLevel LogLevel, wantRole GroupRole) {
	t.Helper()
	gotLevel, gotRole := tr.Classify(entry, raw)
	if gotLevel != wantLevel || gotRole != wantRole {
		t.Fatalf("Classify(%q) = (%v, %v), want (%v, %v)", raw, gotLevel, gotRole, wantLevel, wantRole)
	}
}

// A Python traceback's flush-left header and footer join the error's group so
// a level filter never shows frames without their head or tail.
func TestLevelTrackerAbsorbsPythonTraceback(t *testing.T) {
	var tr LevelTracker
	plain := func() LogEntry { return LogEntry{Level: DEBUG} }
	leveled := func(l LogLevel) LogEntry { return LogEntry{Level: l, LevelDetected: true} }

	classifyStep(t, &tr, leveled(ERROR), `{"level":"error","msg":"payment failed"}`, ERROR, GroupAnchor)
	classifyStep(t, &tr, plain(), "Traceback (most recent call last):", ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), `  File "/app/pay.py", line 42, in charge`, ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), `    gateway.capture(order)`, ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), "PaymentError: insufficient funds", ERROR, GroupContinuation)
	// A further exception line still belongs to the chain (chained exceptions
	// read as one entry); only non-boundary prose starts a new entry.
	classifyStep(t, &tr, plain(), "AnotherError: cascading failure", ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), "while processing the refund queue", DEBUG, GroupAnchor)
}

// Go panics and Java chained causes absorb the same way.
func TestLevelTrackerAbsorbsGoAndJavaBoundaries(t *testing.T) {
	var tr LevelTracker
	plain := func() LogEntry { return LogEntry{Level: DEBUG} }
	leveled := func(l LogLevel) LogEntry { return LogEntry{Level: l, LevelDetected: true} }

	classifyStep(t, &tr, leveled(FATAL), "panic: nil map write", FATAL, GroupAnchor)
	classifyStep(t, &tr, plain(), "goroutine 1 [running]:", FATAL, GroupContinuation)
	// Go stack frames pair a flush-left function line with an indented file
	// line; a run of boundary lines all belongs to the trace.
	classifyStep(t, &tr, plain(), "main.main()", FATAL, GroupContinuation)
	classifyStep(t, &tr, plain(), "\t/app/main.go:10 +0x14", FATAL, GroupContinuation)
	classifyStep(t, &tr, plain(), "server.Serve(l)", FATAL, GroupContinuation)

	classifyStep(t, &tr, leveled(ERROR), "ERROR wrapper failed", ERROR, GroupAnchor)
	classifyStep(t, &tr, plain(), "    at com.example.Wrapper.call(Wrapper.java:10)", ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), "Caused by: java.io.IOException: broken pipe", ERROR, GroupContinuation)
	classifyStep(t, &tr, plain(), "    at com.example.IO.read(IO.java:5)", ERROR, GroupContinuation)
}

// Ordinary level-less prose never joins a group, even directly after an error,
// and neither do blank lines. A boundary line arriving later still absorbs:
// non-matching lines do not burn the header/footer slots.
func TestLevelTrackerLeavesProseAlone(t *testing.T) {
	var tr LevelTracker
	plain := func() LogEntry { return LogEntry{Level: DEBUG} }
	leveled := func(l LogLevel) LogEntry { return LogEntry{Level: l, LevelDetected: true} }

	classifyStep(t, &tr, leveled(ERROR), "ERROR upstream unavailable", ERROR, GroupAnchor)
	classifyStep(t, &tr, plain(), "line without an explicit level", DEBUG, GroupAnchor)
	classifyStep(t, &tr, plain(), "", DEBUG, GroupAnchor)
	classifyStep(t, &tr, plain(), "Traceback (most recent call last):", ERROR, GroupContinuation)
}

// Trace boundaries are recognized under a kubelet timestamp prefix, which the
// timeline always adds and --timestamps adds on request.
func TestLevelTrackerAbsorbsPrefixedBoundary(t *testing.T) {
	var tr LevelTracker
	leveled := func(l LogLevel) LogEntry { return LogEntry{Level: l, LevelDetected: true} }
	classifyStep(t, &tr, leveled(ERROR), "2026-05-15T00:38:02Z ERROR boom", ERROR, GroupAnchor)
	level, role := tr.Classify(LogEntry{Level: DEBUG}, "2026-05-15T00:38:02Z Traceback (most recent call last):")
	if level != ERROR || role != GroupContinuation {
		t.Fatalf("prefixed boundary = (%v, %v), want (ERROR, continuation)", level, role)
	}
}
