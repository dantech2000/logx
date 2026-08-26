package logging

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LogFormat represents different log formats we can handle
type LogFormat int

// Recognized log line formats.
const (
	FormatPlainText LogFormat = iota
	FormatJSON
	FormatBracketed
	FormatLogfmt
	FormatXML
)

// LogEntry represents a parsed log entry with all possible fields
type LogEntry struct {
	Level     LogLevel
	Message   string
	Format    LogFormat
	Logger    string
	Fields    map[string]any
	Timestamp time.Time
	RawLine   string // Original payload used as fallback display text.
	// LevelDetected reports whether Level came from the line itself (an explicit
	// level marker, level field, or HTTP status) rather than being defaulted.
	// Continuation lines of a multi-line entry have no level of their own, so this
	// is false for them, which lets consumers group them with the preceding entry.
	LevelDetected bool
}

// Common field mappings for different JSON log formats
var (
	// Level field names across different loggers
	jsonLevelFields = []string{
		"level",         // Common
		"severity",      // Google Cloud
		"log_level",     // Custom
		"loglevel",      // Custom
		"log.level",     // ECS/Winston
		"severity_text", // OpenTelemetry
		"severityText",  // OpenTelemetry
		"@level",        // Bunyan
		"levelname",     // Python logging
		"LEVEL",         // Some uppercase variants
	}

	// Message field names
	jsonMessageFields = []string{
		"message",  // Common
		"msg",      // Zap, Logrus
		"log",      // Docker
		"text",     // Custom
		"body",     // OpenTelemetry
		"@message", // Bunyan
		"MESSAGE",  // Systemd
	}

	// Timestamp field names
	jsonTimeFields = []string{
		"time",       // Common
		"timestamp",  // Common
		"@timestamp", // ELK
		"ts",         // Zap
		"Time",       // AWS CloudWatch
		"TIME",       // Some uppercase variants
		"datetime",   // Python logging
	}

	// logfmt key names recognized for the level, message, logger, and timestamp of
	// a logfmt line. They are kept as package vars (not inline literals) so config
	// `fields:` aliases registered via RegisterFieldAliases extend logfmt parsing
	// too, not just JSON. The logger list is not user-configurable.
	logfmtLevelFields   = []string{"level", "severity", "log_level", "lvl"}
	logfmtMessageFields = []string{"msg", "message", "log"}
	logfmtLoggerFields  = []string{"component", "logger", "logger_name", "source"}
	logfmtTimeFields    = []string{"time", "timestamp", "ts", "@timestamp"}

	// Time formats to try parsing
	timeFormats = []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02T15:04:05Z0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
		time.UnixDate,
		time.ANSIC,
	}
)

type logParser interface {
	Parse(line string) (LogEntry, bool)
}

var logParsers = []logParser{
	// Ordered from most structured to least structured. Plain text is the fallback.
	jsonLogParser{},
	yamlFlowParser{}, // brace-wrapped like JSON; tried right after it
	bracketedLogParser{},
	logfmtLogParser{},
	klogLogParser{},
	syslogParser{},
	accessLogParser{},
	xmlLogParser{}, // heuristic structured formats last, before the plain-text fallback
	csvLogParser{},
}

// kubeletTimestampPattern is the RFC3339 shape of a kubelet --timestamps prefix.
// It is the shared core of kubernetesTimestampPrefixRegex (below) and
// kubeletTimestampSepRegex (continuation.go) so the two cannot drift; they
// intentionally differ only in the separator they consume.
const kubeletTimestampPattern = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z`

// plainTextTimestampPattern is the shape of a timestamp inside a plain-text log
// line. Shared by the anchored and unanchored regexes below so they cannot drift.
const plainTextTimestampPattern = `\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`

var (
	kubernetesTimestampPrefixRegex = regexp.MustCompile(`^(` + kubeletTimestampPattern + `)\s+(.*)$`)
	// plainTextLeadingTimestampRegex anchors the entry's timestamp to the start of
	// the line, allowing an optional level token and/or square brackets ahead of
	// it so both "<time> LEVEL msg" and "LEVEL <time> msg" are recognized.
	//
	// Searching the whole line let a date mentioned in prose become the entry's
	// timestamp — "Scheduled next backup for 2026-12-25" was stamped with a date
	// six months in the future, which corrupts --timeline ordering, `ts`
	// predicates, and the .time field of --output json. A real entry timestamp is
	// a prefix by universal convention; a date further in is content. A trailing
	// separator (whitespace, `]`, `:`, or end of line) is required too, so a
	// value merely glued to the match ("...Zerror") is not extracted as the
	// entry's time and then also left behind in the rendered message.
	plainTextLeadingTimestampRegex = regexp.MustCompile(
		`^[\s\[]*(?:(?i:DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL)\]?\s+\[?)?(` +
			plainTextTimestampPattern + `)(?:[\s\]:]|$)`)
	// Level detection in plain text uses two regexes, because the cost of a
	// mistake is asymmetric. Reading prose as ERROR or WARN is over-inclusion: the
	// line is still shown. Reading it as TRACE is the one direction that loses
	// data, since TRACE sits below the default --level DEBUG — and LevelTracker
	// then carries that TRACE onto the indented frames beneath it, swallowing an
	// entire stack trace.
	//
	// plainTextLeadingLevelRegex matches a level in the position a real marker
	// occupies: the head of the line, optionally after a timestamp and/or
	// brackets, and followed by a separator. Every level, TRACE included, is
	// trusted here. Its optional leading timestamp carries the same trailing-
	// separator requirement as plainTextLeadingTimestampRegex, so the two
	// regexes cannot disagree about where a timestamp ends: without it,
	// "2026-06-24T10:00:00Zerror boom" was rejected as a timestamp (glued) yet
	// still read as <time><level> and classified ERROR from the prose "error".
	// With it, both regexes reject the glued prefix, the fallback scan's
	// alphanumeric guard keeps "Zerror" from matching either, and the line stays
	// an unclassified DEBUG with its content untouched.
	plainTextLeadingLevelRegex = regexp.MustCompile(
		`^[\s\[]*(?:` + plainTextTimestampPattern + `(?:[\s\]:]|$))?[\s\]\[]*` +
			`((?i:TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL))(?:[\s\]:]|$)`)

	// plainTextLevelRegex is the fallback scan over the rest of the line, for
	// lines that name their level mid-message. TRACE is deliberately absent: a
	// non-leading "trace" is overwhelmingly prose ("stack trace follows", "STACK
	// TRACE follows", "Blocked TRACE request", "TRACE-ID: ..."), and honoring it
	// hides the line. Excluding it also lets a genuine level later on such a line
	// win, instead of losing to a leftmost prose "TRACE".
	plainTextLevelRegex = regexp.MustCompile(`(^|[^[:alnum:]_])((?i:DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL))([^[:alnum:]_]|$)`)
	// logfmtKeyRegex matches identifier-like logfmt keys so that tokens such as a
	// trailing URL ("…/api?foo=bar") are not mistaken for key=value fields.
	logfmtKeyRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

var httpContextFields = []string{
	"method",
	"path",
	"route",
	"uri",
	"url",
	"request",
	"request.method",
	"request.path",
	"requestMethod",
	"requestPath",
	"controller",
	"action",
	"http.method",
	"http.route",
	"http.target",
	"http.url",
}

var httpStatusFields = []string{
	"status",
	"status_code",
	"statusCode",
	"http.status_code",
	"response.status",
	"response.status_code",
	"responseStatusCode",
}

type jsonLogParser struct{}

func (jsonLogParser) Parse(line string) (LogEntry, bool) {
	trimmedLine := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmedLine, "{") || !strings.HasSuffix(trimmedLine, "}") {
		return LogEntry{}, false
	}

	var data map[string]any
	decoder := json.NewDecoder(strings.NewReader(trimmedLine))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return LogEntry{}, false
	}
	// Decode reads only the first JSON value. Accepting trailing content made
	// "{...} garbage" parse with the garbage dropped, and "{...}{...}" lose the
	// second object entirely — its level and message silently discarded.
	// Rejecting lets the line fall through to the plain-text parser, which keeps
	// all of it as the message.
	if decoder.More() {
		return LogEntry{}, false
	}
	return parseJSONLog(trimmedLine, data), true
}

type bracketedLogParser struct{}

var bracketedLogRegex = regexp.MustCompile(`^\[([^\]]+)\]\s+\[([^\]]+)\]\s+\[([^\]]+)\]\s*(.*)$`)

func (bracketedLogParser) Parse(line string) (LogEntry, bool) {
	matches := bracketedLogRegex.FindStringSubmatch(line)
	if matches == nil {
		return LogEntry{}, false
	}

	level, err := ParseLogLevel(matches[2])
	if err != nil {
		return LogEntry{}, false
	}

	message, fields := splitTrailingFields(matches[4])
	entry := LogEntry{
		Level:         level,
		LevelDetected: true,
		Message:       message,
		Format:        FormatBracketed,
		Logger:        matches[3],
		Fields:        fields,
		RawLine:       line,
	}
	if ts, err := parseTimestamp(matches[1]); err == nil {
		entry.Timestamp = ts
	}
	return entry, true
}

type logfmtLogParser struct{}

func (logfmtLogParser) Parse(line string) (LogEntry, bool) {
	fields, ok := parseLogfmtFields(line)
	if !ok {
		return LogEntry{}, false
	}

	entry := LogEntry{
		Level:   DEBUG,
		Format:  FormatLogfmt,
		Fields:  fields,
		RawLine: line,
	}
	if levelValue, ok := firstStringField(fields, logfmtLevelFields...); ok {
		if level, err := ParseLogLevel(levelValue); err == nil {
			entry.Level = level
			entry.LevelDetected = true
		}
	}
	if message, ok := firstStringField(fields, logfmtMessageFields...); ok {
		entry.Message = message
	} else {
		entry.Message = line
	}
	if logger, ok := firstStringField(fields, logfmtLoggerFields...); ok {
		entry.Logger = logger
	}
	if timeValue, ok := firstStringField(fields, logfmtTimeFields...); ok {
		if ts, err := parseTimestamp(timeValue); err == nil {
			entry.Timestamp = ts
		}
	}

	return entry, true
}

type klogLogParser struct{}

// klogLineRegex matches the klog/glog header used by Kubernetes components:
//
//	Lmmdd hh:mm:ss.uuuuuu threadid file:line] message
//
// e.g. "E0624 10:00:00.123456   12 server.go:42] failed to sync". The level
// letter (I/W/E/F) carries the severity; the year is omitted by klog, so the
// timestamp is left for the caller (the timeline supplies kubelet timestamps).
var klogLineRegex = regexp.MustCompile(`^([IWEF])\d{4} \d{2}:\d{2}:\d{2}\.\d+\s+\d+\s+\S+:\d+\]\s?(.*)$`)

func (klogLogParser) Parse(line string) (LogEntry, bool) {
	matches := klogLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return LogEntry{}, false
	}
	return LogEntry{
		Level:         klogLevel(matches[1]),
		LevelDetected: true,
		Message:       matches[2],
		Format:        FormatPlainText,
		RawLine:       line,
	}, true
}

type syslogParser struct{}

// syslogPriorityRegex matches the leading "<PRI>" of a syslog line (RFC 3164 or
// RFC 5424). PRI = facility*8 + severity; severity (PRI mod 8) gives the level.
var syslogPriorityRegex = regexp.MustCompile(`^<(\d{1,3})>`)

func (syslogParser) Parse(line string) (LogEntry, bool) {
	matches := syslogPriorityRegex.FindStringSubmatch(line)
	if matches == nil {
		return LogEntry{}, false
	}
	pri, err := strconv.Atoi(matches[1])
	if err != nil || pri > 191 { // valid PRI is 0..191 (facility 0..23, severity 0..7)
		return LogEntry{}, false
	}
	return LogEntry{
		Level:         syslogSeverityLevel(pri % 8),
		LevelDetected: true,
		Message:       strings.TrimSpace(line[len(matches[0]):]),
		Format:        FormatPlainText,
		RawLine:       line,
	}, true
}

// syslogSeverityLevel maps an RFC 5424 severity (0..7) to a LogLevel.
func syslogSeverityLevel(severity int) LogLevel {
	switch {
	case severity <= 3: // Emergency, Alert, Critical, Error
		return ERROR
	case severity == 4: // Warning
		return WARN
	case severity <= 6: // Notice, Informational
		return INFO
	default: // Debug (7)
		return DEBUG
	}
}

type accessLogParser struct{}

// accessLogStatusRegex matches the quoted request line (including its HTTP
// version) followed by the status code, as used by the Apache/nginx common and
// combined formats and by Envoy, e.g. `"GET /path HTTP/1.1" 404` or
// `"GET /api HTTP/1.1" 503 UF ...`. Requiring the HTTP method and the `HTTP/x`
// protocol keeps the match specific enough to avoid false positives on prose that
// merely contains a quoted phrase and a number.
var accessLogStatusRegex = regexp.MustCompile(`"(?:GET|HEAD|POST|PUT|DELETE|PATCH|OPTIONS|CONNECT|TRACE) [^"]*HTTP/[0-9.]+"\s+(\d{3})\b`)

func (accessLogParser) Parse(line string) (LogEntry, bool) {
	matches := accessLogStatusRegex.FindStringSubmatch(line)
	if matches == nil {
		return LogEntry{}, false
	}
	status, err := strconv.Atoi(matches[1])
	if err != nil {
		return LogEntry{}, false
	}
	level, ok := statusClassLevel(status)
	if !ok {
		return LogEntry{}, false
	}
	// The whole line is the useful record (client, request, status, size), and it
	// carries its own non-ISO date, so display it as-is with the derived level.
	return LogEntry{
		Level:         level,
		LevelDetected: true,
		Message:       line,
		Format:        FormatPlainText,
		RawLine:       line,
	}, true
}

func klogLevel(letter string) LogLevel {
	switch letter {
	case "W":
		return WARN
	case "E":
		return ERROR
	case "F":
		return FATAL
	default: // "I"
		return INFO
	}
}

// jsonLoggerLabel returns the label shown in brackets for a JSON entry.
//
// An explicit logger/component/source field is the application's own name for
// the emitter, and it is what `--where logger==` matches (predicate.go resolves
// a real field ahead of the virtual key). Displaying detectLoggerLabel's guess
// instead made the two disagree in the most confusing possible way: the label on
// screen was not filterable, and the value that *was* filterable never appeared.
// A line carrying logger="payment-service" rendered as [logrus], and
// `--where logger==logrus` then matched nothing.
//
// The field list is loggerKeys, shared with the predicate engine, so display and
// filtering cannot drift apart again.
func jsonLoggerLabel(data map[string]any) string {
	if s, ok := firstStringField(data, loggerKeys...); ok && s != "" {
		return s
	}
	return detectLoggerLabel(data)
}

// detectLoggerLabel guesses which logging *library* produced an object from its
// shape. It is only a fallback for when the line names no logger of its own.
func detectLoggerLabel(data map[string]any) string {
	switch {
	case data["caller"] != nil && data["ts"] != nil:
		return "zap"
	case data["@level"] != nil && data["@timestamp"] != nil:
		return "bunyan"
	case data["log.level"] != nil:
		return "winston"
	case data["levelname"] != nil:
		return "python"
	case data["stream"] != nil && data["log"] != nil:
		return "docker"
	case data["pid"] != nil && data["level"] != nil && data["time"] != nil:
		return "pino"
	case data["level"] != nil && data["msg"] != nil:
		return "logrus"
	default:
		return ""
	}
}

// parseTimestamp attempts to parse a timestamp string using various formats
func parseTimestamp(timeStr string) (time.Time, error) {
	for _, format := range timeFormats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

func parseJSONLog(line string, data map[string]any) LogEntry {
	logger := jsonLoggerLabel(data)
	entry := LogEntry{
		Format:  FormatJSON,
		Fields:  data,
		Logger:  logger,
		RawLine: line,
	}
	entry.Level, entry.LevelDetected = parseJSONLevel(data)
	entry.Message = parseJSONMessage(line, data)
	entry.Timestamp = parseJSONTimestamp(data)

	return entry
}

// parseJSONLevel resolves the level from a JSON log object and reports whether a
// level was actually found (via a level field or HTTP status) rather than
// defaulted to DEBUG.
func parseJSONLevel(data map[string]any) (LogLevel, bool) {
	for _, field := range jsonLevelFields {
		if val, ok := fieldValue(data, field); ok {
			if level, err := ParseLogLevel(stringValue(val)); err == nil {
				return level, true
			}
		}
	}
	if level, ok := parseHTTPStatusLevel(data); ok {
		return level, true
	}
	return DEBUG, false
}

// parseJSONMessage picks the message from the first message alias that carries
// actual text. Taking the first alias merely *present* meant that a line like
// {"message":"","msg":"database connection refused"} rendered as an empty
// message: the empty "message" won, and "msg" was then suppressed from the
// printed fields as already-surfaced. That shape is what log enrichers (Fluent
// Bit, Vector) produce when they add a normalized key alongside the original, so
// the result was total content loss on a common pipeline.
func parseJSONMessage(line string, data map[string]any) string {
	for _, field := range jsonMessageFields {
		if val, ok := fieldValue(data, field); ok {
			if s := stringValue(val); s != "" {
				return s
			}
		}
	}

	if err, ok := data["error"]; ok {
		if s := stringValue(err); s != "" {
			return s
		}
	}
	return line
}

func parseJSONTimestamp(data map[string]any) time.Time {
	for _, field := range jsonTimeFields {
		if val, ok := fieldValue(data, field); ok {
			if numTime, ok := val.(float64); ok {
				return parseUnixTimestamp(numTime)
			}
			if numTime, ok := val.(json.Number); ok {
				if ts, ok := parseUnixTimestampString(numTime.String()); ok {
					return ts
				}
			}
			// yaml.v2 decodes integer scalars as int, not json.Number; without
			// these cases a YAML flow map's epoch timestamp was silently ignored.
			switch numTime := val.(type) {
			case int:
				return parseUnixTimestampInt(int64(numTime))
			case int64:
				return parseUnixTimestampInt(numTime)
			}
			if timeStr, ok := val.(string); ok {
				if ts, ok := parseStringTimestamp(timeStr); ok {
					return ts
				}
				if ts, err := parseTimestamp(timeStr); err == nil {
					return ts
				}
			}
		}
	}
	return time.Time{}
}

// parseStringTimestamp interprets a timestamp field's string value as either
// epoch seconds/millis or an already-formatted time.
//
// The epoch interpretation runs second because it is the more dangerous guess:
// an 8-digit run that is also a valid calendar date ("20260624") parses as both,
// but as epoch seconds it always lands in 1970–1973, silently stamping every
// such entry half a century into the past. A valid YYYYMMDD date therefore wins;
// anything that fails the date layout (a real epoch value like "1750759200", or
// "20261399", which is no month at all) still parses numerically below.
func parseStringTimestamp(value string) (time.Time, bool) {
	if len(value) == 8 && isAllDigits(value) {
		if ts, err := time.Parse("20060102", value); err == nil {
			return ts, true
		}
	}
	return parseUnixTimestampString(value)
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseUnixTimestamp(value float64) time.Time {
	return parseUnixTimestampInt(int64(value))
}

func parseUnixTimestampInt(timestamp int64) time.Time {
	switch {
	case timestamp >= 1e18:
		return time.Unix(0, timestamp)
	case timestamp >= 1e15:
		return time.Unix(0, timestamp*int64(time.Microsecond))
	case timestamp >= 1e11:
		return time.Unix(0, timestamp*int64(time.Millisecond))
	default:
		return time.Unix(timestamp, 0)
	}
}

func parseUnixTimestampString(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parseUnixTimestampInt(timestamp), true
	}
	timestamp, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return time.Time{}, false
	}
	return parseUnixTimestamp(timestamp), true
}

func parseHTTPStatusLevel(data map[string]any) (LogLevel, bool) {
	if !hasHTTPContext(data) {
		return DEBUG, false
	}

	statusValue, ok := firstField(data, httpStatusFields...)
	if !ok {
		return DEBUG, false
	}

	status, ok := toInt(statusValue)
	if !ok {
		return DEBUG, false
	}

	return statusClassLevel(status)
}

// statusClassLevel maps an HTTP status code to a level by class. 5xx is an error,
// 4xx a warning. 1xx/2xx/3xx are successful/informational requests and are
// intentionally classified as DEBUG so high-volume access logs stay out of the
// default INFO view; raise verbosity to --level DEBUG to see them. The bool is
// false for values outside the valid status range (not a real status).
func statusClassLevel(status int) (LogLevel, bool) {
	switch {
	case status >= 500:
		return ERROR, true
	case status >= 400:
		return WARN, true
	case status >= 100:
		return DEBUG, true
	default:
		return DEBUG, false
	}
}

func hasHTTPContext(data map[string]any) bool {
	return hasAnyField(data, httpContextFields)
}

// hasAnyField reports whether data has a value for any of keys, dot-path aware
// (e.g. "request.method" walks into a nested map). Shared by hasHTTPContext and
// the XML/CSV format detectors so both use the same field-presence semantics.
func hasAnyField(data map[string]any, keys []string) bool {
	for _, key := range keys {
		if _, ok := fieldValueExact(data, key); ok {
			return true
		}
	}
	return false
}

// parsePlainTextLog parses a plain text log entry
func parsePlainTextLog(line string) LogEntry {
	entry := LogEntry{
		Level:   DEBUG,
		Format:  FormatPlainText,
		RawLine: line,
	}

	// Extract the timestamp only from the head of the line, so a date appearing
	// in the message body is treated as content rather than as the entry's time.
	timeStr := ""
	if m := plainTextLeadingTimestampRegex.FindStringSubmatch(line); m != nil {
		timeStr = m[1]
	}
	if timeStr != "" {
		if ts, err := parseTimestamp(timeStr); err == nil {
			entry.Timestamp = ts
		}
	}

	// Try a leading level marker first (trusted for every level), then fall back
	// to scanning the rest of the line (which never yields TRACE).
	levelStr := ""
	if m := plainTextLeadingLevelRegex.FindStringSubmatch(line); m != nil {
		levelStr = m[1]
	} else if m := plainTextLevelRegex.FindStringSubmatch(line); m != nil {
		levelStr = m[2]
	}
	if levelStr != "" {
		if level, err := ParseLogLevel(levelStr); err == nil {
			entry.Level = level
			entry.LevelDetected = true
		} else {
			levelStr = ""
		}
	}

	// Build the display message by removing a leading timestamp/level that logx
	// already prints as its own [timestamp] [LEVEL] prefix, so they are not shown
	// twice. Only a leading occurrence is stripped, so message content is safe.
	entry.Message = stripLeadingMeta(line, timeStr, levelStr)
	return entry
}

// stripLeadingMeta removes a leading timestamp and/or level token (each optionally
// wrapped in [ ]) from the start of a plaintext line. If stripping would leave
// nothing, the trimmed original line is returned.
func stripLeadingMeta(line, timeStr, levelStr string) string {
	s := line
	// Try both orders. Loggers disagree on whether the level or the timestamp
	// comes first, and stripping time-then-level once meant a "LEVEL <time> msg"
	// line kept its timestamp in the message — printed again right after the
	// [timestamp] prefix logx had already rendered from it. Two passes are enough
	// to consume two tokens in either order.
	for range 2 {
		if timeStr != "" {
			s = stripPrefixToken(s, timeStr)
		}
		if levelStr != "" {
			s = stripPrefixToken(s, levelStr)
		}
	}
	if strings.TrimSpace(s) == "" {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(s)
}

// stripPrefixToken removes tok from the start of s, allowing it to be wrapped in
// square brackets and surrounded by spaces. If s does not begin with tok (after
// leading whitespace), s is returned unchanged.
func stripPrefixToken(s, tok string) string {
	rest := strings.TrimLeft(s, " \t")
	if bracketed := "[" + tok + "]"; strings.HasPrefix(rest, bracketed) {
		return strings.TrimLeft(rest[len(bracketed):], " \t")
	}
	if strings.HasPrefix(rest, tok) {
		after := rest[len(tok):]
		if after == "" || after[0] == ' ' || after[0] == '\t' {
			return strings.TrimLeft(after, " \t")
		}
	}
	return s
}

func splitTrailingFields(message string) (string, map[string]any) {
	// Fast path: without a '=' there can be no key=value fields, so skip the
	// tokenization (this runs for every bracketed line).
	if !strings.Contains(message, "=") {
		return strings.TrimSpace(message), nil
	}

	parts := strings.Fields(message)
	var fields map[string]any
	fieldStart := len(parts)

	for i, part := range slices.Backward(parts) {
		key, value, ok := simpleField(part)
		if !ok {
			break
		}
		if fields == nil {
			fields = make(map[string]any)
		}
		// Backward iteration visits the last occurrence first; skipping repeats
		// keeps last-wins semantics, matching the logfmt parser.
		if _, exists := fields[key]; !exists {
			fields[key] = strings.Trim(value, `"`)
		}
		fieldStart = i
	}

	if len(fields) == 0 {
		return strings.TrimSpace(message), nil
	}
	return strings.Join(parts[:fieldStart], " "), fields
}

// simpleField reports whether value looks like a "key=value" field and, if so,
// returns the split key/value in the same pass (the caller would otherwise redo
// this same Cut just to get the pieces it already confirmed exist).
func simpleField(value string) (key, fieldValue string, ok bool) {
	key, fieldValue, ok = strings.Cut(value, "=")
	if !ok || key == "" || fieldValue == "" {
		return "", "", false
	}
	// Only treat identifier-like keys as fields; this keeps URLs and paths that
	// happen to contain '=' (e.g. query strings) inside the message text.
	if !logfmtKeyRegex.MatchString(key) {
		return "", "", false
	}
	return key, fieldValue, true
}

func parseLogfmtFields(line string) (map[string]any, bool) {
	fields := make(map[string]any)
	i := 0

	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}

		keyStart := i
		for i < len(line) && line[i] != '=' && line[i] != ' ' {
			i++
		}
		if i >= len(line) || line[i] != '=' || i == keyStart {
			return nil, false
		}
		key := line[keyStart:i]
		// Validate the key the same way splitTrailingFields does. Without this, any
		// line whose whitespace-separated tokens all happen to contain '=' is
		// claimed as logfmt — a bare URL with a query string, or a CSV row whose
		// message contains '='. Because logfmt is tried before klog/syslog/access/
		// xml/csv, such a false match blocks the parser that would have read the
		// line correctly and its real level is lost.
		if !logfmtKeyRegex.MatchString(key) {
			return nil, false
		}
		i++

		value := ""
		if i < len(line) && line[i] == '"' {
			value, i = scanQuotedLogfmtValue(line, i)
		} else {
			valueStart := i
			for i < len(line) && line[i] != ' ' {
				i++
			}
			value = line[valueStart:i]
		}
		fields[key] = value
	}

	return fields, len(fields) > 0
}

// scanQuotedLogfmtValue reads a double-quoted logfmt value starting at the
// opening quote line[i] and returns the decoded value plus the index just past
// the closing quote (or end of line, for an unterminated value).
//
// Escaped backslash and quote collapse to the bare byte; any other escape keeps
// both characters. Dropping the backslash unconditionally corrupted values like
// "C:\temp" into "C:temp", and decoding \n would smuggle a newline into what
// must stay a single output line.
func scanQuotedLogfmtValue(line string, i int) (string, int) {
	i++ // opening quote
	var builder strings.Builder
	for i < len(line) {
		if line[i] == '\\' && i+1 < len(line) {
			i++
			if line[i] == '\\' || line[i] == '"' {
				builder.WriteByte(line[i])
			} else {
				builder.WriteByte('\\')
				builder.WriteByte(line[i])
			}
			i++
			continue
		}
		if line[i] == '"' {
			i++
			break
		}
		builder.WriteByte(line[i])
		i++
	}
	return builder.String(), i
}

func firstStringField(fields map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := fields[name]; ok {
			return stringValue(value), true
		}
	}
	return "", false
}

// stringValue returns v's string form, skipping the fmt machinery for the
// common case where the value already is a string (logfmt and XML parsers only
// ever produce string values; JSON messages usually are strings too).
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	// A JSON null has no textual form. Without this, fmt renders it as "<nil>",
	// which was printed as if it were the log's own content — fabricating data
	// (e.g. {"message":null} displayed the message "<nil>").
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// fieldValueFold is the case-insensitive fallback for fieldValue. Loggers
// disagree on capitalization — Serilog and most .NET stacks emit "Level",
// "Msg", and "Timestamp" — and enumerating every casing variant in the
// field-name lists does not scale (jsonLevelFields already carried both "level"
// and "LEVEL", and still missed "Level"). Getting this wrong is not merely a
// filtering annoyance: an unrecognized level field left the entry at the DEBUG
// default, so a Serilog ERROR line was invisible at --level ERROR.
//
// Exact match always wins, so this only runs on a miss. When several keys differ
// only by case, the lexicographically smallest wins, so the result never depends
// on Go's randomized map iteration order. No allocation on either path.
func fieldValueFold(fields map[string]any, name string) (any, bool) {
	var bestKey string
	var best any
	found := false
	for k, v := range fields {
		if !strings.EqualFold(k, name) {
			continue
		}
		if !found || k < bestKey {
			bestKey, best, found = k, v, true
		}
	}
	return best, found
}

func firstField(fields map[string]any, names ...string) (any, bool) {
	for _, name := range names {
		if value, ok := fieldValue(fields, name); ok {
			return value, true
		}
	}
	return nil, false
}

// fieldValue resolves a field name, falling back to a case-insensitive match so
// loggers that capitalize differently still resolve. Use fieldValueExact for
// pure heuristics, where the extra scan is not worth its cost.
func fieldValue(fields map[string]any, name string) (any, bool) {
	if value, ok := fieldValueExact(fields, name); ok {
		return value, true
	}
	if strings.Contains(name, ".") {
		return nil, false
	}
	return fieldValueFold(fields, name)
}

// fieldValueExact resolves a field name by exact match only, following a dotted
// name as a path into nested maps.
//
// This is the variant used by the HTTP-shape heuristics (hasAnyField over
// httpContextFields), which probe ~16 names purely to decide whether a line
// looks like an access log. Running a case-insensitive scan per miss there cost
// noticeably more on wide JSON objects than the heuristic is worth, while the
// lookups that actually matter — level, message, and timestamp — keep the
// fallback via fieldValue.
func fieldValueExact(fields map[string]any, name string) (any, bool) {
	if value, ok := fields[name]; ok {
		return value, true
	}
	// Only a dotted name can still resolve, as a path into nested maps. This
	// runs on every probe of a missing key (level/message/time candidates,
	// --where, --fields), so avoid any allocation on the miss path.
	if !strings.Contains(name, ".") {
		return nil, false
	}

	current := any(fields)
	rest := name
	for {
		part, remainder, more := strings.Cut(rest, ".")
		currentFields, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := currentFields[part]
		if !ok {
			return nil, false
		}
		current = value
		if !more {
			return current, true
		}
		rest = remainder
	}
}

// ParseLogEntry parses a single log line, trying each known format parser in
// order and falling back to plain-text parsing if none match.
//
// Parsing is stateless and line-oriented: each line is classified independently,
// and LevelDetected records whether a level was actually found. Multi-line entries
// (e.g. a stack trace) are reassembled by the caller: an indented continuation
// line has no level of its own, so a LevelTracker carries the preceding entry's
// level to it. Flush-left lines without a detected level remain independent, so
// the one case still classified as DEBUG is an unindented continuation line such
// as a Java exception-class header.
func ParseLogEntry(line string) LogEntry {
	for _, parser := range logParsers {
		if entry, ok := parser.Parse(line); ok {
			return entry
		}
	}
	return parsePlainTextLog(line)
}

// ParseKubernetesLogEntry parses a log line that may carry a kubelet RFC3339
// timestamp prefix (as emitted with --timestamps), using that timestamp when
// present and otherwise delegating to ParseLogEntry.
func ParseKubernetesLogEntry(line string) LogEntry {
	if timestamp, payload, ok := splitKubernetesTimestampPrefix(line); ok {
		entry := ParseLogEntry(payload)
		entry.Timestamp = timestamp
		return entry
	}
	return ParseLogEntry(line)
}

func splitKubernetesTimestampPrefix(line string) (time.Time, string, bool) {
	matches := kubernetesTimestampPrefixRegex.FindStringSubmatch(line)
	if matches == nil {
		return time.Time{}, "", false
	}
	timestamp, err := parseTimestamp(matches[1])
	if err != nil {
		return time.Time{}, "", false
	}
	return timestamp, matches[2], true
}

// ParseLogLevel parses both string and numeric log levels
func ParseLogLevel(level string) (LogLevel, error) {
	level = strings.TrimSpace(level)
	if numLevel, err := strconv.Atoi(level); err == nil {
		return parseNumericLogLevel(numLevel), nil
	}
	if numLevel, err := strconv.ParseFloat(level, 64); err == nil && float64(int(numLevel)) == numLevel {
		return parseNumericLogLevel(int(numLevel)), nil
	}

	normalizedLevel := strings.ToUpper(level)
	// Handle common variations
	switch {
	case normalizedLevel == "TRACE" || normalizedLevel == "VERBOSE" || normalizedLevel == "FINEST":
		return TRACE, nil
	case strings.HasPrefix(normalizedLevel, "DEBUG") || normalizedLevel == "FINE":
		return DEBUG, nil
	case strings.HasPrefix(normalizedLevel, "INFO") || normalizedLevel == "NOTICE":
		return INFO, nil
	case strings.HasPrefix(normalizedLevel, "WARN"):
		return WARN, nil
	case normalizedLevel == "FATAL" || normalizedLevel == "PANIC":
		return FATAL, nil
	case strings.HasPrefix(normalizedLevel, "ERR") || normalizedLevel == "CRITICAL" || normalizedLevel == "CRIT":
		return ERROR, nil
	default:
		return DEBUG, fmt.Errorf("invalid log level: %s", level)
	}
}

// parseNumericLogLevel maps a numeric level to a LogLevel. It targets the two
// numbering schemes common in the Go/Kubernetes ecosystem this tool serves:
//
//   - zap-style small integers, where Info=0, Warn=1, Error=2 (handled below as
//     explicit cases; zap's negative Debug=-1 and small DPanic/Panic/Fatal codes
//     3..5 fall through to the <10 DEBUG bucket);
//   - bunyan/pino-style decades, where Trace=10, Debug=20, Info=30, Warn=40,
//     Error=50, Fatal=60 (handled by the range checks).
//
// Note: syslog and OpenTelemetry severity numbers invert this (0/low = most
// severe), which is irreconcilable with zap's 0=Info without per-log context.
// We deliberately favor the zap convention; OTel/syslog producers should emit a
// textual severity field (severity_text), which the string path handles.
func parseNumericLogLevel(level int) LogLevel {
	switch level {
	case 0:
		return INFO
	case 1:
		return WARN
	case 2:
		return ERROR
	}

	switch {
	case level < 10:
		return DEBUG
	case level < 20:
		return TRACE
	case level < 30:
		return DEBUG
	case level < 40:
		return INFO
	case level < 50:
		return WARN
	case level < 60:
		return ERROR
	default:
		return FATAL
	}
}

// isInStringSet reports whether key is in a set built by buildStringSet,
// ignoring case. strings.ToLower returns its argument unchanged for an
// all-lowercase ASCII key, so the common case does not allocate.
func isInStringSet(set map[string]bool, key string) bool {
	return set[strings.ToLower(key)]
}

func buildStringSet(groups ...[]string) map[string]bool {
	size := 0
	for _, group := range groups {
		size += len(group)
	}

	// Keys are stored lowercased and looked up through isInStringSet, so the
	// display-exclusion sets recognize the same casing variants that field lookup
	// does. Without this, a Serilog line was parsed correctly but then printed its
	// message twice — once as the message, once as an ordinary "Msg=" field —
	// because the exclusion check was exact-match while the lookup was not.
	set := make(map[string]bool, size)
	for _, group := range groups {
		for _, value := range group {
			set[strings.ToLower(value)] = true
		}
	}
	return set
}
