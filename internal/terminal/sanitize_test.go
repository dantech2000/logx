package terminal

import (
	"fmt"
	"testing"
)

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

func TestSanitizeEscapesUnicodeSpoofing(t *testing.T) {
	// Each rune is a known visual-spoofing character that must be escaped to its
	// \uXXXX form. Building inputs/wants from code points keeps the source ASCII.
	spoofs := []struct {
		name string
		r    rune
	}{
		{"zero-width space", 0x200b},
		{"left-to-right mark", 0x200e},
		{"right-to-left mark", 0x200f},
		{"left-to-right embedding", 0x202a},
		{"right-to-left override", 0x202e},
		{"left-to-right isolate", 0x2066},
		{"pop directional isolate", 0x2069},
		{"line separator", 0x2028},
		{"paragraph separator", 0x2029},
		{"zero-width no-break space (BOM)", 0xfeff},
	}
	for _, s := range spoofs {
		t.Run(s.name, func(t *testing.T) {
			input := "a" + string(s.r) + "b"
			want := fmt.Sprintf(`a\u%04Xb`, s.r)
			if got := Sanitize(input); got != want {
				t.Fatalf("Sanitize(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestSanitizeLeavesUnicodeText(t *testing.T) {
	// Legitimate multi-byte text must pass through untouched.
	input := "café 日本語 rocket \U0001f680"
	if got := Sanitize(input); got != input {
		t.Fatalf("Sanitize() = %q, want %q", got, input)
	}
}
