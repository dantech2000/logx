package cmd

import (
	"testing"
	"time"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
)

func TestApplySince(t *testing.T) {
	t.Run("duration sets SinceSeconds", func(t *testing.T) {
		var lf kubernetes.LogFetcher
		if err := applySince("5m", &lf); err != nil {
			t.Fatalf("applySince(5m) error: %v", err)
		}
		if lf.SinceSeconds == nil || *lf.SinceSeconds != 300 {
			t.Fatalf("SinceSeconds = %v, want 300", lf.SinceSeconds)
		}
		if lf.SinceTime != nil {
			t.Fatal("SinceTime should be nil for a duration")
		}
	})

	t.Run("rfc3339 sets SinceTime", func(t *testing.T) {
		var lf kubernetes.LogFetcher
		if err := applySince("2026-06-24T10:00:00Z", &lf); err != nil {
			t.Fatalf("applySince(rfc3339) error: %v", err)
		}
		if lf.SinceTime == nil {
			t.Fatal("SinceTime should be set for an RFC3339 value")
		}
		if !lf.SinceTime.Time.Equal(time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)) {
			t.Fatalf("SinceTime = %v, want 2026-06-24T10:00:00Z", lf.SinceTime)
		}
		if lf.SinceSeconds != nil {
			t.Fatal("SinceSeconds should be nil for an RFC3339 value")
		}
	})

	t.Run("empty is a no-op", func(t *testing.T) {
		var lf kubernetes.LogFetcher
		if err := applySince("", &lf); err != nil {
			t.Fatalf("applySince(empty) error: %v", err)
		}
		if lf.SinceSeconds != nil || lf.SinceTime != nil {
			t.Fatal("empty --since should leave both nil")
		}
	})

	t.Run("invalid errors", func(t *testing.T) {
		var lf kubernetes.LogFetcher
		if err := applySince("yesterday", &lf); err == nil {
			t.Fatal("expected error for an unparseable --since")
		}
	})

	t.Run("zero duration errors", func(t *testing.T) {
		var lf kubernetes.LogFetcher
		if err := applySince("0s", &lf); err == nil {
			t.Fatal("expected error for a non-positive duration")
		}
	})
}

func TestApplyLogQuery(t *testing.T) {
	cmd := &cobra.Command{Use: "logs"}
	addLogQueryFlags(cmd)
	if err := cmd.ParseFlags([]string{"--since", "1h", "--tail", "50", "--timestamps"}); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}

	var lf kubernetes.LogFetcher
	if err := applyLogQuery(cmd, &lf); err != nil {
		t.Fatalf("applyLogQuery error: %v", err)
	}
	if !lf.Timestamps {
		t.Error("--timestamps not applied")
	}
	if lf.TailLines == nil || *lf.TailLines != 50 {
		t.Errorf("TailLines = %v, want 50", lf.TailLines)
	}
	if lf.SinceSeconds == nil || *lf.SinceSeconds != 3600 {
		t.Errorf("SinceSeconds = %v, want 3600", lf.SinceSeconds)
	}
}

func TestApplyLogQueryTailAllByDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "logs"}
	addLogQueryFlags(cmd)
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags error: %v", err)
	}
	var lf kubernetes.LogFetcher
	if err := applyLogQuery(cmd, &lf); err != nil {
		t.Fatalf("applyLogQuery error: %v", err)
	}
	if lf.TailLines != nil {
		t.Errorf("default --tail should leave TailLines nil (all), got %v", *lf.TailLines)
	}
}

// TestApplySinceSubSecondDurations pins that a sub-second --since is accepted
// and rounded up. Truncating to whole seconds rejected "500ms" with the
// self-contradicting "duration must be positive", and silently narrowed
// "1500ms" to one second, dropping half a second of logs. Rounding up can only
// widen the window, never hide a line.
func TestApplySinceSubSecondDurations(t *testing.T) {
	tests := []struct {
		since string
		want  int64
	}{
		{"500ms", 1},
		{"1500ms", 2},
		{"1.9s", 2},
		{"5m", 300},
		{"1s", 1},
	}

	for _, tc := range tests {
		t.Run(tc.since, func(t *testing.T) {
			var lf kubernetes.LogFetcher
			if err := applySince(tc.since, &lf); err != nil {
				t.Fatalf("applySince(%q) error: %v", tc.since, err)
			}
			if lf.SinceSeconds == nil {
				t.Fatalf("applySince(%q) left SinceSeconds nil", tc.since)
			}
			if *lf.SinceSeconds != tc.want {
				t.Fatalf("applySince(%q) = %d, want %d", tc.since, *lf.SinceSeconds, tc.want)
			}
		})
	}

	// A genuinely non-positive duration is still rejected.
	for _, bad := range []string{"0s", "-5m"} {
		var lf kubernetes.LogFetcher
		if err := applySince(bad, &lf); err == nil {
			t.Errorf("applySince(%q) should have been rejected", bad)
		}
	}
}
