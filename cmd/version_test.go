package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dantech2000/logx/pkg/version"
	"gopkg.in/yaml.v2"
)

func testVersion() version.Version {
	return version.Version{
		Version:    "1.2.3",
		CommitHash: "abc1234",
		BuildDate:  "2026-06-24",
	}
}

func TestWriteVersionShort(t *testing.T) {
	var buf bytes.Buffer
	if err := writeVersion(&buf, testVersion(), true, ""); err != nil {
		t.Fatalf("writeVersion() error = %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "1.2.3" {
		t.Fatalf("short output = %q, want %q", got, "1.2.3")
	}
}

func TestWriteVersionDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := writeVersion(&buf, testVersion(), false, ""); err != nil {
		t.Fatalf("writeVersion() error = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Version: 1.2.3", "Commit: abc1234", "Build Date: 2026-06-24"} {
		if !strings.Contains(out, want) {
			t.Fatalf("default output %q missing %q", out, want)
		}
	}
}

func TestWriteVersionJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writeVersion(&buf, testVersion(), false, "json"); err != nil {
		t.Fatalf("writeVersion() error = %v", err)
	}
	var data versionData
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if data.Version != "1.2.3" || data.Commit != "abc1234" || data.BuildDate != "2026-06-24" {
		t.Fatalf("decoded JSON = %+v, want version fields populated", data)
	}
	if data.GoVersion == "" || data.OS == "" || data.Arch == "" {
		t.Fatalf("decoded JSON = %+v, want runtime fields populated", data)
	}
}

func TestWriteVersionYAML(t *testing.T) {
	var buf bytes.Buffer
	if err := writeVersion(&buf, testVersion(), false, "yaml"); err != nil {
		t.Fatalf("writeVersion() error = %v", err)
	}
	var data versionData
	if err := yaml.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	if data.Version != "1.2.3" {
		t.Fatalf("decoded YAML = %+v, want version 1.2.3", data)
	}
}

func TestWriteVersionUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	err := writeVersion(&buf, testVersion(), false, "toml")
	if err == nil {
		t.Fatal("writeVersion() with unsupported format = nil error, want error")
	}
	if !strings.Contains(err.Error(), "toml") {
		t.Fatalf("error = %v, want it to mention the bad format", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("unsupported format wrote output %q, want nothing", buf.String())
	}
}

// TestVersionShortTakesPrecedence ensures --short wins even if --output is set.
func TestVersionShortTakesPrecedence(t *testing.T) {
	var buf bytes.Buffer
	if err := writeVersion(&buf, testVersion(), true, "json"); err != nil {
		t.Fatalf("writeVersion() error = %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "1.2.3" {
		t.Fatalf("output = %q, want short version to win", got)
	}
}
