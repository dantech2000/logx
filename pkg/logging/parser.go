package logging

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
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
	RawLine   string // Store the original line
}

var logLevelColors = map[LogLevel]*color.Color{
	DEBUG: color.New(color.FgCyan),
	INFO:  color.New(color.FgGreen),
	WARN:  color.New(color.FgYellow),
	ERROR: color.New(color.FgRed),
}

// Colors for different log components
var (
	timestampColor = color.New(color.FgBlue)
	loggerColor    = color.New(color.FgMagenta)
	keyColor       = color.New(color.FgCyan)
	valueColor     = color.New(color.FgWhite)
	quoteColor     = color.New(color.FgHiBlack)
	errorColor     = color.New(color.FgRed, color.Bold)
)

// Common field mappings for different JSON log formats
var (
	// Level field names across different loggers
	jsonLevelFields = []string{
		"level",     // Common
		"severity",  // Google Cloud
		"log_level", // Custom
		"loglevel",  // Custom
		"@level",    // Bunyan
		"levelname", // Python logging
		"status",    // NGINX
		"LEVEL",     // Some uppercase variants
	}

	// Message field names
	jsonMessageFields = []string{
		"message",  // Common
		"msg",      // Zap, Logrus
		"log",      // Docker
		"text",     // Custom
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
	jsonLogParser{},
	bracketedLogParser{},
	logfmtLogParser{},
}

// detectLogFormat determines if the log line is JSON or plain text
func detectLogFormat(line string) LogFormat {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
		var js map[string]interface{}
		if json.Unmarshal([]byte(line), &js) == nil {
			return FormatJSON
		}
	}
	return FormatPlainText
}

type jsonLogParser struct{}

func (jsonLogParser) Parse(line string) (LogEntry, bool) {
	if detectLogFormat(line) != FormatJSON {
		return LogEntry{}, false
	}
	return parseJSONLog(line), true
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

// detectLogger tries to determine which logging framework generated the log
func detectLogger(data map[string]interface{}) string {
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
		return "unknown"
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

// parseJSONLog attempts to parse a JSON log entry
func parseJSONLog(line string) LogEntry {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return LogEntry{
			Level:   DEBUG,
			Message: line,
			Format:  FormatJSON,
		}
	}

	logger := detectLogger(data)
	entry := LogEntry{
		Format:  FormatJSON,
		Fields:  data,
		Logger:  logger,
		RawLine: line,
	}

	// Find and parse level
	for _, field := range jsonLevelFields {
		if val, ok := data[field]; ok {
			// Handle both string and numeric levels
			levelStr := fmt.Sprintf("%v", val)
			if level, err := ParseLogLevel(levelStr); err == nil {
				entry.Level = level
				break
			}
		}
	}

	// Find message
	for _, field := range jsonMessageFields {
		if val, ok := data[field]; ok {
			entry.Message = fmt.Sprintf("%v", val)
			break
		}
	}

	// If no message found, try error field or full line
	if entry.Message == "" {
		if err, ok := data["error"]; ok {
			entry.Message = fmt.Sprintf("%v", err)
		} else {
			entry.Message = line
		}
	}

	// Parse timestamp
	for _, field := range jsonTimeFields {
		if val, ok := data[field]; ok {
			// Handle numeric timestamps (milliseconds since epoch)
			if numTime, ok := val.(float64); ok {
				msec := int64(numTime)
				if msec > 1e11 { // Assuming milliseconds if number is large enough
					entry.Timestamp = time.Unix(0, msec*int64(time.Millisecond))
				} else {
					entry.Timestamp = time.Unix(msec, 0)
				}
				break
			}
			// Try parsing string timestamps
			if timeStr, ok := val.(string); ok {
				if ts, err := parseTimestamp(timeStr); err == nil {
					entry.Timestamp = ts
					break
				}
			}
		}
	}

	return entry
}

// parsePlainTextLog parses a plain text log entry
func parsePlainTextLog(line string) LogEntry {
	entry := LogEntry{
		Level:   DEBUG,
		Format:  FormatPlainText,
		RawLine: line,
	}

	// First try to extract timestamp
	timeRegex := regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	if timeStr := timeRegex.FindString(line); timeStr != "" {
		if ts, err := parseTimestamp(timeStr); err == nil {
			entry.Timestamp = ts
		}
	}

	// Try to extract log level
	levelRegex := regexp.MustCompile(`(?i)(DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|TRACE)`)
	if match := levelRegex.FindString(line); match != "" {
		if level, err := ParseLogLevel(match); err == nil {
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

func ParseLogEntry(line string) LogEntry {
	for _, parser := range logParsers {
		if entry, ok := parser.Parse(line); ok {
			return entry
		}
	}
	return parsePlainTextLog(line)
}

// ParseLogLevel parses both string and numeric log levels
func ParseLogLevel(level string) (LogLevel, error) {
	// Try parsing numeric levels first
	if numLevel, err := strconv.Atoi(level); err == nil {
		switch {
		case numLevel <= 10:
			return DEBUG, nil // DEBUG
		case numLevel <= 20:
			return INFO, nil // INFO
		case numLevel <= 30:
			return WARN, nil // WARN
		case numLevel > 30:
			return ERROR, nil // ERROR
		}
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

func formatLogEntry(entry LogEntry) string {
	var parts []string

	// Add timestamp if available
	if !entry.Timestamp.IsZero() {
		parts = append(parts, timestampColor.Sprintf("[%s]", entry.Timestamp.Format("2006-01-02 15:04:05")))
	}

	// Add level with appropriate color
	parts = append(parts, logLevelColors[entry.Level].Sprint(fmt.Sprintf("[%s]", entry.Level)))

	// Add logger type for JSON logs
	if entry.Format == FormatJSON && entry.Logger != "" {
		parts = append(parts, loggerColor.Sprintf("[%s]", entry.Logger))
	}

	// For JSON logs, parse and format the content
	if entry.Format == FormatJSON {
		// Format the JSON fields with colors
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(entry.RawLine), &data); err == nil {
			excludeFields := map[string]bool{
				"level": true, "severity": true, "log_level": true,
				"time": true, "timestamp": true, "@timestamp": true,
			}

			// Format message field specially
			msg := ""
			for _, field := range jsonMessageFields {
				if val, ok := data[field]; ok {
					msg = fmt.Sprintf("%v", val)
					break
				}
			}

			// Build the formatted JSON output
			var fields []string
			for k, v := range data {
				if !excludeFields[k] && k != "msg" && k != "message" {
					formattedValue := formatValue(v)
					fields = append(fields, fmt.Sprintf("%s=%s",
						keyColor.Sprint(k),
						formattedValue))
				}
			}

			// If we found a message, put it first
			if msg != "" {
				if entry.Level == ERROR || strings.Contains(strings.ToLower(msg), "error") ||
					strings.Contains(strings.ToLower(msg), "warn") ||
					strings.Contains(strings.ToLower(msg), "failed") {
					msg = errorColor.Sprint(msg)
				}
				fields = append([]string{msg}, fields...)
			}

			parts = append(parts, strings.Join(fields, " "))
		} else {
			// If JSON parsing fails, use the raw line
			parts = append(parts, entry.RawLine)
		}
	} else {
		// For plain text, check if it contains error-related text
		if entry.Level == ERROR || strings.Contains(strings.ToLower(entry.RawLine), "error") ||
			strings.Contains(strings.ToLower(entry.RawLine), "failed") {
			parts = append(parts, errorColor.Sprint(entry.RawLine))
		} else {
			parts = append(parts, entry.RawLine)
		}
	}

	return strings.Join(parts, " ")
}

// formatValue formats a value with appropriate coloring based on its type
func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		if val == "" {
			return quoteColor.Sprint(`""`)
		}
		// Check if the string needs quotes (contains spaces or special characters)
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
	case map[string]interface{}:
		parts := make([]string, 0, len(val))
		for k, v := range val {
			parts = append(parts, fmt.Sprintf("%s=%s",
				keyColor.Sprint(k),
				formatValue(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, " "))
	default:
		return valueColor.Sprintf("%v", val)
	}
}

func ParseLog(log string) string {
	entry := ParseLogEntry(log)
	return formatLogEntry(entry)
}
