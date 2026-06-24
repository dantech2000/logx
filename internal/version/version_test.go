package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	v := Version{Version: "1.2.3", CommitHash: "abc", BuildDate: "2026-06-24"}
	if got := v.String(); got != "1.2.3" {
		t.Fatalf("String() = %q, want %q", got, "1.2.3")
	}
}

func TestVersionFullString(t *testing.T) {
	v := Version{Version: "1.2.3", CommitHash: "abc1234", BuildDate: "2026-06-24"}
	got := v.FullString()
	for _, want := range []string{
		"Version: 1.2.3",
		"Commit: abc1234",
		"Build Date: 2026-06-24",
		"Go Version: " + runtime.Version(),
		"OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FullString() = %q, missing %q", got, want)
		}
	}
}
