package logging

import "testing"

func TestParseXMLLog(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLevel  LogLevel
		wantMsg    string
		wantDetect bool
	}{
		{
			name:       "paired element with attrs",
			input:      `<log level="ERROR" ts="2026-06-24T10:00:00Z">connection refused</log>`,
			wantLevel:  ERROR,
			wantMsg:    "connection refused",
			wantDetect: true,
		},
		{
			name:       "severity attribute",
			input:      `<event severity="warn">disk low</event>`,
			wantLevel:  WARN,
			wantMsg:    "disk low",
			wantDetect: true,
		},
		{
			name:       "self-closing element",
			input:      `<event severity="fatal" code="9"/>`,
			wantLevel:  FATAL,
			wantDetect: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogEntry(tt.input)
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", got.Level, tt.wantLevel)
			}
			if got.LevelDetected != tt.wantDetect {
				t.Errorf("LevelDetected = %v, want %v", got.LevelDetected, tt.wantDetect)
			}
			if tt.wantMsg != "" && got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseXMLTimestamp(t *testing.T) {
	got := ParseLogEntry(`<log level="INFO" time="2026-06-24T10:00:00Z">ok</log>`)
	if got.Timestamp.IsZero() {
		t.Fatal("expected timestamp from time attribute")
	}
	if got.Timestamp.UTC().Format("2006-01-02 15:04:05") != "2026-06-24 10:00:00" {
		t.Fatalf("Timestamp = %v", got.Timestamp)
	}
}

func TestParseYAMLFlowLog(t *testing.T) {
	got := ParseLogEntry(`{level: error, msg: "db down", status: 500}`)
	if got.Level != ERROR {
		t.Errorf("Level = %v, want ERROR", got.Level)
	}
	if !got.LevelDetected {
		t.Error("LevelDetected = false, want true")
	}
	if got.Message != "db down" {
		t.Errorf("Message = %q, want 'db down'", got.Message)
	}
}

func TestParseCSVLog(t *testing.T) {
	got := ParseLogEntry(`2026-06-24T10:00:00Z,ERROR,svc,connection failed`)
	if got.Level != ERROR {
		t.Errorf("Level = %v, want ERROR", got.Level)
	}
	if !got.LevelDetected {
		t.Error("LevelDetected = false, want true")
	}
	if got.Timestamp.IsZero() {
		t.Error("expected leading timestamp to be parsed")
	}
	if got.Message != "svc, connection failed" {
		t.Errorf("Message = %q, want 'svc, connection failed'", got.Message)
	}
}

// The new parsers must not claim ordinary text. Each of these should fall
// through to plain text (DEBUG, not level-detected).
func TestNewFormatsDoNotMisclassifyProse(t *testing.T) {
	prose := []string{
		"Hello, world, this is prose with commas",
		"{TODO: refactor this later}",
		"just some <angle> text here",
		"2026-06-24T10:00:00Z,svc,no-level-token,here",
		"<unclosed>some text without a level word",
		"{not: a, log: line}",
	}
	for _, line := range prose {
		t.Run(line, func(t *testing.T) {
			got := ParseLogEntry(line)
			if got.LevelDetected {
				t.Fatalf("line was misclassified as a structured log: %+v", got)
			}
		})
	}
}
