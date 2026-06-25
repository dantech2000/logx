package logging

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dantech2000/logx/internal/terminal"
	"github.com/fatih/color"
)

const (
	// Reverse-video on/off. Unlike a foreground/background color it composes with
	// the colors already in the rendered line: it only toggles the reverse
	// attribute, so surrounding theme colors are preserved across the match.
	highlightOn  = "\x1b[7m"
	highlightOff = "\x1b[27m"
)

// highlightMatches wraps every non-overlapping match of any pattern in s with
// reverse-video so grep matches stand out. Overlapping match ranges from
// different patterns are merged. It is a no-op when color is disabled, so plain
// output stays free of escape codes.
func highlightMatches(s string, patterns []*regexp.Regexp) string {
	if color.NoColor || len(patterns) == 0 || s == "" {
		return s
	}

	type span struct{ start, end int }
	var spans []span
	for _, re := range patterns {
		for _, m := range re.FindAllStringIndex(s, -1) {
			if m[1] > m[0] { // ignore zero-width matches
				spans = append(spans, span{m[0], m[1]})
			}
		}
	}
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var b strings.Builder
	cursor := 0
	cur := spans[0]
	flush := func(sp span) {
		if sp.start < cursor {
			sp.start = cursor
		}
		if sp.start >= sp.end {
			return
		}
		b.WriteString(s[cursor:sp.start])
		b.WriteString(highlightOn)
		b.WriteString(s[sp.start:sp.end])
		b.WriteString(highlightOff)
		cursor = sp.end
	}
	for _, sp := range spans[1:] {
		if sp.start <= cur.end { // overlapping/adjacent: merge
			if sp.end > cur.end {
				cur.end = sp.end
			}
			continue
		}
		flush(cur)
		cur = sp
	}
	flush(cur)
	b.WriteString(s[cursor:])
	return b.String()
}

// Color accessors read from the active theme (see theme.go) so a --theme switch
// re-colors all output through one place.
func levelColorFor(level LogLevel) *color.Color { return activeTheme.levelColor(level) }
func timestampColor() *color.Color              { return activeTheme.timestamp }
func loggerColor() *color.Color                 { return activeTheme.logger }
func keyColor() *color.Color                    { return activeTheme.key }
func valueColor() *color.Color                  { return activeTheme.value }
func quoteColor() *color.Color                  { return activeTheme.quote }
func errorColor() *color.Color                  { return activeTheme.errorText }

var jsonFormattedFieldExclusions = buildStringSet(jsonLevelFields, jsonTimeFields)

// FormatLogEntry renders a parsed log entry as a single colorized line with an
// optional timestamp, level label, logger, and message/field details.
func FormatLogEntry(entry LogEntry) string {
	var parts []string

	if !entry.Timestamp.IsZero() {
		// Normalize to UTC so timestamps are consistent regardless of the source
		// format (RFC3339 parses to UTC, but epoch values parse to local time);
		// this also matches the --timeline output.
		parts = append(parts, timestampColor().Sprintf("[%s]", entry.Timestamp.UTC().Format("2006-01-02 15:04:05")))
	}

	parts = append(parts, FormatLogLevelLabel(entry.Level))

	if entry.Format == FormatJSON && entry.Logger != "" {
		parts = append(parts, loggerColor().Sprintf("[%s]", terminal.Sanitize(entry.Logger)))
	}

	parts = append(parts, FormatLogEntryDetails(entry))
	return strings.Join(parts, " ")
}

// FormatLogLevelLabel returns the colorized bracketed label for a log level.
func FormatLogLevelLabel(level LogLevel) string {
	return levelColorFor(level).Sprint(fmt.Sprintf("[%s]", level))
}

// FormatLogEntryDetails renders the message and structured fields of an entry,
// dispatching on its detected format.
func FormatLogEntryDetails(entry LogEntry) string {
	switch entry.Format {
	case FormatJSON:
		return formatJSONDetails(entry)
	case FormatBracketed, FormatLogfmt:
		return formatStructuredDetails(entry)
	default:
		return formatPlainTextDetails(entry)
	}
}

// formatStructuredDetails renders the parsed message and remaining fields of a
// bracketed or logfmt entry, so the reconstructed output is not the raw line
// (which would duplicate the timestamp/level that logx already prints).
func formatStructuredDetails(entry LogEntry) string {
	var parts []string
	// Skip the message when it is just the raw line (the parser's fallback when
	// no message field was found); otherwise the raw line would be printed and
	// then the fields repeated after it.
	if entry.Message != "" && entry.Message != entry.RawLine {
		parts = append(parts, formatMessage(entry, terminal.Sanitize(entry.Message)))
	}
	parts = append(parts, formatSortedFields(entry.Fields)...)
	if len(parts) == 0 {
		return formatPlainTextDetails(entry)
	}
	return strings.Join(parts, " ")
}

func formatJSONDetails(entry LogEntry) string {
	if entry.Fields == nil {
		return terminal.Sanitize(entry.RawLine)
	}

	var fields []string
	if msg := jsonMessage(entry); msg != "" {
		fields = append(fields, formatMessage(entry, msg))
	}
	fields = append(fields, formatSortedFields(entry.Fields)...)

	return strings.Join(fields, " ")
}

func formatPlainTextDetails(entry LogEntry) string {
	message := entry.Message
	if message == "" {
		message = entry.RawLine
	}
	sanitized := terminal.Sanitize(message)
	if entry.Level == ERROR || containsAttentionText(message) {
		return errorColor().Sprint(sanitized)
	}
	return sanitized
}

func jsonMessage(entry LogEntry) string {
	for _, field := range jsonMessageFields {
		if val, ok := entry.Fields[field]; ok {
			return terminal.Sanitize(fmt.Sprintf("%v", val))
		}
	}
	return ""
}

func formatMessage(entry LogEntry, msg string) string {
	if entry.Level == ERROR || containsAttentionText(msg) {
		return errorColor().Sprint(msg)
	}
	return msg
}

func containsAttentionText(value string) bool {
	lowerValue := strings.ToLower(value)
	return strings.Contains(lowerValue, "error") ||
		strings.Contains(lowerValue, "failed") ||
		strings.Contains(lowerValue, "warn")
}

func formatSortedFields(fields map[string]interface{}) []string {
	formattedFields := make([]string, 0, len(fields))
	for _, key := range sortedKeys(fields) {
		if jsonFormattedFieldExclusions[key] || isJSONMessageField(key) {
			continue
		}
		formattedFields = append(formattedFields, fmt.Sprintf("%s=%s",
			keyColor().Sprint(terminal.Sanitize(key)),
			formatValue(fields[key])))
	}
	return formattedFields
}

func isJSONMessageField(field string) bool {
	for _, messageField := range jsonMessageFields {
		if field == messageField {
			return true
		}
	}
	return false
}

func sortedKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return formatStringValue(val)
	case nil:
		return quoteColor().Sprint("null")
	case bool:
		if val {
			return valueColor().Sprint("true")
		}
		return valueColor().Sprint("false")
	case float64:
		if float64(int64(val)) == val {
			return valueColor().Sprintf("%d", int64(val))
		}
		return valueColor().Sprintf("%.2f", val)
	case float32:
		return formatValue(float64(val))
	case json.Number:
		return formatJSONNumber(val)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return valueColor().Sprintf("%d", val)
	case map[string]interface{}:
		parts := make([]string, 0, len(val))
		for _, key := range sortedKeys(val) {
			parts = append(parts, fmt.Sprintf("%s=%s",
				keyColor().Sprint(terminal.Sanitize(key)),
				formatValue(val[key])))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, " "))
	case []interface{}:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, formatValue(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(parts, " "))
	default:
		return valueColor().Sprint(terminal.Sanitize(fmt.Sprintf("%v", val)))
	}
}

// formatStringValue sanitizes and colorizes a string value, quoting it when it
// contains characters that would be ambiguous in unquoted logfmt output.
func formatStringValue(val string) string {
	val = terminal.Sanitize(val)
	if val == "" {
		return quoteColor().Sprint(`""`)
	}
	if strings.ContainsAny(val, " =,\"'[]{}()") {
		return fmt.Sprintf("%s%s%s",
			quoteColor().Sprint(`"`),
			valueColor().Sprint(val),
			quoteColor().Sprint(`"`))
	}
	return valueColor().Sprint(val)
}

// formatJSONNumber renders a json.Number. Integer literals are printed verbatim
// (so large IDs beyond int64 keep full precision and don't gain a ".00"); values
// with a fraction or exponent are rendered with two decimals.
func formatJSONNumber(val json.Number) string {
	s := val.String()
	if !strings.ContainsAny(s, ".eE") {
		// Pure integer literal: preserve exactly (handles values larger than int64).
		return valueColor().Sprint(terminal.Sanitize(s))
	}
	if parsedValue, err := val.Float64(); err == nil {
		return valueColor().Sprintf("%.2f", parsedValue)
	}
	return valueColor().Sprint(terminal.Sanitize(s))
}
