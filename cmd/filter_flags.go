package cmd

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

// addFilterFlags registers the content-filter flags shared by `logs` and
// `parse`, so both commands filter identically.
func addFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayP(flagGrep, "g", nil, "Show only lines matching this regex (repeatable; OR)")
	cmd.Flags().StringArray(flagExclude, nil, "Hide lines matching this regex (repeatable)")
	cmd.Flags().Bool(flagHighlight, true, "Highlight --grep matches in the output")
	cmd.Flags().StringArrayP(flagWhere, "w", nil, "Keep entries matching a field predicate, e.g. status>=500 (repeatable; AND)")
	cmd.Flags().StringSliceP(flagFields, "F", nil, "Project output to only these fields, e.g. ts,level,msg")
	cmd.Flags().StringP(flagOutput, "o", "text", "Output format: text or json")
	cmd.Flags().Bool(flagStats, false, "Print a summary digest (level/status/top-message counts) instead of the lines")
	registerFilterFlagCompletions(cmd)
}

// buildPipelineOptions assembles PipelineOptions from the filter flags for the
// given minimum level. It is the single place both `logs` and `parse` translate
// flags into the shared pipeline, so a new filter is wired once.
func buildPipelineOptions(cmd *cobra.Command, minLevel logging.LogLevel) (logging.PipelineOptions, error) {
	opts := logging.PipelineOptions{MinLevel: minLevel}

	grep, err := cmd.Flags().GetStringArray(flagGrep)
	if err != nil {
		return opts, err
	}
	opts.Include, err = compileRegexps(grep)
	if err != nil {
		return opts, fmt.Errorf("invalid --grep pattern: %w", err)
	}

	exclude, err := cmd.Flags().GetStringArray(flagExclude)
	if err != nil {
		return opts, err
	}
	opts.Exclude, err = compileRegexps(exclude)
	if err != nil {
		return opts, fmt.Errorf("invalid --exclude pattern: %w", err)
	}

	opts.Highlight, err = cmd.Flags().GetBool(flagHighlight)
	if err != nil {
		return opts, err
	}

	where, err := cmd.Flags().GetStringArray(flagWhere)
	if err != nil {
		return opts, err
	}
	for _, expr := range where {
		pred, perr := logging.ParseFieldPredicate(expr)
		if perr != nil {
			return opts, fmt.Errorf("invalid --where %q: %w", expr, perr)
		}
		opts.Where = append(opts.Where, pred)
	}

	opts.Fields, err = cmd.Flags().GetStringSlice(flagFields)
	if err != nil {
		return opts, err
	}

	outStr, err := cmd.Flags().GetString(flagOutput)
	if err != nil {
		return opts, err
	}
	opts.Output, err = logging.ParseOutputFormat(outStr)
	if err != nil {
		return opts, err
	}

	opts.CollectStats, err = cmd.Flags().GetBool(flagStats)
	if err != nil {
		return opts, err
	}

	// --stats replaces per-line output with a human-readable text digest, so the
	// flags that shape per-line rendering have nothing to act on. They were
	// silently ignored: `logx parse -o json --stats | jq` printed the box-drawing
	// digest to stdout and exited 0, breaking the pipeline with no explanation.
	// Every other incompatible pairing is rejected, so this matches.
	if opts.CollectStats {
		if opts.Output != logging.OutputText {
			return opts, fmt.Errorf("--stats cannot be combined with --output %s (the digest is a text summary)", outStr)
		}
		if len(opts.Fields) > 0 {
			return opts, errors.New("--fields cannot be combined with --stats (the digest replaces per-line output)")
		}
	}

	return opts, nil
}

// compileRegexps compiles each pattern, reporting which one failed.
func compileRegexps(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}
