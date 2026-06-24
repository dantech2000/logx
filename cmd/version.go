package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"

	"github.com/dantech2000/logx/internal/version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// versionData represents the structured version information
type versionData struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	BuildDate string `json:"buildDate" yaml:"buildDate"`
	GoVersion string `json:"goVersion" yaml:"goVersion"`
	OS        string `json:"os" yaml:"os"`
	Arch      string `json:"arch" yaml:"arch"`
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information of logx",
	Long: `Display version information for logx.
	
This command shows detailed information about the build, including:
- Version number (Major.Minor.Patch)
- Git commit hash
- Build date
- Go version
- Operating system and architecture

You can use the --short flag to show only the version number,
or specify an output format using the --output flag.`,
	Example: `  # Show full version information
  logx version
  
  # Show only version number
  logx version --short
  
  # Get version info in JSON format
  logx version --output json
  
  # Get version info in YAML format
  logx version --output yaml`,
	RunE: runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolP("short", "s", false, "Print just the version number")
	versionCmd.Flags().StringP("output", "o", "", "Output format (json or yaml)")
}

func getVersionData(version version.Version) versionData {
	return versionData{
		Version:   version.String(),
		Commit:    version.CommitHash,
		BuildDate: version.BuildDate,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

func runVersion(cmd *cobra.Command, args []string) error {
	short, _ := cmd.Flags().GetBool("short")
	output, _ := cmd.Flags().GetString("output")

	return writeVersion(cmd.OutOrStdout(), version.CurrentVersion, short, output)
}

// writeVersion renders version information to w in the requested form. It is
// separated from the cobra command so it can be unit-tested directly.
func writeVersion(w io.Writer, v version.Version, short bool, output string) error {
	if short {
		_, err := fmt.Fprintln(w, v.String())
		return err
	}

	switch output {
	case "json":
		return printJSON(w, v)
	case "yaml":
		return printYAML(w, v)
	case "":
		_, err := fmt.Fprintln(w, v.FullString())
		return err
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}
}

func printJSON(w io.Writer, v version.Version) error {
	jsonData, err := json.MarshalIndent(getVersionData(v), "", "  ")
	if err != nil {
		return fmt.Errorf("creating JSON output: %w", err)
	}
	_, err = fmt.Fprintln(w, string(jsonData))
	return err
}

func printYAML(w io.Writer, v version.Version) error {
	yamlData, err := yaml.Marshal(getVersionData(v))
	if err != nil {
		return fmt.Errorf("creating YAML output: %w", err)
	}
	_, err = fmt.Fprintln(w, string(yamlData))
	return err
}
