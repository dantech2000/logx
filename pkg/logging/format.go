package logging

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dantech2000/logx/pkg/terminal"
	"github.com/fatih/color"
)

var logLevelColors = map[LogLevel]*color.Color{
	DEBUG: color.New(color.FgCyan),
	INFO:  color.New(color.FgGreen),
	WARN:  color.New(color.FgYellow),
	ERROR: color.New(color.FgRed),
}

var (
	timestampColor = color.New(color.FgBlue)
	loggerColor    = color.New(color.FgMagenta)
	keyColor       = color.New(color.FgCyan)
	valueColor     = color.New(color.FgWhite)
	quoteColor     = color.New(color.FgHiBlack)
	errorColor     = color.New(color.FgRed, color.Bold)
)

var jsonFormattedFieldExclusions = buildStringSet(jsonLevelFields, jsonTimeFields)

func FormatLogEntry(entry LogEntry) string {
	var parts []string

	if !entry.Timestamp.IsZero() {
		parts = append(parts, timestampColor.Sprintf("[%s]", entry.Timestamp.Format("2006-01-02 15:04:05")))
	}

	parts = append(parts, FormatLogLevelLabel(entry.Level))

	if entry.Format == FormatJSON && entry.Logger != "" {
		parts = append(parts, loggerColor.Sprintf("[%s]", terminal.Sanitize(entry.Logger)))
	}

	parts = append(parts, FormatLogEntryDetails(entry))
	return strings.Join(parts, " ")
}

func FormatLogLevelLabel(level LogLevel) string {
	levelColor, ok := logLevelColors[level]
	if !ok {
		levelColor = logLevelColors[DEBUG]
	}
	return levelColor.Sprint(fmt.Sprintf("[%s]", level))
}

func FormatLogEntryDetails(entry LogEntry) string {
	if entry.Format == FormatJSON {
		return formatJSONDetails(entry)
	}
	return formatPlainTextDetails(entry)
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
	rawLine := terminal.Sanitize(entry.RawLine)
	if entry.Level == ERROR || containsAttentionText(entry.RawLine) {
		return errorColor.Sprint(rawLine)
	}
	return rawLine
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
		return errorColor.Sprint(msg)
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
			keyColor.Sprint(terminal.Sanitize(key)),
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
		val = terminal.Sanitize(val)
		if val == "" {
			return quoteColor.Sprint(`""`)
		}
		if strings.ContainsAny(val, " =,\"'[]{}()") {
			return fmt.Sprintf("%s%s%s",
				quoteColor.Sprint(`"`),
				valueColor.Sprint(val),
				quoteColor.Sprint(`"`))
		}
		return valueColor.Sprint(val)
	case nil:
		return quoteColor.Sprint("null")
	case bool:
		if val {
			return valueColor.Sprint("true")
		}
		return valueColor.Sprint("false")
	case float64:
		if float64(int64(val)) == val {
			return valueColor.Sprintf("%d", int64(val))
		}
		return valueColor.Sprintf("%.2f", val)
	case float32:
		return formatValue(float64(val))
	case json.Number:
		if _, err := val.Int64(); err == nil {
			return valueColor.Sprint(val.String())
		}
		if parsedValue, err := val.Float64(); err == nil {
			return valueColor.Sprintf("%.2f", parsedValue)
		}
		return valueColor.Sprint(terminal.Sanitize(val.String()))
	case int:
		return valueColor.Sprintf("%d", val)
	case int8:
		return valueColor.Sprintf("%d", val)
	case int16:
		return valueColor.Sprintf("%d", val)
	case int32:
		return valueColor.Sprintf("%d", val)
	case int64:
		return valueColor.Sprintf("%d", val)
	case uint:
		return valueColor.Sprintf("%d", val)
	case uint8:
		return valueColor.Sprintf("%d", val)
	case uint16:
		return valueColor.Sprintf("%d", val)
	case uint32:
		return valueColor.Sprintf("%d", val)
	case uint64:
		return valueColor.Sprintf("%d", val)
	case map[string]interface{}:
		parts := make([]string, 0, len(val))
		for _, key := range sortedKeys(val) {
			parts = append(parts, fmt.Sprintf("%s=%s",
				keyColor.Sprint(terminal.Sanitize(key)),
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
		return valueColor.Sprint(terminal.Sanitize(fmt.Sprintf("%v", val)))
	}
}
