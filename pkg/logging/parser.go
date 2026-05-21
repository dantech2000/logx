package logging

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LogFormat represents different log formats we can handle
type LogFormat int

const (
	FormatPlainText LogFormat = iota
	FormatJSON
	FormatBracketed
	FormatLogfmt
)

// LogEntry represents a parsed log entry with all possible fields
type LogEntry struct {
	Level     LogLevel
	Message   string
	Format    LogFormat
	Logger    string
	Fields    map[string]interface{}
	Timestamp time.Time
	RawLine   string // Original payload used as fallback display text.
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
	bracketedLogParser{},
	logfmtLogParser{},
}

var (
	kubernetesTimestampPrefixRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s+(.*)$`)
	plainTextTimestampRegex        = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	plainTextLevelRegex            = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])(DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|TRACE)([^[:alnum:]_]|$)`)
)

var httpContextFields = buildStringSet([]string{
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
})

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

	var data map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(trimmedLine))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
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
		Level:   level,
		Message: message,
		Format:  FormatBracketed,
		Logger:  matches[3],
		Fields:  fields,
		RawLine: line,
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
	if levelValue, ok := firstStringField(fields, "level", "severity", "log_level", "lvl"); ok {
		if level, err := ParseLogLevel(levelValue); err == nil {
			entry.Level = level
		}
	}
	if message, ok := firstStringField(fields, "msg", "message", "log"); ok {
		entry.Message = message
	} else {
		entry.Message = line
	}
	if logger, ok := firstStringField(fields, "component", "logger", "logger_name", "source"); ok {
		entry.Logger = logger
	}
	if timeValue, ok := firstStringField(fields, "time", "timestamp", "ts", "@timestamp"); ok {
		if ts, err := parseTimestamp(timeValue); err == nil {
			entry.Timestamp = ts
		}
	}

	return entry, true
}

// detectLoggerLabel returns a display label for known structured logging formats.
func detectLoggerLabel(data map[string]interface{}) string {
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

func parseJSONLog(line string, data map[string]interface{}) LogEntry {
	logger := detectLoggerLabel(data)
	entry := LogEntry{
		Format:  FormatJSON,
		Fields:  data,
		Logger:  logger,
		RawLine: line,
	}
	entry.Level = parseJSONLevel(data)
	entry.Message = parseJSONMessage(line, data)
	entry.Timestamp = parseJSONTimestamp(data)

	return entry
}

func parseJSONLevel(data map[string]interface{}) LogLevel {
	for _, field := range jsonLevelFields {
		if val, ok := fieldValue(data, field); ok {
			levelStr := fmt.Sprintf("%v", val)
			if level, err := ParseLogLevel(levelStr); err == nil {
				return level
			}
		}
	}
	if level, ok := parseHTTPStatusLevel(data); ok {
		return level
	}
	return DEBUG
}

func parseJSONMessage(line string, data map[string]interface{}) string {
	for _, field := range jsonMessageFields {
		if val, ok := fieldValue(data, field); ok {
			return fmt.Sprintf("%v", val)
		}
	}

	if err, ok := data["error"]; ok {
		return fmt.Sprintf("%v", err)
	}
	return line
}

func parseJSONTimestamp(data map[string]interface{}) time.Time {
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
			if timeStr, ok := val.(string); ok {
				if ts, ok := parseUnixTimestampString(timeStr); ok {
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

func parseHTTPStatusLevel(data map[string]interface{}) (LogLevel, bool) {
	if !hasHTTPContext(data) {
		return DEBUG, false
	}

	statusValue, ok := firstField(data, httpStatusFields...)
	if !ok {
		return DEBUG, false
	}

	var status int
	switch value := statusValue.(type) {
	case float64:
		status = int(value)
	case json.Number:
		parsedStatus, err := strconv.Atoi(value.String())
		if err != nil {
			return DEBUG, false
		}
		status = parsedStatus
	case string:
		parsedStatus, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return DEBUG, false
		}
		status = parsedStatus
	default:
		return DEBUG, false
	}

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

func hasHTTPContext(data map[string]interface{}) bool {
	for field := range httpContextFields {
		if _, ok := fieldValue(data, field); ok {
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

	// First try to extract timestamp
	if timeStr := plainTextTimestampRegex.FindString(line); timeStr != "" {
		if ts, err := parseTimestamp(timeStr); err == nil {
			entry.Timestamp = ts
		}
	}

	// Try to extract log level
	if matches := plainTextLevelRegex.FindStringSubmatch(line); matches != nil {
		if level, err := ParseLogLevel(matches[2]); err == nil {
			entry.Level = level
		}
	}

	// Use the original line as the message
	entry.Message = line
	return entry
}

func splitTrailingFields(message string) (string, map[string]interface{}) {
	parts := strings.Fields(message)
	fields := make(map[string]interface{})
	fieldStart := len(parts)

	for i := len(parts) - 1; i >= 0; i-- {
		if !isSimpleField(parts[i]) {
			break
		}
		key, value, _ := strings.Cut(parts[i], "=")
		fields[key] = strings.Trim(value, `"`)
		fieldStart = i
	}

	if len(fields) == 0 {
		return strings.TrimSpace(message), nil
	}
	return strings.Join(parts[:fieldStart], " "), fields
}

func isSimpleField(value string) bool {
	key, fieldValue, ok := strings.Cut(value, "=")
	if !ok || key == "" || fieldValue == "" {
		return false
	}
	return !strings.ContainsAny(key, " \t")
}

func parseLogfmtFields(line string) (map[string]interface{}, bool) {
	fields := make(map[string]interface{})
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
		i++

		value := ""
		if i < len(line) && line[i] == '"' {
			i++
			var builder strings.Builder
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					i++
					builder.WriteByte(line[i])
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
			value = builder.String()
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

func firstStringField(fields map[string]interface{}, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := fields[name]; ok {
			return fmt.Sprintf("%v", value), true
		}
	}
	return "", false
}

func firstField(fields map[string]interface{}, names ...string) (interface{}, bool) {
	for _, name := range names {
		if value, ok := fieldValue(fields, name); ok {
			return value, true
		}
	}
	return nil, false
}

func fieldValue(fields map[string]interface{}, name string) (interface{}, bool) {
	if value, ok := fields[name]; ok {
		return value, true
	}

	current := interface{}(fields)
	for _, part := range strings.Split(name, ".") {
		currentFields, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, ok := currentFields[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func ParseLogEntry(line string) LogEntry {
	for _, parser := range logParsers {
		if entry, ok := parser.Parse(line); ok {
			return entry
		}
	}
	return parsePlainTextLog(line)
}

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
	case strings.HasPrefix(normalizedLevel, "DEBUG") || normalizedLevel == "TRACE" || normalizedLevel == "FINE":
		return DEBUG, nil
	case strings.HasPrefix(normalizedLevel, "INFO") || normalizedLevel == "NOTICE":
		return INFO, nil
	case strings.HasPrefix(normalizedLevel, "WARN"):
		return WARN, nil
	case strings.HasPrefix(normalizedLevel, "ERR") || normalizedLevel == "CRITICAL" || normalizedLevel == "FATAL":
		return ERROR, nil
	default:
		return DEBUG, fmt.Errorf("invalid log level: %s", level)
	}
}

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
	case level < 30:
		return DEBUG
	case level < 40:
		return INFO
	case level < 50:
		return WARN
	default:
		return ERROR
	}
}

func buildStringSet(groups ...[]string) map[string]bool {
	size := 0
	for _, group := range groups {
		size += len(group)
	}

	set := make(map[string]bool, size)
	for _, group := range groups {
		for _, value := range group {
			set[value] = true
		}
	}
	return set
}
