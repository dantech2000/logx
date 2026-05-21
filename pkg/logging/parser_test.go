package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseLogEntry(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogEntry
	}{
		{
			name:  "Plain text log with level",
			input: "2024-03-15 12:19:57 DEBUG Starting application",
			expected: LogEntry{
				Level:   DEBUG,
				Message: "2024-03-15 12:19:57 DEBUG Starting application",
				Format:  FormatPlainText,
			},
		},
		{
			name:  "Plain text log with level",
			input: "2024-03-15 12:19:57 INFO Starting application",
			expected: LogEntry{
				Level:   INFO,
				Message: "2024-03-15 12:19:57 INFO Starting application",
				Format:  FormatPlainText,
			},
		},
		{
			name:  "Plain text log with level",
			input: "2024-03-15 12:19:57 WARN Starting application",
			expected: LogEntry{
				Level:   WARN,
				Message: "2024-03-15 12:19:57 WARN Starting application",
				Format:  FormatPlainText,
			},
		},
		{
			name:  "Plain text log with level",
			input: "2024-03-15 12:19:57 ERROR Starting application",
			expected: LogEntry{
				Level:   ERROR,
				Message: "2024-03-15 12:19:57 ERROR Starting application",
				Format:  FormatPlainText,
			},
		},
		{
			name:  "JSON log with Logrus format",
			input: `{"level":"info","msg":"Server started","time":"2024-03-15T12:19:57Z","port":8080}`,
			expected: LogEntry{
				Level:   INFO,
				Message: "Server started",
				Format:  FormatJSON,
				Logger:  "logrus",
				Fields: map[string]interface{}{
					"level": "info",
					"msg":   "Server started",
					"time":  "2024-03-15T12:19:57Z",
					"port":  json.Number("8080"),
				},
			},
		},
		{
			name:  "JSON log with Zap format",
			input: `{"level":"error","ts":1647340797,"caller":"api/handler.go:42","msg":"Failed to process request","error":"invalid input"}`,
			expected: LogEntry{
				Level:   ERROR,
				Message: "Failed to process request",
				Format:  FormatJSON,
				Logger:  "zap",
				Fields: map[string]interface{}{
					"level":  "error",
					"ts":     json.Number("1647340797"),
					"caller": "api/handler.go:42",
					"msg":    "Failed to process request",
					"error":  "invalid input",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogEntry(tt.input)

			// Compare basic fields
			if got.Level != tt.expected.Level {
				t.Errorf("Level = %v, want %v", got.Level, tt.expected.Level)
			}
			if got.Format != tt.expected.Format {
				t.Errorf("Format = %v, want %v", got.Format, tt.expected.Format)
			}
			if got.Logger != tt.expected.Logger {
				t.Errorf("Logger = %v, want %v", got.Logger, tt.expected.Logger)
			}

			// For JSON logs, compare fields
			if tt.expected.Format == FormatJSON {
				if len(got.Fields) != len(tt.expected.Fields) {
					t.Errorf("Fields count = %v, want %v", len(got.Fields), len(tt.expected.Fields))
				}
				for k, v := range tt.expected.Fields {
					if got.Fields[k] != v {
						t.Errorf("Fields[%q] = %v, want %v", k, got.Fields[k], v)
					}
				}
			}
		})
	}
}

func TestParseLogEntryBracketedStructuredText(t *testing.T) {
	input := "[2026-05-15 00:38:05] [WARN] [api] Cross-namespace reference is enabled providerName=kubernetescrd namespace=default"

	got := ParseLogEntry(input)

	if got.Format != FormatBracketed {
		t.Fatalf("Format = %v, want %v", got.Format, FormatBracketed)
	}
	if got.Level != WARN {
		t.Errorf("Level = %v, want %v", got.Level, WARN)
	}
	if got.Logger != "api" {
		t.Errorf("Logger = %q, want %q", got.Logger, "api")
	}
	if got.Message != "Cross-namespace reference is enabled" {
		t.Errorf("Message = %q, want %q", got.Message, "Cross-namespace reference is enabled")
	}
	if got.Timestamp.IsZero() {
		t.Fatal("Timestamp is zero, want parsed timestamp")
	}
	if got.Timestamp.Year() != 2026 || got.Timestamp.Month() != 5 || got.Timestamp.Day() != 15 ||
		got.Timestamp.Hour() != 0 || got.Timestamp.Minute() != 38 || got.Timestamp.Second() != 5 {
		t.Errorf("Timestamp = %v, want 2026-05-15 00:38:05", got.Timestamp)
	}
	if got.Fields["providerName"] != "kubernetescrd" {
		t.Errorf("Fields[providerName] = %v, want %q", got.Fields["providerName"], "kubernetescrd")
	}
	if got.Fields["namespace"] != "default" {
		t.Errorf("Fields[namespace] = %v, want %q", got.Fields["namespace"], "default")
	}
}

func TestParseLogEntryLogfmt(t *testing.T) {
	input := `time=2026-05-15T00:38:05Z level=info component=worker msg="job completed" job_id=abc123 duration_ms=42`

	got := ParseLogEntry(input)

	if got.Format != FormatLogfmt {
		t.Fatalf("Format = %v, want %v", got.Format, FormatLogfmt)
	}
	if got.Level != INFO {
		t.Errorf("Level = %v, want %v", got.Level, INFO)
	}
	if got.Logger != "worker" {
		t.Errorf("Logger = %q, want %q", got.Logger, "worker")
	}
	if got.Message != "job completed" {
		t.Errorf("Message = %q, want %q", got.Message, "job completed")
	}
	if got.Timestamp.IsZero() {
		t.Fatal("Timestamp is zero, want parsed timestamp")
	}
	if got.Fields["job_id"] != "abc123" {
		t.Errorf("Fields[job_id] = %v, want %q", got.Fields["job_id"], "abc123")
	}
	if got.Fields["duration_ms"] != "42" {
		t.Errorf("Fields[duration_ms] = %v, want %q", got.Fields["duration_ms"], "42")
	}
}

func TestParseLogEntryKubernetesTimestampPrefix(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantLevel     LogLevel
		wantFormat    LogFormat
		wantMessage   string
		wantTimestamp string
	}{
		{
			name:          "plain framework log",
			input:         "2026-05-20T20:37:54.813922141Z   ▲ Next.js 14.2.35",
			wantLevel:     DEBUG,
			wantFormat:    FormatPlainText,
			wantMessage:   "▲ Next.js 14.2.35",
			wantTimestamp: "2026-05-20T20:37:54Z",
		},
		{
			name:          "json app log keeps pino warn level",
			input:         `2026-05-20T21:00:07.803821976Z {"level":40,"time":1779310807488,"pid":7,"msg":"Cannot execute the operation on ended Span"}`,
			wantLevel:     WARN,
			wantFormat:    FormatJSON,
			wantMessage:   "Cannot execute the operation on ended Span",
			wantTimestamp: "2026-05-20T21:00:07Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseKubernetesLogEntry(tt.input)

			if got.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tt.wantLevel)
			}
			if got.Format != tt.wantFormat {
				t.Errorf("Format = %v, want %v", got.Format, tt.wantFormat)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if got.Timestamp.IsZero() {
				t.Fatal("Timestamp is zero")
			}
			if got.Timestamp.UTC().Format(time.RFC3339) != tt.wantTimestamp {
				t.Errorf("Timestamp = %v, want %s", got.Timestamp.UTC().Format(time.RFC3339), tt.wantTimestamp)
			}
		})
	}
}

func TestParseLogEntryHTTPStatusLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel LogLevel
	}{
		{
			name:      "rails successful request is debug",
			input:     `{"method":"GET","path":"/status","status":200,"duration":0.49}`,
			wantLevel: DEBUG,
		},
		{
			name:      "client error request is warn",
			input:     `{"method":"GET","path":"/missing","status":404,"duration":0.49}`,
			wantLevel: WARN,
		},
		{
			name:      "server error request is error",
			input:     `{"method":"GET","path":"/broken","status":500,"duration":0.49}`,
			wantLevel: ERROR,
		},
		{
			name:      "explicit level wins over status",
			input:     `{"level":"info","status":500,"msg":"reported upstream status"}`,
			wantLevel: INFO,
		},
		{
			name:      "domain status without request context is debug",
			input:     `{"status":500,"message":"background job state"}`,
			wantLevel: DEBUG,
		},
		{
			name:      "url gives status request context",
			input:     `{"url":"https://example.test/missing","status":404}`,
			wantLevel: WARN,
		},
		{
			name:      "otel method gives status request context",
			input:     `{"http.method":"GET","http.route":"/broken","status":500}`,
			wantLevel: ERROR,
		},
		{
			name:      "status_code alias with request path",
			input:     `{"request.path":"/missing","status_code":404}`,
			wantLevel: WARN,
		},
		{
			name:      "statusCode alias with request method",
			input:     `{"requestMethod":"POST","statusCode":503}`,
			wantLevel: ERROR,
		},
		{
			name:      "response status alias with http context",
			input:     `{"http.method":"GET","response":{"body":"ignored"},"response.status_code":"502"}`,
			wantLevel: ERROR,
		},
		{
			name:      "nested http status context",
			input:     `{"http":{"method":"GET","status_code":503},"message":"upstream failed"}`,
			wantLevel: ERROR,
		},
		{
			name:      "nested response status context",
			input:     `{"request":{"path":"/missing"},"response":{"status_code":"404"},"message":"not found"}`,
			wantLevel: WARN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogEntry(tt.input)
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tt.wantLevel)
			}
		})
	}
}

func TestParseLogEntryPlainTextLevelBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel LogLevel
	}{
		{
			name:      "does not match info inside word",
			input:     "configuration loaded",
			wantLevel: DEBUG,
		},
		{
			name:      "does not match error inside word",
			input:     "ErrorBudget remaining",
			wantLevel: DEBUG,
		},
		{
			name:      "matches standalone warn",
			input:     "worker WARN retrying job",
			wantLevel: WARN,
		},
		{
			name:      "matches bracketed error",
			input:     "[ERROR] worker failed",
			wantLevel: ERROR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogEntry(tt.input)
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tt.wantLevel)
			}
		})
	}
}

func TestParseKubernetesLogEntryPrefersKubernetesTimestamp(t *testing.T) {
	input := `2026-05-20T20:37:54.813922141Z {"level":"info","time":"2026-05-19T10:11:12Z","message":"app timestamp is older"}`

	got := ParseKubernetesLogEntry(input)

	want := "2026-05-20T20:37:54Z"
	if got.Timestamp.UTC().Format(time.RFC3339) != want {
		t.Fatalf("Timestamp = %s, want %s", got.Timestamp.UTC().Format(time.RFC3339), want)
	}
	if got.Message != "app timestamp is older" {
		t.Fatalf("Message = %q, want app timestamp message", got.Message)
	}
}

func TestParseLogEntryStructuredJSONAliases(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantLevel     LogLevel
		wantMessage   string
		wantTimestamp string
		wantNanos     int
	}{
		{
			name:          "ecs log level alias",
			input:         `{"log":{"level":"error"},"message":"request failed","timestamp":"2026-05-20T20:37:54Z"}`,
			wantLevel:     ERROR,
			wantMessage:   "request failed",
			wantTimestamp: "2026-05-20T20:37:54Z",
		},
		{
			name:          "otel severity and body aliases",
			input:         `{"severityText":"WARN","body":"span ended","time":"1789840674123"}`,
			wantLevel:     WARN,
			wantMessage:   "span ended",
			wantTimestamp: "2026-09-19T17:57:54Z",
		},
		{
			name:          "numeric timestamp string with whitespace",
			input:         `{"level":"info","message":"started","ts":" 1789840674123 "}`,
			wantLevel:     INFO,
			wantMessage:   "started",
			wantTimestamp: "2026-09-19T17:57:54Z",
		},
		{
			name:          "nanosecond timestamp number preserves scale",
			input:         `{"level":40.0,"message":"precise","ts":1789840674123000000}`,
			wantLevel:     WARN,
			wantMessage:   "precise",
			wantTimestamp: "2026-09-19T17:57:54Z",
			wantNanos:     123000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogEntry(tt.input)
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tt.wantLevel)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if got.Timestamp.UTC().Format(time.RFC3339) != tt.wantTimestamp {
				t.Errorf("Timestamp = %s, want %s", got.Timestamp.UTC().Format(time.RFC3339), tt.wantTimestamp)
			}
			if tt.wantNanos != 0 && got.Timestamp.UTC().Nanosecond() != tt.wantNanos {
				t.Errorf("Timestamp nanoseconds = %d, want %d", got.Timestamp.UTC().Nanosecond(), tt.wantNanos)
			}
		})
	}
}

func TestFilterAndFormatLogsUsesSharedFormatter(t *testing.T) {
	input := strings.NewReader(`{"level":"info","time":"2026-05-20T20:37:54Z","message":"started","component":"api"}` + "\n")
	var buf bytes.Buffer

	if err := FilterAndFormatLogs(input, &buf, INFO); err != nil {
		t.Fatalf("FilterAndFormatLogs() error = %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "[2026-05-20 20:37:54] [INFO]") || !strings.Contains(got, "started") || !strings.Contains(got, "component=api") {
		t.Fatalf("FilterAndFormatLogs output did not use shared formatter: %q", got)
	}
}

func TestFilterAndFormatLogsAcceptsLongLines(t *testing.T) {
	input := strings.NewReader(`{"level":"info","message":"` + strings.Repeat("x", 70*1024) + `"}` + "\n")
	var buf bytes.Buffer

	if err := FilterAndFormatLogs(input, &buf, INFO); err != nil {
		t.Fatalf("FilterAndFormatLogs() error = %v", err)
	}
	if !strings.Contains(buf.String(), strings.Repeat("x", 1024)) {
		t.Fatal("FilterAndFormatLogs output missing long message content")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
		wantErr  bool
	}{
		{"debug level", "DEBUG", DEBUG, false},
		{"info level", "INFO", INFO, false},
		{"warn level", "WARN", WARN, false},
		{"warning level", "WARNING", WARN, false},
		{"error level", "ERROR", ERROR, false},
		{"lowercase level", "debug", DEBUG, false},
		{"mixed case level", "InFo", INFO, false},
		{"level with whitespace", " WARN ", WARN, false},
		{"numeric float string level", "40.0", WARN, false},
		{"zap numeric debug level", "-1", DEBUG, false},
		{"zap numeric info level", "0", INFO, false},
		{"zap numeric warn level", "1", WARN, false},
		{"zap numeric error level", "2", ERROR, false},
		{"pino numeric trace level", "10", DEBUG, false},
		{"pino numeric debug level", "20", DEBUG, false},
		{"pino numeric info level", "30", INFO, false},
		{"pino numeric warn level", "40", WARN, false},
		{"pino numeric error level", "50", ERROR, false},
		{"invalid level", "INVALID", DEBUG, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLogLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLogLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseLogLevel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetectLogger(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected string
	}{
		{
			name: "Zap logger",
			input: map[string]interface{}{
				"caller": "main.go:42",
				"ts":     1647340797,
			},
			expected: "zap",
		},
		{
			name: "Bunyan logger",
			input: map[string]interface{}{
				"@level":     "info",
				"@timestamp": "2024-03-15T12:19:57Z",
			},
			expected: "bunyan",
		},
		{
			name: "Logrus logger",
			input: map[string]interface{}{
				"level": "info",
				"msg":   "test message",
			},
			expected: "logrus",
		},
		{
			name: "Unknown logger",
			input: map[string]interface{}{
				"custom": "field",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLoggerLabel(tt.input)
			if got != tt.expected {
				t.Errorf("detectLoggerLabel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantHour  int
		wantMin   int
		wantSec   int
	}{
		{
			name:      "RFC3339",
			input:     "2024-03-15T12:19:57Z",
			wantError: false,
			wantYear:  2024,
			wantMonth: 3,
			wantDay:   15,
			wantHour:  12,
			wantMin:   19,
			wantSec:   57,
		},
		{
			name:      "RFC3339 with timezone",
			input:     "2024-03-15T12:19:57+00:00",
			wantError: false,
			wantYear:  2024,
			wantMonth: 3,
			wantDay:   15,
			wantHour:  12,
			wantMin:   19,
			wantSec:   57,
		},
		{
			name:      "RFC3339 with nanoseconds",
			input:     "2024-03-15T12:19:57.123456789Z",
			wantError: false,
			wantYear:  2024,
			wantMonth: 3,
			wantDay:   15,
			wantHour:  12,
			wantMin:   19,
			wantSec:   57,
		},
		{
			name:      "Simple date time",
			input:     "2024-03-15 12:19:57",
			wantError: false,
			wantYear:  2024,
			wantMonth: 3,
			wantDay:   15,
			wantHour:  12,
			wantMin:   19,
			wantSec:   57,
		},
		{
			name:      "Date with slashes",
			input:     "2024/03/15 12:19:57",
			wantError: false,
			wantYear:  2024,
			wantMonth: 3,
			wantDay:   15,
			wantHour:  12,
			wantMin:   19,
			wantSec:   57,
		},
		{
			name:      "Invalid timestamp",
			input:     "not a timestamp",
			wantError: true,
		},
		{
			name:      "Empty timestamp",
			input:     "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimestamp(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("parseTimestamp() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if !tt.wantError {
				if got.Year() != tt.wantYear {
					t.Errorf("Year = %v, want %v", got.Year(), tt.wantYear)
				}
				if got.Month() != tt.wantMonth {
					t.Errorf("Month = %v, want %v", got.Month(), tt.wantMonth)
				}
				if got.Day() != tt.wantDay {
					t.Errorf("Day = %v, want %v", got.Day(), tt.wantDay)
				}
				if got.Hour() != tt.wantHour {
					t.Errorf("Hour = %v, want %v", got.Hour(), tt.wantHour)
				}
				if got.Minute() != tt.wantMin {
					t.Errorf("Minute = %v, want %v", got.Minute(), tt.wantMin)
				}
				if got.Second() != tt.wantSec {
					t.Errorf("Second = %v, want %v", got.Second(), tt.wantSec)
				}
			}
		})
	}
}

func TestParseUnixTimestampScales(t *testing.T) {
	want := time.Date(2026, 5, 20, 20, 37, 54, 123000000, time.UTC)
	tests := []struct {
		name      string
		input     float64
		tolerance time.Duration
	}{
		{"seconds", float64(want.Unix()), 0},
		{"milliseconds", float64(want.UnixMilli()), 0},
		{"microseconds", float64(want.UnixMicro()), 0},
		{"nanoseconds", float64(want.UnixNano()), time.Microsecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnixTimestamp(tt.input).UTC()
			if tt.name == "seconds" {
				wantSeconds := want.Truncate(time.Second)
				if !got.Equal(wantSeconds) {
					t.Fatalf("parseUnixTimestamp() = %s, want %s", got, wantSeconds)
				}
				return
			}
			if tt.tolerance == 0 && !got.Equal(want) {
				t.Fatalf("parseUnixTimestamp() = %s, want %s", got, want)
			}
			if tt.tolerance > 0 && got.Sub(want).Abs() > tt.tolerance {
				t.Fatalf("parseUnixTimestamp() = %s, want within %s of %s", got, tt.tolerance, want)
			}
		})
	}
}

func assertInOrder(t *testing.T, got string, expected []string) {
	t.Helper()

	lastIndex := -1
	for _, value := range expected {
		index := strings.Index(got, value)
		if index == -1 {
			t.Fatalf("output missing %q: %q", value, got)
		}
		if index < lastIndex {
			t.Fatalf("output has %q out of order: %q", value, got)
		}
		lastIndex = index
	}
}
