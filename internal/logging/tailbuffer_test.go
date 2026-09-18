package logging

import (
	"strings"
	"testing"
)

func tailLines(b *TailBuffer[string]) string {
	return strings.Join(b.Lines(), "\n")
}

// A single-line stream tails by lines, exactly like before.
func TestTailBufferKeepsLastAnchors(t *testing.T) {
	b := NewTailBuffer[string](2)
	for _, line := range []string{"one", "two", "three", "four"} {
		b.Add(line, GroupAnchor)
	}
	if got := tailLines(b); got != "three\nfour" {
		t.Fatalf("tail = %q, want last two lines", got)
	}
}

// A group entering the window is kept whole: the anchor counts, its frames
// ride along even past a strict line count.
func TestTailBufferKeepsGroupsWhole(t *testing.T) {
	b := NewTailBuffer[string](1)
	b.Add("INFO first", GroupAnchor)
	b.Add("ERROR boom", GroupAnchor)
	b.Add("Traceback (most recent call last):", GroupContinuation)
	b.Add("  at pay.go:42", GroupContinuation)
	if got := tailLines(b); got != "ERROR boom\nTraceback (most recent call last):\n  at pay.go:42" {
		t.Fatalf("tail split a group:\n%s", got)
	}
}

// Eviction drops whole sealed groups: with capacity two over three anchors,
// only the last two entries (with their frames) survive.
func TestTailBufferEvictsWholeGroups(t *testing.T) {
	b := NewTailBuffer[string](2)
	b.Add("INFO a", GroupAnchor)
	b.Add("  frame-a", GroupContinuation)
	b.Add("INFO b", GroupAnchor)
	b.Add("INFO c", GroupAnchor)
	b.Add("  frame-c", GroupContinuation)
	want := "INFO b\nINFO c\n  frame-c"
	if got := tailLines(b); got != want {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

// Continuations before any anchor (a stream starting mid-trace, e.g. under
// --since) are kept as a leading partial group outside the budget.
func TestTailBufferKeepsLeadingOrphans(t *testing.T) {
	b := NewTailBuffer[string](1)
	b.Add("  orphan frame", GroupContinuation)
	b.Add("INFO only", GroupAnchor)
	want := "  orphan frame\nINFO only"
	if got := tailLines(b); got != want {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

// Zero capacity retains nothing.
func TestTailBufferZeroRetainsNothing(t *testing.T) {
	b := NewTailBuffer[string](0)
	b.Add("INFO dropped", GroupAnchor)
	if lines := b.Lines(); len(lines) != 0 {
		t.Fatalf("zero-capacity tail retained %q", lines)
	}
}
