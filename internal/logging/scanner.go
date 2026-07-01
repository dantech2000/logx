package logging

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

const (
	// MaxLogLineSize is the largest single log line kept in full. Longer lines are
	// truncated (not dropped) so one pathological line — a huge base64 blob or a
	// giant stack trace — cannot abort the stream or hide later lines.
	MaxLogLineSize = 1024 * 1024
	// readerBufferSize is the underlying bufio read-buffer size.
	readerBufferSize = 64 * 1024
	// truncationMarker is appended to a line that exceeded MaxLogLineSize.
	truncationMarker = " …[line truncated]"
)

// LineReader reads newline-delimited lines from an io.Reader with a bounded
// per-line memory cost. It mirrors the bufio.Scanner Scan/Text/Err shape but,
// unlike bufio.Scanner, never fails on an over-long line: such a line is
// truncated to MaxLogLineSize (with a marker) and reading continues.
type LineReader struct {
	r    *bufio.Reader
	text string
	err  error
	done bool
}

// NewLineReader returns a LineReader over r.
func NewLineReader(r io.Reader) *LineReader {
	return &LineReader{r: bufio.NewReaderSize(r, readerBufferSize)}
}

// Scan advances to the next line, returning false at end of input or on error
// (retrievable via Err). The line (without the trailing newline) is available
// via Text.
func (lr *LineReader) Scan() bool {
	if lr.done {
		return false
	}

	var b strings.Builder
	truncated := false
	sawAny := false

	for {
		frag, err := lr.r.ReadSlice('\n')
		if len(frag) > 0 {
			sawAny = true
			chunk := frag
			if err == nil { // frag includes the trailing '\n'
				chunk = frag[:len(frag)-1]
			}
			// Keep at most the room left under MaxLogLineSize; anything beyond
			// it is dropped and the line marked truncated.
			keep := min(len(chunk), max(MaxLogLineSize-b.Len(), 0))
			b.Write(chunk[:keep])
			if keep < len(chunk) {
				truncated = true
			}
		}

		if err == nil { // reached the line's newline
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) { // more of this same line remains
			continue
		}
		// io.EOF or a real read error ends the stream.
		lr.done = true
		if !errors.Is(err, io.EOF) {
			lr.err = err
		}
		break
	}

	if !sawAny {
		return false
	}

	text := strings.TrimSuffix(b.String(), "\r") // tolerate CRLF endings
	if truncated {
		text += truncationMarker
	}
	lr.text = text
	return true
}

// Text returns the most recent line read by Scan.
func (lr *LineReader) Text() string { return lr.text }

// Err returns the first non-EOF read error encountered, if any.
func (lr *LineReader) Err() error { return lr.err }
