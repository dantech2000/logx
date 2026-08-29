package logging

import (
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

// This file adds parsers for a few less-common but real single-line log bodies
// that would otherwise fall through to plain text: flow-style YAML maps, XML
// elements, and timestamped CSV rows. Each is deliberately conservative so prose
// that merely contains a brace, an angle bracket, or commas is not misclassified.

// --- YAML flow maps: {level: info, msg: "hi"} -----------------------------

type yamlFlowParser struct{}

func (yamlFlowParser) Parse(line string) (LogEntry, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return LogEntry{}, false
	}
	var data map[string]any
	// A decoder (rather than Unmarshal) also lets us reject trailing content:
	// Decode reads one document, so "{...}{...}" or "{...} garbage" would
	// otherwise parse with everything after the first map silently dropped.
	decoder := yaml.NewDecoder(strings.NewReader(trimmed))
	if err := decoder.Decode(&data); err != nil || len(data) == 0 {
		return LogEntry{}, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return LogEntry{}, false
	}
	// Require a recognized level or message key so a stray "{note: ...}" or a code
	// snippet in a log line is not claimed as a structured entry.
	if !hasAnyField(data, jsonLevelFields) && !hasAnyField(data, jsonMessageFields) {
		return LogEntry{}, false
	}
	// yaml.v2 decodes nested maps as map[interface{}]interface{} even when the top
	// level is typed map[string]any. json.Marshal cannot encode that key type, so
	// an entry with any nested map failed to marshal and --output json emitted a
	// bare "{}" for the whole line. Normalizing here also makes dot-path --where
	// and --fields work, and keeps Go's map[...] syntax out of text output.
	normalizeYAMLMaps(data)

	// A YAML flow map is JSON-equivalent for our purposes, so reuse the JSON
	// field extraction (level/message/timestamp/logger).
	return parseJSONLog(trimmed, data), true
}

// normalizeYAMLMaps rewrites every nested map[interface{}]interface{} produced by
// yaml.v2 into a map[string]any, in place, so the result is JSON-encodable and
// indistinguishable from the same document parsed as JSON.
func normalizeYAMLMaps(data map[string]any) {
	for k, v := range data {
		data[k] = normalizeYAMLValue(v)
	}
}

func normalizeYAMLValue(v any) any {
	switch typed := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			out[stringValue(k)] = normalizeYAMLValue(val)
		}
		return out
	case map[string]any:
		normalizeYAMLMaps(typed)
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = normalizeYAMLValue(item)
		}
		return typed
	default:
		return v
	}
}

// --- XML elements: <log level="ERROR" ts="...">message</log> --------------

type xmlLogParser struct{}

var (
	// A single XML element with matching open/close tags, or a self-closing tag.
	xmlPairedRegex = regexp.MustCompile(`^<([A-Za-z][\w.-]*)((?:\s+[\w.:-]+="[^"]*")*)\s*>(.*)</([A-Za-z][\w.-]*)>$`)
	xmlSelfRegex   = regexp.MustCompile(`^<([A-Za-z][\w.-]*)((?:\s+[\w.:-]+="[^"]*")*)\s*/>$`)
	xmlAttrRegex   = regexp.MustCompile(`([\w.:-]+)="([^"]*)"`)
)

func (xmlLogParser) Parse(line string) (LogEntry, bool) {
	trimmed := strings.TrimSpace(line)
	var attrPart, inner string
	if m := xmlPairedRegex.FindStringSubmatch(trimmed); m != nil {
		if m[1] != m[4] { // open and close tags must match
			return LogEntry{}, false
		}
		attrPart, inner = m[2], strings.TrimSpace(m[3])
	} else if m := xmlSelfRegex.FindStringSubmatch(trimmed); m != nil {
		attrPart = m[2]
	} else {
		return LogEntry{}, false
	}

	fields := make(map[string]any)
	for _, a := range xmlAttrRegex.FindAllStringSubmatch(attrPart, -1) {
		fields[a[1]] = a[2]
	}
	// An element with no attributes and no inner text carries no log signal.
	if len(fields) == 0 && inner == "" {
		return LogEntry{}, false
	}

	// Level defaults to DEBUG to match every other parser's undetected-level
	// default. Leaving it as the zero value would mean TRACE, which sits below
	// the default --level and would hide every XML line lacking a level attribute.
	entry := LogEntry{Level: DEBUG, Format: FormatXML, Fields: fields, RawLine: line}
	if level, ok := parseJSONLevel(fields); ok {
		entry.Level, entry.LevelDetected = level, true
	}
	if logger, ok := firstStringField(fields, "logger", "name", "source", "category"); ok {
		entry.Logger = logger
	}
	if ts, ok := firstStringField(fields, "time", "timestamp", "ts", "date"); ok {
		if parsed, err := parseTimestamp(ts); err == nil {
			entry.Timestamp = parsed
		}
	}
	if inner != "" {
		entry.Message = inner
	} else if msg, ok := firstStringField(fields, "message", "msg"); ok {
		entry.Message = msg
	} else {
		entry.Message = line
	}
	return entry, true
}

// --- CSV rows: 2026-06-24T10:00:00Z,ERROR,svc,connection refused ----------

type csvLogParser struct{}

func (csvLogParser) Parse(line string) (LogEntry, bool) {
	if !strings.Contains(line, ",") {
		return LogEntry{}, false
	}
	reader := csv.NewReader(strings.NewReader(line))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	record, err := reader.Read()
	if err != nil || len(record) < 3 {
		return LogEntry{}, false
	}

	// Require a leading timestamp so ordinary comma-separated prose isn't claimed.
	ts, err := parseTimestamp(strings.TrimSpace(record[0]))
	if err != nil {
		return LogEntry{}, false
	}
	// Require an explicit level token somewhere in the row.
	levelIdx, level := -1, DEBUG
	for i := 1; i < len(record); i++ {
		if lvl, ok := levelToken(record[i]); ok {
			levelIdx, level = i, lvl
			break
		}
	}
	if levelIdx == -1 {
		return LogEntry{}, false
	}

	rest := make([]string, 0, len(record)-2)
	for i := 1; i < len(record); i++ {
		if i == levelIdx {
			continue
		}
		rest = append(rest, strings.TrimSpace(record[i]))
	}
	return LogEntry{
		Level:         level,
		LevelDetected: true,
		Message:       strings.Join(rest, ", "),
		Format:        FormatPlainText,
		Timestamp:     ts,
		RawLine:       line,
	}, true
}

// levelToken reports whether s is exactly a level keyword (not merely a number,
// which ParseLogLevel would also accept), returning the mapped level.
func levelToken(s string) (LogLevel, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "TRACE", "VERBOSE", "FINEST", "DEBUG", "FINE", "INFO", "INFORMATION", "NOTICE",
		"WARN", "WARNING", "ERROR", "ERR", "CRITICAL", "CRIT", "FATAL", "PANIC":
		lvl, err := ParseLogLevel(s)
		return lvl, err == nil
	default:
		return DEBUG, false
	}
}
