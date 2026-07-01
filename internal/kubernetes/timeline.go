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
	pod, err := lf.prepareLogRequest(ctx)
	if err != nil {
		return err
	}

	items, err := lf.collectLogTimelineItems(ctx)
	logErr := err
	if err != nil {
		items = nil
	}
	eventItems, eventsTruncated, err := lf.collectEventTimelineItems(ctx, len(items))
	if err != nil {
		return err
	}
	if logErr != nil && len(eventItems) == 0 {
		return logErr
	}
	items = append(items, eventItems...)

	// Container termination details (exit code, OOMKilled, signal) explain why a
	// container died — something the events don't spell out. Reuses the pod
	// fetched by prepareLogRequest instead of fetching it again.
	items = append(items, lf.collectTerminationItems(pod, len(items))...)

	// Logs failed but events are available: degrade gracefully (events often
	// explain why logs are missing, e.g. ImagePullBackOff) while still telling
	// the user that the log stream could not be read.
	if logErr != nil {
		if _, err := fmt.Fprintf(lf.Writer, "[notice] container logs unavailable: %s\n", terminal.Sanitize(logErr.Error())); err != nil {
			return fmt.Errorf("error writing timeline notice: %w", err)
		}
	}
	// Make event-list truncation visible rather than silently dropping events.
	if eventsTruncated {
		if _, err := fmt.Fprintf(lf.Writer, "[notice] event list truncated to the first %d events\n", maxTimelineEvents); err != nil {
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

// prepareLogRequest fetches the pod once and validates the requested container
// (and, if --previous is set, its restart history) against that single fetch,
// returning the pod so callers with further pod-shaped work (e.g. GetTimeline's
// termination lookup) don't need to fetch it again.
func (lf *LogFetcher) prepareLogRequest(ctx context.Context) (*corev1.Pod, error) {
	pod, err := lf.Clientset.CoreV1().Pods(lf.Namespace).Get(ctx, lf.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error fetching pod details: %w", err)
	}

	if lf.ContainerName == "" {
		containerName, err := lf.selectContainerName(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get container name: %w", err)
		}
		lf.ContainerName = containerName
	}

	if !podHasContainer(pod, lf.ContainerName) {
		return nil, fmt.Errorf("container %q not found in pod %q", lf.ContainerName, lf.PodName)
	}

	if lf.Previous {
		hasPrevious, err := lf.previousContainerTerminated(pod, lf.ContainerName)
		if err != nil {
			return nil, fmt.Errorf("failed to check for previous container: %w", err)
		}
		if !hasPrevious {
			return nil, fmt.Errorf("no previous terminated container found for %q in pod %q\nNote: The -p flag only works for containers that have terminated or restarted",
				lf.ContainerName, lf.PodName)
		}
	}

	return pod, nil
}

func (lf *LogFetcher) collectLogTimelineItems(ctx context.Context) ([]timelineItem, error) {
	// --since/--tail bound the log portion of the timeline just as they bound a
	// plain log fetch. Follow and Timestamps are forced regardless of the fetcher's
	// own settings: the timeline never follows, and it always needs timestamps to
	// sort by; --tail limits the trailing log lines, while events are bounded
	// separately by maxTimelineEvents.
	podLogOpts := lf.podLogOptions(lf.ContainerName)
	podLogOpts.Follow = false
	podLogOpts.Timestamps = true

	req := lf.Clientset.CoreV1().Pods(lf.Namespace).GetLogs(lf.PodName, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() { _ = podLogs.Close() }()

	var items []timelineItem
	var tracker logging.LevelTracker
	scanner := logging.NewLineReader(podLogs)
	for scanner.Scan() {
		rawLine := scanner.Text()
		entry := logging.ParseKubernetesLogEntry(rawLine)
		// Continuation lines inherit the level of the entry they belong to so a
		// multi-line entry (e.g. a stack trace) is filtered as a unit.
		entry.Level = tracker.Effective(entry, rawLine)
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

// collectEventTimelineItems returns the timeline items for this pod's events and
// reports whether the server-side list was truncated by the result limit.
func (lf *LogFetcher) collectEventTimelineItems(ctx context.Context, orderOffset int) ([]timelineItem, bool, error) {
	// Scope the server-side query to events for this pod: this avoids reading the
	// whole namespace's events (which leaks unrelated workloads, needs broad RBAC,
	// and floods the timeline) and bounds the result size.
	listOpts := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.name", lf.PodName).String(),
		Limit:         maxTimelineEvents,
	}
	events, err := lf.Clientset.CoreV1().Events(lf.Namespace).List(ctx, listOpts)
	if err != nil {
		return nil, false, fmt.Errorf("error listing events: %w", err)
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
	// A non-empty Continue token means more events matched than the limit returned.
	truncated := events.Continue != ""
	return items, truncated, nil
}

// collectTerminationItems emits a timeline item for each terminated state of the
// target container — the current termination (if the container has exited) and
// the previous instance's termination (from LastTerminationState) — carrying the
// exit code, reason (e.g. OOMKilled), and signal.
func (lf *LogFetcher) collectTerminationItems(pod *corev1.Pod, orderOffset int) []timelineItem {
	var items []timelineItem
	for _, cs := range allContainerStatuses(pod) {
		if cs.Name != lf.ContainerName {
			continue
		}
		if t := cs.LastTerminationState.Terminated; t != nil {
			items = append(items, terminationTimelineItem(*t, cs.Name, true, orderOffset+len(items)))
		}
		if t := cs.State.Terminated; t != nil {
			items = append(items, terminationTimelineItem(*t, cs.Name, false, orderOffset+len(items)))
		}
	}
	return items
}

// allContainerStatuses returns the statuses of regular, init, and ephemeral
// containers in a single slice.
func allContainerStatuses(pod *corev1.Pod) []corev1.ContainerStatus {
	statuses := make([]corev1.ContainerStatus, 0,
		len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses)+len(pod.Status.EphemeralContainerStatuses))
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.EphemeralContainerStatuses...)
	return statuses
}

func terminationTimelineItem(term corev1.ContainerStateTerminated, container string, previous bool, order int) timelineItem {
	return timelineItem{
		timestamp: term.FinishedAt.Time,
		order:     order,
		line:      formatTermination(term, container, previous),
	}
}

// formatTermination renders a [TERM] timeline line. A non-zero exit code or an
// OOMKilled reason is highlighted, since those are the failures users hunt for.
func formatTermination(term corev1.ContainerStateTerminated, container string, previous bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "container %q exited with code %d", container, term.ExitCode)
	if term.Signal != 0 {
		fmt.Fprintf(&b, " (signal %d)", term.Signal)
	}
	if term.Reason != "" {
		fmt.Fprintf(&b, " — %s", term.Reason)
	}
	if msg := strings.TrimSpace(term.Message); msg != "" {
		fmt.Fprintf(&b, ": %s", msg)
	}

	bad := term.ExitCode != 0 || strings.EqualFold(term.Reason, "OOMKilled")
	phase := ""
	if previous {
		phase = " (previous)"
	}
	return fmt.Sprintf("%s [%s]%s %s",
		formatTimelineTimestamp(term.FinishedAt.Time),
		terminationColor(bad).Sprint("TERM"),
		phase,
		terminal.Sanitize(b.String()))
}

func terminationColor(bad bool) *color.Color {
	if bad {
		return color.New(color.FgRed, color.Bold)
	}
	return color.New(color.FgHiBlack)
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
