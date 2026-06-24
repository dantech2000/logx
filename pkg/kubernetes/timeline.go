package kubernetes

import (
	"bufio"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dantech2000/logx/pkg/logging"
	"github.com/dantech2000/logx/pkg/terminal"
	"github.com/fatih/color"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type timelineEventType int

const (
	timelineEventTypeNormal timelineEventType = iota
	timelineEventTypeWarning
	timelineEventTypeUnknown
)

type timelineItem struct {
	timestamp time.Time
	order     int
	line      string
}

type timelineEvent struct {
	Timestamp time.Time
	Type      timelineEventType
	RawType   string
	Object    string
	Reason    string
	Message   string
}

var timelineEventTypeColors = map[timelineEventType]*color.Color{
	timelineEventTypeNormal:  color.New(color.FgGreen),
	timelineEventTypeWarning: color.New(color.FgYellow),
	timelineEventTypeUnknown: color.New(color.FgHiBlack),
}

// GetTimeline writes pod logs and Kubernetes events together sorted by timestamp.
func (lf *LogFetcher) GetTimeline() error {
	if err := lf.prepareLogRequest(); err != nil {
		return err
	}

	items, err := lf.collectLogTimelineItems()
	logErr := err
	if err != nil {
		items = nil
	}
	eventItems, err := lf.collectEventTimelineItems(len(items))
	if err != nil {
		return err
	}
	if logErr != nil && len(eventItems) == 0 {
		return logErr
	}
	items = append(items, eventItems...)

	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.timestamp.IsZero() && right.timestamp.IsZero() {
			return left.order < right.order
		}
		if left.timestamp.IsZero() {
			return false
		}
		if right.timestamp.IsZero() {
			return true
		}
		if left.timestamp.Equal(right.timestamp) {
			return left.order < right.order
		}
		return left.timestamp.Before(right.timestamp)
	})

	for _, item := range items {
		if _, err := fmt.Fprintln(lf.Writer, item.line); err != nil {
			return fmt.Errorf("error writing timeline item: %w", err)
		}
	}
	return nil
}

func (lf *LogFetcher) prepareLogRequest() error {
	if lf.ContainerName == "" {
		containerName, err := lf.getSingleContainerName()
		if err != nil {
			return fmt.Errorf("failed to get container name: %w", err)
		}
		lf.ContainerName = containerName
	}

	ctx := context.Background()
	pod, err := lf.Clientset.CoreV1().Pods(lf.Namespace).Get(ctx, lf.PodName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error fetching pod details: %w", err)
	}

	containerExists := false
	for _, container := range pod.Spec.Containers {
		if container.Name == lf.ContainerName {
			containerExists = true
			break
		}
	}
	if !containerExists {
		return fmt.Errorf("container '%s' not found in pod '%s'", lf.ContainerName, lf.PodName)
	}

	if lf.Previous {
		hasPrevious, err := lf.hasPreviousContainer(lf.ContainerName)
		if err != nil {
			return fmt.Errorf("failed to check for previous container: %w", err)
		}
		if !hasPrevious {
			return fmt.Errorf("no previous terminated container found for '%s' in pod '%s'\nNote: The -p flag only works for containers that have terminated or restarted",
				lf.ContainerName, lf.PodName)
		}
	}

	return nil
}

func (lf *LogFetcher) collectLogTimelineItems() ([]timelineItem, error) {
	podLogOpts := corev1.PodLogOptions{
		Container:  lf.ContainerName,
		Follow:     false,
		Previous:   lf.Previous,
		Timestamps: true,
	}

	req := lf.Clientset.CoreV1().Pods(lf.Namespace).GetLogs(lf.PodName, &podLogOpts)
	podLogs, err := req.Stream(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() { _ = podLogs.Close() }()

	var items []timelineItem
	scanner := bufio.NewScanner(podLogs)
	scanner.Buffer(make([]byte, 64*1024), maxLogLineSize)
	for scanner.Scan() {
		entry := logging.ParseKubernetesLogEntry(scanner.Text())
		if entry.Level < lf.FilterLevel {
			continue
		}
		items = append(items, timelineItem{
			timestamp: entry.Timestamp,
			order:     len(items),
			line:      formatTimelineLog(entry),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log stream: %w", err)
	}
	return items, nil
}

func (lf *LogFetcher) collectEventTimelineItems(orderOffset int) ([]timelineItem, error) {
	events, err := lf.Clientset.CoreV1().Events(lf.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing events: %w", err)
	}

	var items []timelineItem
	for _, event := range events.Items {
		timelineEvent := newTimelineEvent(event)
		items = append(items, timelineItem{
			timestamp: timelineEvent.Timestamp,
			order:     orderOffset + len(items),
			line:      formatTimelineEvent(timelineEvent),
		})
	}
	return items, nil
}

func newTimelineEvent(event corev1.Event) timelineEvent {
	eventType, rawType := parseTimelineEventType(event.Type)
	reason := event.Reason
	if reason == "" {
		reason = "Event"
	}
	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = reason
	}
	return timelineEvent{
		Timestamp: eventTimestamp(event),
		Type:      eventType,
		RawType:   rawType,
		Object:    formatEventObject(event.InvolvedObject),
		Reason:    reason,
		Message:   message,
	}
}

func parseTimelineEventType(rawType string) (timelineEventType, string) {
	switch rawType {
	case "", corev1.EventTypeNormal:
		return timelineEventTypeNormal, corev1.EventTypeNormal
	case corev1.EventTypeWarning:
		return timelineEventTypeWarning, corev1.EventTypeWarning
	default:
		return timelineEventTypeUnknown, rawType
	}
}

func eventTimestamp(event corev1.Event) time.Time {
	switch {
	case !event.EventTime.IsZero():
		return event.EventTime.Time
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time
	case !event.FirstTimestamp.IsZero():
		return event.FirstTimestamp.Time
	default:
		return event.CreationTimestamp.Time
	}
}

func formatTimelineLog(entry logging.LogEntry) string {
	message := entry.Message
	if message == "" {
		message = entry.RawLine
	}
	return fmt.Sprintf("%s [LOG] %s %s",
		formatTimelineTimestamp(entry.Timestamp),
		logging.FormatLogLevelLabel(entry.Level),
		formatTimelineLogMessage(entry, message))
}

func formatTimelineEvent(event timelineEvent) string {
	if event.Object != "" {
		return fmt.Sprintf("%s [EVENT] [%s] %s %s: %s",
			formatTimelineTimestamp(event.Timestamp),
			formatTimelineEventType(event),
			event.Object,
			terminal.Sanitize(event.Reason),
			terminal.Sanitize(event.Message))
	}
	return fmt.Sprintf("%s [EVENT] [%s] %s: %s",
		formatTimelineTimestamp(event.Timestamp),
		formatTimelineEventType(event),
		terminal.Sanitize(event.Reason),
		terminal.Sanitize(event.Message))
}

func formatTimelineEventType(event timelineEvent) string {
	label := terminal.Sanitize(event.RawType)
	if label == "" {
		label = corev1.EventTypeNormal
	}
	eventColor, ok := timelineEventTypeColors[event.Type]
	if !ok {
		eventColor = timelineEventTypeColors[timelineEventTypeUnknown]
	}
	return eventColor.Sprint(label)
}

func formatTimelineLogMessage(entry logging.LogEntry, fallback string) string {
	details := logging.FormatLogEntryDetails(entry)
	if strings.TrimSpace(details) != "" {
		return details
	}
	return terminal.Sanitize(fallback)
}

func formatEventObject(ref corev1.ObjectReference) string {
	if ref.Kind == "" && ref.Name == "" {
		return ""
	}
	if ref.Kind == "" {
		return terminal.Sanitize(ref.Name)
	}
	if ref.Name == "" {
		return terminal.Sanitize(strings.ToLower(ref.Kind))
	}
	return terminal.Sanitize(fmt.Sprintf("%s/%s", strings.ToLower(ref.Kind), ref.Name))
}

func formatTimelineTimestamp(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "[no timestamp]"
	}
	return fmt.Sprintf("[%s]", timestamp.UTC().Format("2006-01-02 15:04:05"))
}
