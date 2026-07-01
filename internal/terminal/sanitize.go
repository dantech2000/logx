package terminal

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Sanitize makes untrusted text safe to print to a terminal by escaping
// control bytes while preserving printable text and tabs. In addition to C0/C1
// control characters and DEL (which carry ANSI escape sequences), it neutralizes
// Unicode characters used for visual spoofing: bidirectional overrides
// (Trojan-Source), zero-width characters, and the U+2028/U+2029 line separators.
//
// Invalid UTF-8 bytes are escaped too (e.g. a raw 0x9B, the C1 CSI), so the
// result is always valid UTF-8 with no raw control bytes — decoding via the
// `range` operator alone would silently turn such bytes into U+FFFD and could
// leave the raw byte in the output via the unchanged fast path.
func Sanitize(value string) string {
	// Fast path: find the first byte/rune that needs escaping. Most values are
	// clean and return unchanged without touching a builder — this runs many
	// times per rendered line, so the zero-allocation path matters.
	start := 0
	for start < len(value) {
		r, size := utf8.DecodeRuneInString(value[start:])
		if needsEscape(r, size) {
			break
		}
		start += size
	}
	if start == len(value) {
		return value
	}

	var builder strings.Builder
	builder.Grow(len(value))
	builder.WriteString(value[:start])

	for i := start; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if !needsEscape(r, size) {
			builder.WriteRune(r)
			i += size
			continue
		}
		switch {
		case r == utf8.RuneError && size == 1:
			// Invalid UTF-8 byte: escape the raw byte itself.
			fmt.Fprintf(&builder, `\x%02X`, value[i])
		case r <= 0xff:
			fmt.Fprintf(&builder, `\x%02X`, r)
		default:
			fmt.Fprintf(&builder, `\u%04X`, r)
		}
		i += size
	}

	return builder.String()
}

// needsEscape reports whether the rune decoded at the current position must be
// escaped: an invalid UTF-8 byte (RuneError with size 1) or an unsafe rune,
// with tab exempted. The single predicate shared by Sanitize's fast-path scan
// and its escape loop, so the two can never drift.
func needsEscape(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		return true
	}
	return r != '\t' && isUnsafeRune(r)
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
