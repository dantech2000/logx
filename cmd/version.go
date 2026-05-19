package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/dantech2000/logx/pkg/version"
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
	Run: runVersion,
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

func runVersion(cmd *cobra.Command, args []string) {
	short, _ := cmd.Flags().GetBool("short")
	output, _ := cmd.Flags().GetString("output")

	version := version.CurrentVersion

	if short {
		fmt.Println(version.String())
		return
	}

	switch output {
	case "json":
		printJSON(version)
	case "yaml":
		printYAML(version)
	case "":
		fmt.Println(version.FullString())
	default:
		fmt.Printf("Error: unsupported output format %q\n", output)
		os.Exit(1)
	}
}

func printJSON(version version.Version) {
	data := getVersionData(version)
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error creating JSON output: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonData))
}

func printYAML(version version.Version) {
	data := getVersionData(version)
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		fmt.Printf("Error creating YAML output: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(yamlData))
}
