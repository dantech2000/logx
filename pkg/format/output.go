package format

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dantech2000/logx/pkg/kubernetes"
	"gopkg.in/yaml.v2"
)

type OutputFormatter struct {
	PodName    string
	Namespace  string
	Containers []kubernetes.ContainerInfo
}

// NewOutputFormatter creates a new OutputFormatter
func NewOutputFormatter(podName, namespace string, containers []kubernetes.ContainerInfo) *OutputFormatter {
	return &OutputFormatter{
		PodName:    podName,
		Namespace:  namespace,
		Containers: containers,
	}
}

// FormatOutput formats the output based on the specified format
func (of *OutputFormatter) FormatOutput(format string) (string, error) {
	switch format {
	case "json":
		return of.formatJSON()
	case "yaml":
		return of.formatYAML()
	case "posix":
		return of.formatPOSIX()
	default:
		return FormatContainerList(of.PodName, of.Namespace, of.Containers), nil
	}
}

func (of *OutputFormatter) formatJSON() (string, error) {
	jsonData, err := json.MarshalIndent(of, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling to JSON: %w", err)
	}
	return string(jsonData), nil
}

func (of *OutputFormatter) formatYAML() (string, error) {
	yamlData, err := yaml.Marshal(of)
	if err != nil {
		return "", fmt.Errorf("error marshalling to YAML: %w", err)
	}
	return string(yamlData), nil
}

func (of *OutputFormatter) formatPOSIX() (string, error) {
	var names []string
	for _, container := range of.Containers {
		names = append(names, container.Name)
	}
	return strings.Join(names, "\n"), nil
}
