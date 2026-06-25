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
	statusGroups := [][]corev1.ContainerStatus{
		pod.Status.ContainerStatuses,
		pod.Status.InitContainerStatuses,
		pod.Status.EphemeralContainerStatuses,
	}
	for _, group := range statusGroups {
		for _, status := range group {
			if status.Name == containerName {
				return status.Ready, GetContainerState(status.State)
			}
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

// ListContainers returns detailed information about containers in a pod
func ListContainers(ctx context.Context, clientset kubernetes.Interface, namespace, podName string) ([]ContainerInfo, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod details: %w", err)
	}

	var containers []ContainerInfo
	for _, container := range pod.Spec.Containers {
		containers = append(containers, containerInfoFor(pod, container.Name, container.Image, ""))
	}
	for _, container := range pod.Spec.InitContainers {
		containers = append(containers, containerInfoFor(pod, container.Name, container.Image, containerKindInit))
	}
	for _, container := range pod.Spec.EphemeralContainers {
		containers = append(containers, containerInfoFor(pod, container.Name, container.Image, containerKindEphemeral))
	}

	return containers, nil
}

// podHasContainer reports whether the pod defines a container with the given
// name, including init and ephemeral containers (which also have fetchable logs).
func podHasContainer(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			return true
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if c.Name == name {
			return true
		}
	}
	for _, c := range pod.Spec.EphemeralContainers {
		if c.Name == name {
			return true
		}
	}
	return false
}

func containerInfoFor(pod *corev1.Pod, name, image, kind string) ContainerInfo {
	ready, status := GetContainerStatus(pod, name)
	return ContainerInfo{
		Name:   name,
		Ready:  ready,
		Status: status,
		Image:  image,
		Kind:   kind,
	}
}
