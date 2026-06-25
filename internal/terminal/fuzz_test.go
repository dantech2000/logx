package terminal

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzSanitize checks that Sanitize upholds its invariants on arbitrary input:
// it never panics, the output is always valid UTF-8, the output never contains an
// unsafe control/spoofing rune, no raw ESC/CSI bytes survive, and it is
// idempotent.
func FuzzSanitize(f *testing.F) {
	seeds := []string{
		"",
		"plain text",
		"with\ttab",
		"esc \x1b[31m red",
		"c1 csi \x9b raw",
		"null\x00here",
		"bidi \u202e override",
		"zero\u200bwidth",
		"café 日本語 \U0001f680",
		"\xff\xfe invalid utf8",
		"\x1b]0;title\x07",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		out := Sanitize(in)

		// 1. Output must be valid UTF-8.
		if !utf8.ValidString(out) {
			t.Fatalf("Sanitize(%q) produced invalid UTF-8: %q", in, out)
		}

		// 2. No unsafe control/spoofing rune may remain (tab is allowed).
		for _, r := range out {
			if r != '\t' && isUnsafeRune(r) {
				t.Fatalf("Sanitize(%q) left unsafe rune %U in output %q", in, r, out)
			}
		}

		// 3. No raw ESC (0x1B) byte may survive. (0x1B never appears in valid
		// multibyte UTF-8 as a continuation byte, so a byte check is sound here;
		// the C1 CSI 0x9B can legitimately appear as a UTF-8 continuation byte, so
		// it is covered by invariants 1+2 instead of a byte check.)
		if strings.IndexByte(out, 0x1b) >= 0 {
			t.Fatalf("Sanitize(%q) leaked a raw ESC byte: %q", in, out)
		}

		// 4. Idempotent: sanitizing already-sanitized output is a no-op.
		if again := Sanitize(out); again != out {
			t.Fatalf("Sanitize not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}
