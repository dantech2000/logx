// Package kubernetes provides functionality for interacting with Kubernetes clusters
package kubernetes

import (
	"context"
	"fmt"

	"github.com/dantech2000/logx/internal/terminal"
	"github.com/fatih/color"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ContainerInfo holds information about a container in a pod
type ContainerInfo struct {
	// Name is the container name
	Name string
	// Ready indicates if the container is ready
	Ready bool
	// Status is the current state of the container (Running, Waiting, Terminated)
	Status string
	// Image is the container image
	Image string
	// Kind distinguishes init and ephemeral containers from regular ones. It is
	// empty for a regular container, "init" for an init container, and
	// "ephemeral" for an ephemeral (debug) container.
	Kind string
}

// Container kinds reported in ContainerInfo.Kind.
const (
	containerKindInit      = "init"
	containerKindEphemeral = "ephemeral"
)

// GetContainerState returns a string representation of the container state
func GetContainerState(state corev1.ContainerState) string {
	if state.Running != nil {
		return "Running"
	}
	if state.Waiting != nil {
		return stateWithReason("Waiting", state.Waiting.Reason)
	}
	if state.Terminated != nil {
		return stateWithReason("Terminated", state.Terminated.Reason)
	}
	return "Unknown"
}

// stateWithReason formats a container state, appending the reason in parentheses
// only when one is present (avoids an empty "Waiting ()").
func stateWithReason(state, reason string) string {
	if reason == "" {
		return state
	}
	return fmt.Sprintf("%s (%s)", state, reason)
}

// GetContainerStatus returns the ready state and status string for a container,
// searching regular, init, and ephemeral container statuses.
func GetContainerStatus(pod *corev1.Pod, containerName string) (bool, string) {
	for _, status := range allContainerStatuses(pod) {
		if status.Name == containerName {
			return status.Ready, GetContainerState(status.State)
		}
	}
	return false, "Unknown"
}

// FormatContainerInfo returns a formatted string representation of container information
// with color-coded status indicators
func FormatContainerInfo(info ContainerInfo) string {
	statusColor := color.New(color.FgRed)
	if info.Ready {
		statusColor = color.New(color.FgGreen)
	}

	readySymbol := "✗"
	if info.Ready {
		readySymbol = "✓"
	}

	kindTag := ""
	if info.Kind != "" {
		kindTag = " " + terminal.Sanitize("["+info.Kind+"]")
	}

	return fmt.Sprintf("%s %s%s [%s] (%s)",
		statusColor.Sprint(readySymbol),
		terminal.Sanitize(info.Name),
		kindTag,
		terminal.Sanitize(info.Status),
		terminal.Sanitize(info.Image))
}

// containerSpec is a minimal, common view over corev1.Container and
// corev1.EphemeralContainer — the two have different Kubernetes API types, but
// expose the same name/image shape, so this lets the three spec groups
// (regular, init, ephemeral) be walked once instead of once per helper.
type containerSpec struct {
	name  string
	image string
	kind  string
}

// allContainerSpecs returns every container defined on the pod — regular, init,
// then ephemeral — in a single, stable-ordered slice. The spec-side counterpart
// of allContainerStatuses (timeline.go).
func allContainerSpecs(pod *corev1.Pod) []containerSpec {
	specs := make([]containerSpec, 0,
		len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+len(pod.Spec.EphemeralContainers))
	for _, c := range pod.Spec.Containers {
		specs = append(specs, containerSpec{name: c.Name, image: c.Image})
	}
	for _, c := range pod.Spec.InitContainers {
		specs = append(specs, containerSpec{name: c.Name, image: c.Image, kind: containerKindInit})
	}
	for _, c := range pod.Spec.EphemeralContainers {
		specs = append(specs, containerSpec{name: c.Name, image: c.Image, kind: containerKindEphemeral})
	}
	return specs
}

// ListContainers returns detailed information about containers in a pod
func ListContainers(ctx context.Context, clientset kubernetes.Interface, namespace, podName string) ([]ContainerInfo, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod details: %w", err)
	}

	statuses := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses)+
		len(pod.Status.InitContainerStatuses)+len(pod.Status.EphemeralContainerStatuses))
	for _, s := range allContainerStatuses(pod) {
		statuses[s.Name] = s
	}

	specs := allContainerSpecs(pod)
	containers := make([]ContainerInfo, len(specs))
	for i, spec := range specs {
		ready, status := false, "Unknown"
		if s, ok := statuses[spec.name]; ok {
			ready, status = s.Ready, GetContainerState(s.State)
		}
		containers[i] = ContainerInfo{Name: spec.name, Ready: ready, Status: status, Image: spec.image, Kind: spec.kind}
	}

	return containers, nil
}

// podContainerNames returns every container name whose logs can be fetched —
// regular, then init, then ephemeral — in a stable order.
func podContainerNames(pod *corev1.Pod) []string {
	specs := allContainerSpecs(pod)
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.name
	}
	return names
}

// podHasContainer reports whether the pod defines a container with the given
// name, including init and ephemeral containers (which also have fetchable logs).
func podHasContainer(pod *corev1.Pod, name string) bool {
	for _, s := range allContainerSpecs(pod) {
		if s.name == name {
			return true
		}
	}
	return false
}
