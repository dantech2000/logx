package cmd

import (
	"os"
	"strings"

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
	levelCompletions  = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
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

// staticFlagCompletion returns a completion function that offers the given fixed
// values, filtered by the prefix the user has typed. File completion is disabled
// since these flags take an enum, not a path.
func staticFlagCompletion(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var out []string
		for _, v := range values {
			if strings.HasPrefix(v, toComplete) {
				out = append(out, v)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFieldList completes the comma-separated --fields value, offering field
// name hints for the final element while preserving any already-typed prefix.
func completeFieldList(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix, last := splitLastComma(toComplete)
	var out []string
	for _, name := range knownFieldNames {
		if strings.HasPrefix(name, last) {
			out = append(out, prefix+name)
		}
	}
	// NoSpace lets the user keep typing (another comma-separated field); NoFileComp
	// suppresses path completion.
	return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeWhereField hints field names for a --where predicate. Once the user has
// started typing an operator there is a value we cannot predict, so we stop
// offering hints (and leave file completion off).
func completeWhereField(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.ContainsAny(toComplete, operatorChars) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, name := range knownFieldNames {
		if strings.HasPrefix(name, toComplete) {
			out = append(out, name)
		}
	}
	// NoSpace so the user can append the operator (e.g. status>=500) without a gap.
	return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// operatorChars mirrors the predicate operator characters; once any appears in a
// --where token the user is past the field name.
const operatorChars = "<>=!~"

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
