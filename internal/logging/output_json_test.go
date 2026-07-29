package logging

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseOutputFormat(t *testing.T) {
	for in, want := range map[string]OutputFormat{
		"":       OutputText,
		"text":   OutputText,
		"json":   OutputJSON,
		"ndjson": OutputJSON,
	} {
		got, err := ParseOutputFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseOutputFormat(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseOutputFormat("xml"); err == nil {
		t.Error("ParseOutputFormat(xml) should error")
	}
}

func TestMarshalEntryJSON(t *testing.T) {
	out := MarshalEntryJSON(ParseLogEntry(`{"level":"error","msg":"db down","status":503,"ts":"2026-06-24T10:00:00Z"}`))

	var got jsonEntry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, out)
	}
	if got.Level != "ERROR" {
		t.Errorf("level = %q, want ERROR", got.Level)
	}
	if got.Message != "db down" {
		t.Errorf("message = %q, want 'db down'", got.Message)
	}
	if got.Format != "json" {
		t.Errorf("format = %q, want json", got.Format)
	}
	// status survives in fields; level/msg/ts are not duplicated there.
	if _, ok := got.Fields["status"]; !ok {
		t.Errorf("expected status in fields, got %v", got.Fields)
	}
	for _, dup := range []string{"level", "msg", "ts"} {
		if _, ok := got.Fields[dup]; ok {
			t.Errorf("field %q should not be duplicated into fields", dup)
		}
	}
}

func TestMarshalEntryJSONEscapesControlBytes(t *testing.T) {
	// A raw ESC byte in the message must be escaped, never emitted raw.
	out := MarshalEntryJSON(ParseLogEntry("INFO be\x1bvil"))
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("JSON output leaked a raw ESC byte: %q", out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("JSON output is invalid: %q", out)
	}
}

func TestMarshalProjectedJSON(t *testing.T) {
	out, ok := marshalProjectedJSON(
		ParseLogEntry(`{"level":"warn","status":404,"path":"/x","msg":"nope"}`),
		classifyFields([]string{"level", "status", "missing"}),
	)
	if !ok {
		t.Fatal("projection with resolvable keys should be emitted")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, out)
	}
	if obj["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", obj["level"])
	}
	if _, ok := obj["missing"]; ok {
		t.Error("missing key should be omitted from projection")
	}
	if _, ok := obj["path"]; ok {
		t.Error("unselected key should not appear")
	}
}

func TestPipelineJSONOutputIsNDJSON(t *testing.T) {
	var buf strings.Builder
	in := "INFO one\nWARN two\n"
	if err := NewPipeline(PipelineOptions{MinLevel: TRACE, Output: OutputJSON}).Run(context.Background(), strings.NewReader(in), &buf); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d: %q", len(lines), buf.String())
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Fatalf("line is not valid JSON: %q", l)
		}
	}
}
