package logging

import (
	"strings"
	"testing"
)

func readAll(t *testing.T, in string) ([]string, error) {
	t.Helper()
	lr := NewLineReader(strings.NewReader(in))
	var lines []string
	for lr.Scan() {
		lines = append(lines, lr.Text())
	}
	return lines, lr.Err()
}

func TestLineReaderBasics(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"two lines", "a\nb\n", []string{"a", "b"}},
		{"no trailing newline", "a\nb", []string{"a", "b"}},
		{"crlf endings", "a\r\nb\r\n", []string{"a", "b"}},
		{"blank lines preserved as empty", "a\n\nb\n", []string{"a", "", "b"}},
		{"only newline", "\n", []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readAll(t, tt.in)
			if err != nil {
				t.Fatalf("Err() = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("lines = %q, want %q", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("line[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLineReaderTruncatesOverLongLineAndContinues(t *testing.T) {
	huge := strings.Repeat("x", MaxLogLineSize+5000)
	in := "before\n" + huge + "\nafter\n"

	got, err := readAll(t, in)
	if err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3 (the over-long line must not abort the stream)", len(got))
	}
	if got[0] != "before" || got[2] != "after" {
		t.Fatalf("surrounding lines wrong: %q / %q", got[0], got[2])
	}
	if !strings.HasSuffix(got[1], truncationMarker) {
		t.Fatalf("over-long line not marked truncated: ...%q", got[1][len(got[1])-40:])
	}
	// The truncated payload is bounded by MaxLogLineSize.
	if payload := strings.TrimSuffix(got[1], truncationMarker); len(payload) != MaxLogLineSize {
		t.Fatalf("truncated payload length = %d, want %d", len(payload), MaxLogLineSize)
	}
}
