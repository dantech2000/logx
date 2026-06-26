package cmd

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
