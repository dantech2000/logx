package cmd

import (
	"fmt"
	"time"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// addLogQueryFlags registers the server-side log-window flags. These shape the
// Kubernetes GetLogs request itself (how far back, how many lines, timestamps),
// as opposed to the client-side content filters.
func addLogQueryFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagSince, "", "Only return logs newer than a duration (e.g. 5m, 2h) or an RFC3339 time")
	cmd.Flags().Int64(flagTail, -1, "Show only the last N lines (-1 for all)")
	cmd.Flags().Bool(flagTimestamps, false, "Include timestamps on each log line")
}

// applyLogQuery reads the log-window flags and configures the fetcher. --since
// accepts either a Go duration or an RFC3339 timestamp; --tail < 0 means all.
func applyLogQuery(cmd *cobra.Command, lf *kubernetes.LogFetcher) error {
	timestamps, err := cmd.Flags().GetBool(flagTimestamps)
	if err != nil {
		return err
	}
	lf.Timestamps = timestamps

	tail, err := cmd.Flags().GetInt64(flagTail)
	if err != nil {
		return err
	}
	if tail >= 0 {
		lf.TailLines = &tail
	}

	since, err := cmd.Flags().GetString(flagSince)
	if err != nil {
		return err
	}
	return applySince(since, lf)
}

// applySince parses the --since value into either SinceSeconds (duration) or
// SinceTime (RFC3339).
func applySince(since string, lf *kubernetes.LogFetcher) error {
	if since == "" {
		return nil
	}
	if d, derr := time.ParseDuration(since); derr == nil {
		secs := int64(d.Seconds())
		if secs <= 0 {
			return fmt.Errorf("--since duration must be positive, got %q", since)
		}
		lf.SinceSeconds = &secs
		return nil
	}
	if ts, terr := time.Parse(time.RFC3339, since); terr == nil {
		mt := metav1.NewTime(ts)
		lf.SinceTime = &mt
		return nil
	}
	return fmt.Errorf("invalid --since %q: want a duration (e.g. 5m) or an RFC3339 time", since)
}
