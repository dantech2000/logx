package logging

import (
	"bufio"
	"fmt"
	"io"
)

const maxScannerTokenSize = 1024 * 1024

// FilterAndFormatLogs reads log lines from reader, parses each one, and writes
// the formatted result to writer for entries at or above filterLevel.
func FilterAndFormatLogs(reader io.Reader, writer io.Writer, filterLevel LogLevel) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxScannerTokenSize)
	for scanner.Scan() {
		entry := ParseLogEntry(scanner.Text())
		if entry.Level >= filterLevel {
			if _, err := fmt.Fprintln(writer, FormatLogEntry(entry)); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
