package format

import (
	"fmt"
	"strings"

	"github.com/dantech2000/logx/pkg/kubernetes"
	"github.com/dantech2000/logx/pkg/terminal"
	"github.com/fatih/color"
)

// ContainerList formats the container list in a uniform way
func ContainerList(podName, namespace string, containers []kubernetes.ContainerInfo) string {
	var sb strings.Builder

	// Write header
	fmt.Fprintf(&sb, "\nPod: %s\nNamespace: %s\n\n",
		color.CyanString(terminal.Sanitize(podName)),
		color.CyanString(terminal.Sanitize(namespace)))

	// Write containers, reusing the single-row formatter so the row layout and
	// sanitization live in exactly one place.
	for _, container := range containers {
		fmt.Fprintln(&sb, kubernetes.FormatContainerInfo(container))
	}

	return sb.String()
}
