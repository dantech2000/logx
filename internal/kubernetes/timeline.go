package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/dantech2000/logx/internal/terminal"
	"github.com/fatih/color"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// maxTimelineEvents bounds how many Kubernetes events are pulled into a single
// timeline so a busy pod cannot exhaust memory.
const maxTimelineEvents = 500

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
	Count     int32
}

var timelineEventTypeColors = map[timelineEventType]*color.Color{
	timelineEventTypeNormal:  color.New(color.FgGreen),
	timelineEventTypeWarning: color.New(color.FgYellow),
	timelineEventTypeUnknown: color.New(color.FgHiBlack),
}

// GetTimeline writes pod logs and Kubernetes events together sorted by timestamp.
func (lf *LogFetcher) GetTimeline(ctx context.Context) error {
	if err := lf.prepareLogRequest(ctx); err != nil {
		return err
	}

	items, err := lf.collectLogTimelineItems(ctx)
	logErr := err
	if err != nil {
		items = nil
	}
	eventItems, err := lf.collectEventTimelineItems(ctx, len(items))
	if err != nil {
		return err
	}
	if logErr != nil && len(eventItems) == 0 {
		return logErr
	}
	items = append(items, eventItems...)

	// Logs failed but events are available: degrade gracefully (events often
	// explain why logs are missing, e.g. ImagePullBackOff) while still telling
	// the user that the log stream could not be read.
	if logErr != nil {
		if _, err := fmt.Fprintf(lf.Writer, "[notice] container logs unavailable: %s\n", terminal.Sanitize(logErr.Error())); err != nil {
			return fmt.Errorf("error writing timeline notice: %w", err)
		}
	}

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

func (lf *LogFetcher) prepareLogRequest(ctx context.Context) error {
	if lf.ContainerName == "" {
		containerName, err := lf.getSingleContainerName(ctx)
		if err != nil {
			return fmt.Errorf("failed to get container name: %w", err)
		}
		lf.ContainerName = containerName
	}

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
		hasPrevious, err := lf.hasPreviousContainer(ctx, lf.ContainerName)
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

func (lf *LogFetcher) collectLogTimelineItems(ctx context.Context) ([]timelineItem, error) {
	podLogOpts := corev1.PodLogOptions{
		Container:  lf.ContainerName,
		Follow:     false,
		Previous:   lf.Previous,
		Timestamps: true,
	}

	req := lf.Clientset.CoreV1().Pods(lf.Namespace).GetLogs(lf.PodName, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() { _ = podLogs.Close() }()

	var items []timelineItem
	scanner := logging.NewLineScanner(podLogs)
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

func (lf *LogFetcher) collectEventTimelineItems(ctx context.Context, orderOffset int) ([]timelineItem, error) {
	// Scope the server-side query to events for this pod: this avoids reading the
	// whole namespace's events (which leaks unrelated workloads, needs broad RBAC,
	// and floods the timeline) and bounds the result size.
	listOpts := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.name", lf.PodName).String(),
		Limit:         maxTimelineEvents,
	}
	events, err := lf.Clientset.CoreV1().Events(lf.Namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("error listing events: %w", err)
	}

	var items []timelineItem
	for _, event := range events.Items {
		// Guard client-side as well: not every API server (or test fake) enforces
		// the field selector, so we never surface another object's events here.
		if event.InvolvedObject.Name != lf.PodName {
			continue
		}
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
		Count:     eventCount(event),
	}
}

// eventCount returns how many times an aggregated event has fired, preferring
// the newer Series representation and falling back to the legacy Count field.
func eventCount(event corev1.Event) int32 {
	if event.Series != nil && event.Series.Count > 0 {
		return event.Series.Count
	}
	return event.Count
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
	message := terminal.Sanitize(event.Message)
	if event.Count > 1 {
		message = fmt.Sprintf("%s (x%d)", message, event.Count)
	}
	if event.Object != "" {
		return fmt.Sprintf("%s [EVENT] [%s] %s %s: %s",
			formatTimelineTimestamp(event.Timestamp),
			formatTimelineEventType(event),
			event.Object,
			terminal.Sanitize(event.Reason),
			message)
	}
	return fmt.Sprintf("%s [EVENT] [%s] %s: %s",
		formatTimelineTimestamp(event.Timestamp),
		formatTimelineEventType(event),
		terminal.Sanitize(event.Reason),
		message)
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
