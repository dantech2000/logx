package logging

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fatih/color"
)

// parserSeeds covers one example of every format the engine recognizes plus a
// few nasty cases, used to seed the parser fuzz targets.
var parserSeeds = []string{
	"",
	"plain message no level",
	"2026-06-24 10:06:00 INFO started app",
	`{"level":"error","msg":"boom","ts":"2026-06-24T10:00:00Z"}`,
	`{"level":50,"time":1779616800000,"msg":"pino"}`,
	`[2026-06-24 10:07:00] [WARN] [api] msg key=val`,
	`time=2026-06-24T10:05:00Z level=info msg="job" id=1`,
	"E0624 10:00:02.333456 12 server.go:42] failed to sync",
	`<34>1 2026-06-24T10:00:00Z h app - - - critical`,
	`192.0.2.10 - - [d] "GET /x HTTP/1.1" 500 87`,
	"\tindented continuation frame",
	"2026-06-24T10:00:00.1Z {\"level\":\"info\",\"msg\":\"k8s ts\"}",
	"esc \x1b[31m bad \x9b raw \x00 null",
	"{\"level\":\"info\",\"msg\":\"\xff invalid\"}",
}

// assertSafeRender verifies a parsed entry renders to terminal-safe output:
// valid UTF-8 with no raw ESC byte, and a level in range.
func assertSafeRender(t *testing.T, in string, entry LogEntry) {
	t.Helper()
	if entry.Level < DEBUG || entry.Level > ERROR {
		t.Fatalf("ParseLogEntry(%q) returned out-of-range level %d", in, entry.Level)
	}
	out := FormatLogEntry(entry)
	if !utf8.ValidString(out) {
		t.Fatalf("FormatLogEntry for %q produced invalid UTF-8: %q", in, out)
	}
	if strings.IndexByte(out, 0x1b) >= 0 {
		t.Fatalf("FormatLogEntry for %q leaked a raw ESC byte: %q", in, out)
	}
}

func FuzzParseLogEntry(f *testing.F) {
	color.NoColor = true // deterministic, ANSI-free output so the ESC check is meaningful
	for _, s := range parserSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		assertSafeRender(t, in, ParseLogEntry(in))
		assertSafeRender(t, in, ParseKubernetesLogEntry(in))
	})
}

func FuzzParseLogLevel(f *testing.F) {
	for _, s := range []string{"", "INFO", "warn", "3", "-1", "40.5", "notalevel", "  ERROR  ", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		level, err := ParseLogLevel(in)
		if err == nil && (level < DEBUG || level > ERROR) {
			t.Fatalf("ParseLogLevel(%q) = %d with nil error, out of range", in, level)
		}
	})
}

func FuzzFilterAndFormatLogs(f *testing.F) {
	color.NoColor = true
	f.Add("line1\nline2\n", uint8(0))
	f.Add(strings.Join(parserSeeds, "\n"), uint8(2))
	f.Fuzz(func(t *testing.T, data string, lvl uint8) {
		var buf bytes.Buffer
		// Must never panic and must always return a nil error for an in-memory
		// reader/writer regardless of content.
		if err := FilterAndFormatLogs(strings.NewReader(data), &buf, LogLevel(lvl%4)); err != nil {
			t.Fatalf("FilterAndFormatLogs error on %q: %v", data, err)
		}
		out := buf.String()
		if !utf8.ValidString(out) {
			t.Fatalf("FilterAndFormatLogs produced invalid UTF-8 for %q", data)
		}
		if strings.IndexByte(out, 0x1b) >= 0 {
			t.Fatalf("FilterAndFormatLogs leaked a raw ESC byte for %q: %q", data, out)
		}
	})
}
