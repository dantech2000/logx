package logging

import (
	"strings"
	"testing"
)

func TestTemplateMessage(t *testing.T) {
	cases := map[string]string{
		"db timeout after 30s":     "db timeout after #s",
		"db timeout after 45s":     "db timeout after #s",
		"user deadbeefcafe1234 in": "user # in",
		"no variables here":        "no variables here",
		"   ":                      "",
	}
	for in, want := range cases {
		if got := templateMessage(in); got != want {
			t.Errorf("templateMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatsRecordAndWrite(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	s := NewStats()
	lines := []string{
		"INFO ok",
		"ERROR db timeout after 30s",
		"ERROR db timeout after 45s",
		`{"level":"warn","status":404,"path":"/x"}`,
		`{"level":"error","status":503,"path":"/y"}`,
	}
	for _, l := range lines {
		s.Record(ParseLogEntry(l))
	}

	if s.Total() != 5 {
		t.Fatalf("Total() = %d, want 5", s.Total())
	}

	var b strings.Builder
	if err := s.Write(&b); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	out := b.String()

	for _, want := range []string{"lines: 5", "ERROR 3", "WARN 1", "INFO 1", "4xx 1", "5xx 1", "db timeout after #s"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats summary missing %q:\n%s", want, out)
		}
	}
	// The two timeout messages must collapse to a single templated entry with count 2.
	if strings.Count(out, "db timeout after #s") != 1 {
		t.Errorf("templated message not grouped once:\n%s", out)
	}
}

func TestStatusClassOf(t *testing.T) {
	cases := []struct {
		line      string
		wantClass int
		wantOK    bool
	}{
		{`{"status":503,"path":"/a"}`, 5, true},
		{`{"status":404,"path":"/a"}`, 4, true},
		{`{"status":200,"path":"/a"}`, 2, true},
		{`{"msg":"no status"}`, 0, false},
		{`{"status":999,"path":"/a"}`, 0, false},
	}
	for _, c := range cases {
		got, ok := statusClassOf(ParseLogEntry(c.line))
		if got != c.wantClass || ok != c.wantOK {
			t.Errorf("statusClassOf(%s) = %d,%v want %d,%v", c.line, got, ok, c.wantClass, c.wantOK)
		}
	}
}
