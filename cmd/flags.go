package cmd

// Flag name constants, shared between flag definition and access so a typo
// surfaces at compile time instead of as a runtime "flag accessed but not
// defined" error.
const (
	flagNamespace  = "namespace"
	flagContext    = "context"
	flagKubeconfig = "kubeconfig"
	flagContainer  = "container"
	flagFollow     = "follow"
	flagLevel      = "level"
	flagPrevious   = "previous"
	flagTimeline   = "timeline"
	flagOutput     = "output"
	flagShort      = "short"
)
