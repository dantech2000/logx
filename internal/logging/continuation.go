package logging

import "regexp"

// kubeletTimestampSepRegex matches a kubelet RFC3339 timestamp prefix followed by
// exactly one separating space. Unlike kubernetesTimestampPrefixRegex (which uses
// \s+ and therefore swallows the payload's own leading whitespace), this matches
// only the single separator, so the content's indentation is preserved for
// continuation detection.
var kubeletTimestampSepRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z `)

// payloadIndented reports whether the log content carried by rawLine begins with
// whitespace (a space or tab). It first removes an optional kubelet timestamp
// prefix (as added by --timestamps) so the check applies to the application's own
// content, not the kubelet separator.
func payloadIndented(rawLine string) bool {
	payload := rawLine
	if loc := kubeletTimestampSepRegex.FindStringIndex(rawLine); loc != nil {
		payload = rawLine[loc[1]:]
	}
	return len(payload) > 0 && (payload[0] == ' ' || payload[0] == '\t')
}

// LevelTracker groups the lines of a multi-line log entry by carrying the level
// of the most recent entry whose level was explicitly detected. An indented
// continuation line (e.g. a stack-trace frame), which has no level of its own,
// inherits that level so it is filtered and displayed together with the entry it
// belongs to. A flush-left line without a detected level keeps its own default
// (it is never itself relabeled).
//
// Tradeoff (intentional): the parent level persists across intervening flush-left
// lines rather than resetting on them. This is required to keep real stack traces
// working — in Java/Go/Python a flush-left exception/panic header sits between the
// error line and its indented frames, so resetting on that header would orphan
// the frames. The cost is that an indented line appearing later under the same
// parent (e.g. an indented framework banner after an earlier error) can inherit
// that level. Distinguishing the two cases cleanly would require one-line
// lookahead, which is incompatible with --follow streaming. For a debugging tool
// we deliberately bias toward over-inclusion (a little extra at --level ERROR)
// rather than hiding a stack frame.
//
// The zero value is ready to use.
type LevelTracker struct {
	current   LogLevel
	hasParent bool
}

// Effective returns the level to use for filtering and display for the entry
// parsed from rawLine, updating the tracker. An entry with a detected level
// becomes the new parent. An entry without one inherits the current parent's
// level only when its content is indented (a continuation line); otherwise it
// keeps its own default and leaves the parent unchanged. See the type doc for
// the rationale behind not resetting on flush-left lines.
func (t *LevelTracker) Effective(entry LogEntry, rawLine string) LogLevel {
	if entry.LevelDetected {
		t.current = entry.Level
		t.hasParent = true
		return entry.Level
	}
	if t.hasParent && payloadIndented(rawLine) {
		return t.current
	}
	return entry.Level
}
