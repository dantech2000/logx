package terminal

import "testing"

func TestSanitizeEscapesTerminalControls(t *testing.T) {
	input := "ok\x1b]52;c;secret\x07\tkeep"
	got := Sanitize(input)
	want := `ok\x1B]52;c;secret\x07	keep`

	if got != want {
		t.Fatalf("Sanitize() = %q, want %q", got, want)
	}
}

func TestSanitizeLeavesPrintableText(t *testing.T) {
	input := "plain text 123"
	if got := Sanitize(input); got != input {
		t.Fatalf("Sanitize() = %q, want %q", got, input)
	}
}
