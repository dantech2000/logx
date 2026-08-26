package logging

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dantech2000/logx/internal/terminal"
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
	FormatXML:       "xml",
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
//
// Every string is run through terminal.Sanitize first. json.Marshal alone is not
// enough: it escapes only code points below 0x20, so DEL (0x7F), the C1 controls
// (U+0080–U+009F — a UTF-8 terminal maps U+009B to CSI, a working escape
// introducer), and Trojan-Source bidi overrides all passed through raw. NDJSON
// gets read by humans in a terminal as often as by jq, so it needs the same
// guarantee as the text renderer.
func MarshalEntryJSON(entry LogEntry) string {
	// The message is emitted even when it is just the raw line. Blanking it here
	// dropped the content of every unstructured log line — the most common pod-log
	// shape — because jsonEntry has no raw field to carry it instead.
	message := entry.Message
	if message == "" {
		message = entry.RawLine
	}

	out := jsonEntry{
		Level:   entry.Level.String(),
		Logger:  terminal.Sanitize(entry.Logger),
		Message: terminal.Sanitize(message),
		Format:  formatNames[entry.Format],
		Fields:  sanitizeJSONFields(extraFields(entry.Fields)),
	}
	if !entry.Timestamp.IsZero() {
		out.Time = entry.Timestamp.UTC().Format(time.RFC3339)
	}
	return marshalLine(out)
}

// marshalProjectedJSON renders only the requested (pre-classified) keys as a
// flat JSON object, the JSON counterpart of the --fields projection. It reports
// false when no requested key resolved, so the caller can skip the line instead
// of emitting a content-free "{}" for every non-matching entry.
func marshalProjectedJSON(entry LogEntry, fields []projectedField) (string, bool) {
	obj := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := resolveByKind(entry, f.kind, f.key); ok {
			obj[terminal.Sanitize(f.key)] = sanitizeJSONValue(v)
		}
	}
	if len(obj) == 0 {
		return "", false
	}
	return marshalLine(obj), true
}

// sanitizeJSONFields sanitizes a field map, preserving nil for omitempty.
func sanitizeJSONFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[terminal.Sanitize(k)] = sanitizeJSONValue(v)
	}
	return out
}

// sanitizeJSONValue recursively neutralizes control characters in strings and
// normalizes values json.Marshal cannot encode.
func sanitizeJSONValue(v any) any {
	switch typed := v.(type) {
	case string:
		return terminal.Sanitize(typed)
	case map[string]any:
		return sanitizeJSONFields(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = sanitizeJSONValue(item)
		}
		return out
	case float64:
		// NaN and ±Inf are not representable in JSON and make json.Marshal fail
		// for the whole object. A YAML-flow line carrying `.nan` therefore used to
		// be emitted as a bare "{}", losing level, message, timestamp and every
		// field. Render them as their textual form instead.
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return strconv.FormatFloat(typed, 'g', -1, 64)
		}
		return typed
	default:
		return v
	}
}

// extraFields returns the structured fields minus the ones already surfaced as
// dedicated columns (level/time/message), so JSON output isn't redundant.
func extraFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if isInStringSet(jsonFormattedFieldExclusions, k) || isJSONMessageField(k) {
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
		// Values are normalized by sanitizeJSONValue before they get here, so this
		// should be unreachable. Emit a visible marker rather than a bare "{}",
		// which silently discarded the entire entry — a single NaN field used to
		// erase level, message, timestamp and all.
		return `{"logx_error":"entry could not be encoded as JSON"}`
	}
	return string(b)
}
