package logging

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OutputFormat selects how the pipeline renders each kept entry.
type OutputFormat int

// Output formats: Text is the colorized human format; JSON emits one normalized
// JSON object per line (NDJSON) for piping into jq and other tools.
const (
	OutputText OutputFormat = iota
	OutputJSON
)

// ParseOutputFormat parses an --output value.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text", "plain":
		return OutputText, nil
	case "json", "ndjson":
		return OutputJSON, nil
	default:
		return OutputText, fmt.Errorf("unknown output format %q (valid: text, json)", s)
	}
}

// formatNames maps a LogFormat to a stable string for JSON output.
var formatNames = map[LogFormat]string{
	FormatPlainText: "text",
	FormatJSON:      "json",
	FormatBracketed: "bracketed",
	FormatLogfmt:    "logfmt",
}

// jsonEntry is the normalized shape emitted by --output json. It is intentionally
// stable so downstream tooling can rely on it regardless of the source format.
type jsonEntry struct {
	Time    string         `json:"time,omitempty"`
	Level   string         `json:"level"`
	Logger  string         `json:"logger,omitempty"`
	Message string         `json:"message,omitempty"`
	Format  string         `json:"format"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// MarshalEntryJSON renders a parsed entry as a single normalized JSON object.
// json.Marshal escapes control bytes (so no raw ANSI/escape leaks) and replaces
// invalid UTF-8, keeping the output safe to print while preserving data.
func MarshalEntryJSON(entry LogEntry) string {
	out := jsonEntry{
		Level:   entry.Level.String(),
		Logger:  entry.Logger,
		Message: entry.Message,
		Format:  formatNames[entry.Format],
		Fields:  extraFields(entry.Fields),
	}
	if entry.Message == entry.RawLine {
		// The parser fell back to the raw line as the message; don't echo it.
		out.Message = ""
	}
	if !entry.Timestamp.IsZero() {
		out.Time = entry.Timestamp.UTC().Format(time.RFC3339)
	}
	return marshalLine(out)
}

// marshalProjectedJSON renders only the requested (pre-classified) keys as a
// flat JSON object, the JSON counterpart of the --fields projection.
func marshalProjectedJSON(entry LogEntry, fields []projectedField) string {
	obj := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := resolveByKind(entry, f.kind, f.key); ok {
			obj[f.key] = v
		}
	}
	return marshalLine(obj)
}

// extraFields returns the structured fields minus the ones already surfaced as
// dedicated columns (level/time/message), so JSON output isn't redundant.
func extraFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if jsonFormattedFieldExclusions[k] || isJSONMessageField(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func marshalLine(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
