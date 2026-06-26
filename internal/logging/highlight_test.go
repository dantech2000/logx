package logging

import (
	"regexp"
	"strings"
	"testing"
)

func TestHighlightMatchesMergesOverlaps(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorAlways)

	// Two overlapping patterns over "errors" must produce a single contiguous
	// highlighted span, not nested/duplicated escapes.
	s := "errors happen"
	out := highlightMatches(s, []*regexp.Regexp{
		regexp.MustCompile(`err`),
		regexp.MustCompile(`rror`),
	})
	if got := strings.Count(out, highlightOn); got != 1 {
		t.Fatalf("expected 1 merged highlight span, got %d: %q", got, out)
	}
	if !strings.Contains(out, highlightOn+"error"+highlightOff) {
		t.Fatalf("merged span did not cover the whole overlap: %q", out)
	}
	if !strings.HasSuffix(out, "s happen") {
		t.Fatalf("text after the match was lost: %q", out)
	}
}

func TestHighlightMatchesNoColorIsNoop(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)
	s := "plain text"
	if out := highlightMatches(s, []*regexp.Regexp{regexp.MustCompile(`plain`)}); out != s {
		t.Fatalf("highlight changed text with color off: %q", out)
	}
}

func TestHighlightMatchesPreservesAllSegments(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorAlways)
	out := highlightMatches("a-b-a-b", []*regexp.Regexp{regexp.MustCompile(`b`)})
	// Strip the escapes; the visible text must be unchanged.
	visible := strings.NewReplacer(highlightOn, "", highlightOff, "").Replace(out)
	if visible != "a-b-a-b" {
		t.Fatalf("visible text changed: %q -> %q", "a-b-a-b", visible)
	}
	if strings.Count(out, highlightOn) != 2 {
		t.Fatalf("expected 2 highlight spans, got %q", out)
	}
}
