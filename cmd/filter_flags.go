package cmd

import (
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
