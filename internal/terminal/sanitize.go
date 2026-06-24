package terminal

import (
	"fmt"
	"strings"
)

// Sanitize makes untrusted text safe to print to a terminal by escaping
// control bytes while preserving printable text and tabs. In addition to C0/C1
// control characters and DEL (which carry ANSI escape sequences), it neutralizes
// Unicode characters used for visual spoofing: bidirectional overrides
// (Trojan-Source), zero-width characters, and the U+2028/U+2029 line separators.
func Sanitize(value string) string {
	var builder strings.Builder
	changed := false

	for _, r := range value {
		if r == '\t' {
			builder.WriteRune(r)
			continue
		}
		if isUnsafeRune(r) {
			changed = true
			if r <= 0xff {
				fmt.Fprintf(&builder, `\x%02X`, r)
			} else {
				fmt.Fprintf(&builder, `\u%04X`, r)
			}
			continue
		}
		builder.WriteRune(r)
	}

	if !changed {
		return value
	}
	return builder.String()
}

// isUnsafeRune reports whether r should be escaped before printing to a terminal.
func isUnsafeRune(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		// C0 control chars, DEL, and C1 control chars (carry ANSI escapes).
		return true
	case r >= 0x200b && r <= 0x200f:
		// Zero-width space/joiners and LRM/RLM bidi marks.
		return true
	case r >= 0x202a && r <= 0x202e:
		// Bidi embedding/override controls (Trojan-Source).
		return true
	case r >= 0x2066 && r <= 0x2069:
		// Bidi isolate controls (LRI/RLI/FSI/PDI).
		return true
	case r == 0x2028, r == 0x2029:
		// Line/paragraph separators.
		return true
	case r == 0xfeff:
		// Zero-width no-break space / BOM.
		return true
	default:
		return false
	}
}
