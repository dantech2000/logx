package format

import (
	"fmt"
	"strings"

	"github.com/dantech2000/logx/pkg/kubernetes"
	"github.com/dantech2000/logx/pkg/terminal"
	"github.com/fatih/color"
)

// FormatContainerList formats the container list in a uniform way
func FormatContainerList(podName, namespace string, containers []kubernetes.ContainerInfo) string {
	var sb strings.Builder

	// Write header
	sb.WriteString(fmt.Sprintf("\nPod: %s\nNamespace: %s\n\n",
		color.CyanString(terminal.Sanitize(podName)),
		color.CyanString(terminal.Sanitize(namespace))))

	// Write containers
	for _, container := range containers {
		statusColor := color.New(color.FgRed)
		if container.Ready {
			statusColor = color.New(color.FgGreen)
		}

		readySymbol := "✗"
		if container.Ready {
			readySymbol = "✓"
		}

		sb.WriteString(fmt.Sprintf("%s %s [%s] (%s)\n",
			statusColor.Sprint(readySymbol),
			terminal.Sanitize(container.Name),
			terminal.Sanitize(container.Status),
			terminal.Sanitize(container.Image)))
	}

	return sb.String()
}
