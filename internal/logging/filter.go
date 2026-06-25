package logging

import (
	"fmt"
	"io"
)

// FilterAndFormatLogs reads log lines from reader, parses each one, and writes
// the formatted result to writer for entries at or above filterLevel. Indented
// continuation lines of a multi-line entry inherit the level of the entry they
// belong to so a multi-line entry is filtered as a unit.
func FilterAndFormatLogs(reader io.Reader, writer io.Writer, filterLevel LogLevel) error {
	var tracker LevelTracker
	scanner := NewLineScanner(reader)
	for scanner.Scan() {
		rawLine := scanner.Text()
		entry := ParseLogEntry(rawLine)
		entry.Level = tracker.Effective(entry, rawLine)
		if entry.Level >= filterLevel {
			if _, err := fmt.Fprintln(writer, FormatLogEntry(entry)); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
