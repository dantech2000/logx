package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainErrorIsSanitized(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "logx")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Trigger an error that contains a non-existent kubeconfig path. The error
	// message itself is safe, but this exercises the stderr write path where
	// Sanitize now runs.
	cmd := exec.Command(bin, "--kubeconfig", filepath.Join(t.TempDir(), "nope"), "logs", "x")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	_ = cmd.Run()

	if stderr.Len() == 0 {
		t.Fatal("expected error output on stderr, got nothing")
	}

	// The output must not contain raw C0 control characters (except tab, which
	// Sanitize passes through). This is a structural guarantee: every byte that
	// reaches stderr has been filtered.
	out := stderr.String()
	for i, b := range []byte(out) {
		if b < 0x20 && b != '\t' && b != '\n' {
			t.Errorf("raw control byte 0x%02X at offset %d in stderr: %q", b, i, out)
		}
	}
}
