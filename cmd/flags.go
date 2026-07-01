package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Flag name constants, shared between flag definition and access so a typo
// surfaces at compile time instead of as a runtime "flag accessed but not
// defined" error.
const (
	flagNamespace      = "namespace"
	flagContext        = "context"
	flagKubeconfig     = "kubeconfig"
	flagContainer      = "container"
	flagFollow         = "follow"
	flagLevel          = "level"
	flagPrevious       = "previous"
	flagTimeline       = "timeline"
	flagOutput         = "output"
	flagShort          = "short"
	flagColor          = "color"
	flagNoColor        = "no-color"
	flagTheme          = "theme"
	flagGrep           = "grep"
	flagExclude        = "exclude"
	flagHighlight      = "highlight"
	flagWhere          = "where"
	flagFields         = "fields"
	flagSince          = "since"
	flagTail           = "tail"
	flagTimestamps     = "timestamps"
	flagAllContainers  = "all-containers"
	flagSelector       = "selector"
	flagAllNamespaces  = "all-namespaces"
	flagStats          = "stats"
	flagMaxConcurrency = "max-concurrency"
)

// getStringFlag, getBoolFlag, and getIntFlag wrap Cobra's typed flag getters
// with a consistently worded error, so command option builders don't each
// repeat the "get flag, wrap error" boilerplate.
func getStringFlag(cmd *cobra.Command, name string) (string, error) {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", fmt.Errorf("error getting %s flag: %w", name, err)
	}
	return v, nil
}

func getBoolFlag(cmd *cobra.Command, name string) (bool, error) {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		return false, fmt.Errorf("error getting %s flag: %w", name, err)
	}
	return v, nil
}

func getIntFlag(cmd *cobra.Command, name string) (int, error) {
	v, err := cmd.Flags().GetInt(name)
	if err != nil {
		return 0, fmt.Errorf("error getting %s flag: %w", name, err)
	}
	return v, nil
}
