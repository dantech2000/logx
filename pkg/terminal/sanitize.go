package terminal

import (
	"fmt"
	"strings"
)

// Sanitize makes untrusted text safe to print to a terminal by escaping
// control bytes while preserving printable text and tabs.
func Sanitize(value string) string {
	var builder strings.Builder
	changed := false

	for _, r := range value {
		if r == '\t' {
			builder.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			changed = true
			if r <= 0xff {
				builder.WriteString(fmt.Sprintf(`\x%02X`, r))
			} else {
				builder.WriteString(fmt.Sprintf(`\u%04X`, r))
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
