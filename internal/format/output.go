package format

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dantech2000/logx/internal/kubernetes"
	"github.com/dantech2000/logx/internal/terminal"
	"gopkg.in/yaml.v2"
)

// OutputFormatter renders a pod's container list in a selectable output format
// (table, json, yaml, or posix).
type OutputFormatter struct {
	PodName    string
	Namespace  string
	Containers []kubernetes.ContainerInfo
}

// containerListDTO is the stable, machine-readable shape emitted by the json and
// yaml formats. It is owned by the format package so the serialized contract is
// decoupled from the internal kubernetes data types, and uses lowercase keys
// rather than the Go field names. All string fields are sanitized so that
// untrusted values (e.g. a crafted image reference) cannot inject terminal
// escape sequences regardless of output format.
type containerListDTO struct {
	Pod        string         `json:"pod" yaml:"pod"`
	Namespace  string         `json:"namespace" yaml:"namespace"`
	Containers []containerDTO `json:"containers" yaml:"containers"`
}

type containerDTO struct {
	Name   string `json:"name" yaml:"name"`
	Ready  bool   `json:"ready" yaml:"ready"`
	Status string `json:"status" yaml:"status"`
	Image  string `json:"image" yaml:"image"`
	// Kind is "init" or "ephemeral" for those container types; omitted for
	// regular containers.
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
}

func (of *OutputFormatter) toDTO() containerListDTO {
	containers := make([]containerDTO, len(of.Containers))
	for i, c := range of.Containers {
		containers[i] = containerDTO{
			Name:   terminal.Sanitize(c.Name),
			Ready:  c.Ready,
			Status: terminal.Sanitize(c.Status),
			Image:  terminal.Sanitize(c.Image),
			Kind:   terminal.Sanitize(c.Kind),
		}
	}
	return containerListDTO{
		Pod:        terminal.Sanitize(of.PodName),
		Namespace:  terminal.Sanitize(of.Namespace),
		Containers: containers,
	}
}

// NewOutputFormatter creates a new OutputFormatter
func NewOutputFormatter(podName, namespace string, containers []kubernetes.ContainerInfo) *OutputFormatter {
	return &OutputFormatter{
		PodName:    podName,
		Namespace:  namespace,
		Containers: containers,
	}
}

// FormatOutput formats the output based on the specified format. An empty format
// selects the human-readable table.
//
// An unrecognized format is an error rather than a silent fall-through to the
// table: `logx containers pod -o jsonn | jq` previously exited 0 having emitted
// ANSI-colored table text, and a mistyped or wrong-command value (`-o text`,
// which is valid on `logs`, or `-o JSON`) failed the same silent way.
func (of *OutputFormatter) FormatOutput(format string) (string, error) {
	switch format {
	case "":
		return ContainerList(of.PodName, of.Namespace, of.Containers), nil
	case "json":
		return of.formatJSON()
	case "yaml":
		return of.formatYAML()
	case "posix":
		return of.formatPOSIX()
	default:
		return "", fmt.Errorf("unknown output format %q (valid: json, yaml, posix, or omit for a table)", format)
	}
}

func (of *OutputFormatter) formatJSON() (string, error) {
	jsonData, err := json.MarshalIndent(of.toDTO(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling to JSON: %w", err)
	}
	return string(jsonData), nil
}

func (of *OutputFormatter) formatYAML() (string, error) {
	yamlData, err := yaml.Marshal(of.toDTO())
	if err != nil {
		return "", fmt.Errorf("error marshalling to YAML: %w", err)
	}
	return string(yamlData), nil
}

func (of *OutputFormatter) formatPOSIX() (string, error) {
	names := make([]string, 0, len(of.Containers))
	for _, container := range of.Containers {
		names = append(names, terminal.Sanitize(container.Name))
	}
	return strings.Join(names, "\n"), nil
}
