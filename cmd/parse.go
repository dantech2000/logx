package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

var parseCmd = &cobra.Command{
	Use:   "parse [file]",
	Short: "Parse and colorize logs from a file or stdin (no cluster required)",
	Long: `Parse logs from a file or standard input and render them the same way
logx renders pod logs: detected level, timestamp, structured fields, multi-line
grouping, and --level filtering.

This is useful for inspecting captured logs, piping output from other tools, or
testing logx against sample logs without connecting to a cluster.

Examples:
  logx parse app.log
  logx parse app.log -l WARN
  kubectl logs my-pod | logx parse -l ERROR
  cat app.log | logx parse`,
	Args: cobra.MaximumNArgs(1),
	RunE: runParse,
}

func init() {
	rootCmd.AddCommand(parseCmd)
	parseCmd.Flags().StringP(flagLevel, "l", "DEBUG", "Filter logs by level (TRACE, DEBUG, INFO, WARN, ERROR, FATAL)")
	addFilterFlags(parseCmd)
}

func runParse(cmd *cobra.Command, args []string) error {
	levelStr, err := effectiveLevel(cmd)
	if err != nil {
		return err
	}
	level, err := logging.ParseLogLevel(levelStr)
	if err != nil {
		return fmt.Errorf("invalid level %q: %w", levelStr, err)
	}

	opts, err := buildPipelineOptions(cmd, level)
	if err != nil {
		return err
	}

	reader, closeFn, err := openLogSource(cmd, args)
	if err != nil {
		return err
	}
	defer closeFn()

	pipeline := logging.NewPipeline(opts)
	if err := pipeline.Run(reader, cmd.OutOrStdout()); err != nil {
		return err
	}
	if stats := pipeline.Stats(); stats != nil {
		return stats.Write(cmd.OutOrStdout())
	}
	return nil
}

// openLogSource returns the log input: the named file, or stdin when no file (or
// "-") is given. The returned close function is always safe to call.
func openLogSource(cmd *cobra.Command, args []string) (io.Reader, func(), error) {
	if len(args) == 1 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return nil, func() {}, fmt.Errorf("opening %s: %w", args[0], err)
		}
		return f, func() { _ = f.Close() }, nil
	}
	return cmd.InOrStdin(), func() {}, nil
}
