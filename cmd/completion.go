package cmd

import (
	"os"
	"strings"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
)

// completionCmd represents the completion command
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `To load completions:

Bash:
  $ source <(logx completion bash)

Zsh:
  # If shell completion is not already enabled in your environment:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  
  # Then generate and source completion:
  $ logx completion zsh > "${fpath[1]}/_logx"

Fish:
  $ logx completion fish | source

PowerShell:
  PS> logx completion powershell | Out-String | Invoke-Expression`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

// Static value enums offered for flag completion, kept in one place so the help
// text, parser, and completion agree on what is valid.
var (
	levelCompletions  = logging.LevelNames()
	themeCompletions  = []string{"dark", "light"}
	outputCompletions = []string{"text", "json"}
	colorCompletions  = []string{"auto", "always", "never"}

	// knownFieldNames are common log field/virtual-key names offered as hints for
	// --fields and --where. They are suggestions, not an exhaustive or enforced
	// set: any field present on a parsed entry still works.
	knownFieldNames = []string{
		"level", "msg", "message", "logger", "component", "source",
		"ts", "time", "timestamp",
		"status", "status_code", "method", "path", "route", "url", "error", "caller",
	}
)

// filterPrefixBy returns the name of each item whose name(item) starts with
// prefix, in the same order as items. The one prefix-filter loop shared by every
// completion function below, whether the candidates are plain strings (values
// themselves are the name) or richer types like corev1.Pod (named by a field).
func filterPrefixBy[T any](items []T, prefix string, name func(T) string) []string {
	var out []string
	for _, item := range items {
		if n := name(item); strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// filterPrefix is filterPrefixBy for a plain string slice.
func filterPrefix(values []string, prefix string) []string {
	return filterPrefixBy(values, prefix, func(v string) string { return v })
}

// staticFlagCompletion returns a completion function that offers the given fixed
// values, filtered by the prefix the user has typed. File completion is disabled
// since these flags take an enum, not a path.
func staticFlagCompletion(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return filterPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFieldList completes the comma-separated --fields value, offering field
// name hints for the final element while preserving any already-typed prefix.
func completeFieldList(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix, last := splitLastComma(toComplete)
	matches := filterPrefix(knownFieldNames, last)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = prefix + m
	}
	// NoSpace lets the user keep typing (another comma-separated field); NoFileComp
	// suppresses path completion.
	return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeWhereField hints field names for a --where predicate. Once the user has
// started typing an operator there is a value we cannot predict, so we stop
// offering hints (and leave file completion off).
func completeWhereField(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.ContainsAny(toComplete, logging.PredicateOperatorChars) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// NoSpace so the user can append the operator (e.g. status>=500) without a gap.
	return filterPrefix(knownFieldNames, toComplete), cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// splitLastComma splits s at its final comma into (everything up to and including
// the comma, the trailing element). With no comma it returns ("", s).
func splitLastComma(s string) (prefix, last string) {
	if i := strings.LastIndex(s, ","); i >= 0 {
		return s[:i+1], s[i+1:]
	}
	return "", s
}

// registerFilterFlagCompletions wires completion for the shared filter flags
// (--output, --fields, --where). Called from addFilterFlags so both `logs` and
// `parse` get them.
func registerFilterFlagCompletions(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc(flagOutput, staticFlagCompletion(outputCompletions...))
	_ = cmd.RegisterFlagCompletionFunc(flagFields, completeFieldList)
	_ = cmd.RegisterFlagCompletionFunc(flagWhere, completeWhereField)
}

// registerLevelCompletion wires --level value completion on cmd.
func registerLevelCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc(flagLevel, staticFlagCompletion(levelCompletions...))
}

// addLevelFlag registers the shared --level flag plus its value completion. One
// definition so `logs` and `parse` agree on the help text and the level list
// stays in sync with levelCompletions.
func addLevelFlag(cmd *cobra.Command) {
	cmd.Flags().StringP(flagLevel, "l", "DEBUG",
		"Filter logs by level ("+strings.Join(levelCompletions, ", ")+")")
	registerLevelCompletion(cmd)
}
