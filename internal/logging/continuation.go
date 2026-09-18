package logging

import (
	"regexp"
	"strings"
)

// kubeletTimestampSepRegex matches a kubelet RFC3339 timestamp prefix followed by
// exactly one separating space. Unlike kubernetesTimestampPrefixRegex (which uses
// \s+ and therefore swallows the payload's own leading whitespace), this matches
// only the single separator, so the content's indentation is preserved for
// continuation detection. The timestamp core is shared via kubeletTimestampPattern
// (parser.go).
var kubeletTimestampSepRegex = regexp.MustCompile(`^` + kubeletTimestampPattern + ` `)

// traceBoundaryRegex matches the flush-left lines that open or close a stack
// trace, as opposed to ordinary level-less prose:
//   - Python's "Traceback (most recent call last):" header,
//   - Go's "goroutine N [...]" header and "pkg.Func(args)" frame lines (Go
//     stack frames pair a flush-left function line with an indented file
//     line, so the function half needs absorbing too),
//   - "Caused by: ..." chained-exception lines (Java and friends),
//   - exception-class footers ("PaymentError: ...",
//     "java.lang.RuntimeException: upstream timeout").
//
// The class token is matched case-insensitively with a word boundary so prose
// merely mentioning errors ("Errors were found:", "No exception occurred:")
// does not match — while a genuine "error mounting volume:" footer dyes at
// most one line the parent's level, which for error-ish text is harmless. The
// match runs against the payload with any kubelet timestamp prefix removed,
// since --timestamps prefixes every line (and the timeline always enables it).
var traceBoundaryRegex = regexp.MustCompile(`(?i)^(Traceback \(most recent call last\):|goroutine \d+|Caused by:|\(?[\w./*-]+\.\w+\(.*\)|[\w.$]*?(Error|Exception|Panic)\b\s*:)`)

// stripKubeletTimestampPrefix removes the optional kubelet RFC3339 timestamp
// prefix (as added by --timestamps) so content checks apply to the
// application's own text. Only the single separating space is removed, so the
// payload's indentation survives for continuation detection.
func stripKubeletTimestampPrefix(rawLine string) string {
	if loc := kubeletTimestampSepRegex.FindStringIndex(rawLine); loc != nil {
		return rawLine[loc[1]:]
	}
	return rawLine
}

// payloadIndented reports whether the log content carried by rawLine begins with
// whitespace (a space or tab). It first removes an optional kubelet timestamp
// prefix (as added by --timestamps) so the check applies to the application's own
// content, not the kubelet separator.
func payloadIndented(rawLine string) bool {
	payload := stripKubeletTimestampPrefix(rawLine)
	return len(payload) > 0 && (payload[0] == ' ' || payload[0] == '\t')
}

// isTraceBoundary reports whether the line opens or closes a stack trace (see
// traceBoundaryRegex), checked against the payload without any kubelet
// timestamp prefix.
func isTraceBoundary(rawLine string) bool {
	return traceBoundaryRegex.MatchString(stripKubeletTimestampPrefix(rawLine))
}

// GroupRole tells whether a processed line starts its own entry or continues
// the entry begun by the most recent anchor. Anchors are lines with a detected
// level plus level-less flush-left lines that stand alone; continuations are
// the indented frames and the trace-boundary header/footer lines the tracker
// absorbs into an open group. Filters use the role to keep a multi-line entry
// together instead of shredding it line by line.
type GroupRole int

const (
	// GroupAnchor starts (or single-handedly constitutes) an entry.
	GroupAnchor GroupRole = iota
	// GroupContinuation belongs to the preceding anchor's entry.
	GroupContinuation
)

// LevelTracker groups the lines of a multi-line log entry by carrying the level
// of the most recent entry whose level was explicitly detected. An indented
// continuation line (e.g. a stack-trace frame), which has no level of its own,
// inherits that level so it is filtered and displayed together with the entry it
// belongs to. So does a trace-boundary line (see traceBoundaryRegex): in
// Java/Go/Python the exception/panic header ("Traceback (most recent call
// last):", "goroutine 1 [running]:", "pkg.Func()") sits flush-left between the
// error line and its indented frames, and chained or closing exception lines
// ("Caused by: ...", "PaymentError: ...") sit flush-left after them. Without
// absorbing those lines a level filter shows a decapitated trace: frames
// without their head or tail.
//
// The absorption is pattern-gated, not positional: only lines that look like
// trace boundaries join. Ordinary level-less prose ("line without an explicit
// level", "stack trace follows") never joins — it stays an independent entry
// at its own default level. Blank lines never join a group either.
//
// Tradeoff (intentional): the parent level persists across independent
// flush-left lines rather than resetting on them. This is required to keep real
// stack traces working — resetting on the header would orphan the frames. The
// cost is that an indented line appearing later under the same parent (e.g. an
// indented framework banner after an earlier error) can inherit that level.
// Distinguishing the two cases cleanly would require one-line lookahead, which
// is incompatible with --follow streaming. For a debugging tool we deliberately
// bias toward over-inclusion (a little extra at --level ERROR) rather than
// hiding a stack frame.
//
// The zero value is ready to use.
type LevelTracker struct {
	current   LogLevel
	hasParent bool
}

// Classify returns the level to use for filtering and display for the entry
// parsed from rawLine, updating the tracker, plus the entry's role in its
// group. An entry with a detected level becomes the new anchor. An entry
// without one joins the current group — inheriting the parent's level — when
// its content is indented (a frame) or a trace boundary (see
// traceBoundaryRegex); otherwise it stands alone as its own anchor and leaves
// the parent unchanged. See the type doc for the rationale.
func (t *LevelTracker) Classify(entry LogEntry, rawLine string) (LogLevel, GroupRole) {
	if entry.LevelDetected {
		t.current = entry.Level
		t.hasParent = true
		return entry.Level, GroupAnchor
	}
	if !t.hasParent || isBlankLine(rawLine) {
		return entry.Level, GroupAnchor
	}
	if payloadIndented(rawLine) || isTraceBoundary(rawLine) {
		return t.current, GroupContinuation
	}
	return entry.Level, GroupAnchor
}

// Effective returns the level to use for filtering and display for the entry
// parsed from rawLine, updating the tracker. It is Classify discarding the
// group role, kept for callers that only need the level.
func (t *LevelTracker) Effective(entry LogEntry, rawLine string) LogLevel {
	level, _ := t.Classify(entry, rawLine)
	return level
}

// isBlankLine reports whether the line carries no content. Blank lines never
// join a group: absorbing one as a header would dye the void the parent's
// level and emit a colored nothing.
func isBlankLine(rawLine string) bool {
	return strings.TrimSpace(rawLine) == ""
}
