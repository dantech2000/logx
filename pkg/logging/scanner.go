package logging

import (
	"bufio"
	"io"
)

const (
	// MaxLogLineSize is the largest single log line the scanners accept before
	// reporting bufio.ErrTooLong. Lines longer than this are unusual and are
	// almost always a sign of binary or pathological output.
	MaxLogLineSize = 1024 * 1024
	// initialScannerBuffer is the starting token buffer size; it grows up to
	// MaxLogLineSize as needed.
	initialScannerBuffer = 64 * 1024
)

// NewLineScanner returns a bufio.Scanner configured to read log lines up to
// MaxLogLineSize, so long lines do not abort the stream prematurely. Centralizing
// this avoids the buffer sizes drifting apart across call sites.
func NewLineScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, initialScannerBuffer), MaxLogLineSize)
	return scanner
}
