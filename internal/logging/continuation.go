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
// belongs to. Flush-left lines without a detected level are treated as
// independent entries and are never swept into a preceding entry's level.
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
// keeps its own default and leaves the parent unchanged.
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
