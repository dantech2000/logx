// Package kubernetes provides functionality for interacting with Kubernetes clusters
package kubernetes

import (
	"context"
	"fmt"

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
}

// GetContainerState returns a string representation of the container state
func GetContainerState(state corev1.ContainerState) string {
	if state.Running != nil {
		return "Running"
	}
	if state.Waiting != nil {
		return fmt.Sprintf("Waiting (%s)", state.Waiting.Reason)
	}
	if state.Terminated != nil {
		return fmt.Sprintf("Terminated (%s)", state.Terminated.Reason)
	}
	return "Unknown"
}

// GetContainerStatus returns the ready state and status string for a container
func GetContainerStatus(pod *corev1.Pod, containerName string) (bool, string) {
	for _, status := range pod.Status.ContainerStatuses {
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

	return fmt.Sprintf("%s %s [%s] (%s)",
		statusColor.Sprint(readySymbol),
		info.Name,
		info.Status,
		info.Image)
}

// ListContainers returns detailed information about containers in a pod
func ListContainers(clientset kubernetes.Interface, namespace, podName string) ([]ContainerInfo, error) {
	ctx := context.Background()
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod details: %w", err)
	}

	containers := make([]ContainerInfo, len(pod.Spec.Containers))
	for i, container := range pod.Spec.Containers {
		ready, status := GetContainerStatus(pod, container.Name)
		containers[i] = ContainerInfo{
			Name:   container.Name,
			Ready:  ready,
			Status: status,
			Image:  container.Image,
		}
	}

	return containers, nil
}
