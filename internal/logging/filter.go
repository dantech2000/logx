package logging

import (
	"fmt"
	"io"
	"strings"
)

// FilterAndFormatLogs reads log lines from reader, parses each one, and writes
// the formatted result to writer for entries at or above filterLevel. Indented
// continuation lines of a multi-line entry inherit the level of the entry they
// belong to so a multi-line entry is filtered as a unit. A leading kubelet
// timestamp prefix (as added by `kubectl logs --timestamps`) is recognized and
// used as the entry's timestamp, so piping such output in still parses cleanly.
func FilterAndFormatLogs(reader io.Reader, writer io.Writer, filterLevel LogLevel) error {
	var tracker LevelTracker
	scanner := NewLineScanner(reader)
	for scanner.Scan() {
		rawLine := scanner.Text()
		// Skip blank/whitespace-only lines so they don't render as empty entries
		// (and so they don't interfere with multi-line grouping).
		if strings.TrimSpace(rawLine) == "" {
			continue
		}
		entry := ParseKubernetesLogEntry(rawLine)
		entry.Level = tracker.Effective(entry, rawLine)
		if entry.Level >= filterLevel {
			if _, err := fmt.Fprintln(writer, FormatLogEntry(entry)); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
