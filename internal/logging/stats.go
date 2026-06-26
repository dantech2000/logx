package logging

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stats accumulates a digest over the entries a pipeline keeps: counts by level,
// HTTP status classes, the most frequent (templated) messages, and the time
// span. It is written once at the end of a run by --stats.
//
// A single Stats may be shared by several concurrent pipelines (one per stream
// in --all-containers/--selector mode); Record is guarded by a mutex so the
// aggregate is correct across streams. Total/Write are also guarded, though they
// are normally called only after every stream has finished.
type Stats struct {
	mu          sync.Mutex
	total       int
	byLevel     map[LogLevel]int
	statusClass map[int]int // keyed by class (2,3,4,5) → count
	messages    map[string]int
	firstTS     time.Time
	lastTS      time.Time
}

// NewStats returns an empty accumulator.
func NewStats() *Stats {
	return &Stats{
		byLevel:     make(map[LogLevel]int),
		statusClass: make(map[int]int),
		messages:    make(map[string]int),
	}
}

var (
	statsHexRunRegex = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	statsNumberRegex = regexp.MustCompile(`\d+`)
)

// Record folds one kept entry into the digest. It is safe to call concurrently
// from multiple streams.
func (s *Stats) Record(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	s.byLevel[entry.Level]++

	if !entry.Timestamp.IsZero() {
		ts := entry.Timestamp.UTC()
		if s.firstTS.IsZero() || ts.Before(s.firstTS) {
			s.firstTS = ts
		}
		if s.lastTS.IsZero() || ts.After(s.lastTS) {
			s.lastTS = ts
		}
	}

	if class, ok := statusClassOf(entry); ok {
		s.statusClass[class]++
	}

	if msg := templateMessage(messageOf(entry)); msg != "" {
		s.messages[msg]++
	}
}

// Total reports how many entries were recorded.
func (s *Stats) Total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// messageOf returns the best display message for stats grouping.
func messageOf(entry LogEntry) string {
	if entry.Message != "" && entry.Message != entry.RawLine {
		return entry.Message
	}
	return entry.RawLine
}

// templateMessage collapses variable tokens (long hex/UUID runs, then numbers)
// to "#" so that otherwise-identical messages group together.
func templateMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = statsHexRunRegex.ReplaceAllString(msg, "#")
	msg = statsNumberRegex.ReplaceAllString(msg, "#")
	return msg
}

// statusClassOf returns the HTTP status class digit (2..5) for an entry that
// carries a recognized status field.
func statusClassOf(entry LogEntry) (int, bool) {
	if entry.Fields == nil {
		return 0, false
	}
	val, ok := firstField(entry.Fields, httpStatusFields...)
	if !ok {
		return 0, false
	}
	status, ok := toInt(val)
	if !ok || status < 100 || status > 599 {
		return 0, false
	}
	return status / 100, true
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		i, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", v)))
		return i, err == nil
	}
}

// Write renders the digest to w in one write. The level counts are colorized
// with the active theme (so they obey --no-color), ordered from most to least
// severe.
func (s *Stats) Write(w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	fmt.Fprintln(&b, quoteColor().Sprint("── logx stats ──"))
	fmt.Fprintf(&b, "lines: %d", s.total)
	if !s.firstTS.IsZero() {
		fmt.Fprintf(&b, "   span: %s → %s",
			s.firstTS.Format("2006-01-02 15:04:05"),
			s.lastTS.Format("2006-01-02 15:04:05"))
	}
	fmt.Fprintln(&b)

	s.writeLevels(&b)
	s.writeStatusClasses(&b)
	s.writeTopMessages(&b, 5)

	_, err := io.WriteString(w, b.String())
	return err
}

func (s *Stats) writeLevels(b *strings.Builder) {
	if len(s.byLevel) == 0 {
		return
	}
	parts := make([]string, 0, len(s.byLevel))
	for _, level := range []LogLevel{FATAL, ERROR, WARN, INFO, DEBUG, TRACE} {
		if c, ok := s.byLevel[level]; ok {
			parts = append(parts, fmt.Sprintf("%s %d", levelColorFor(level).Sprint(level.String()), c))
		}
	}
	fmt.Fprintf(b, "levels: %s\n", strings.Join(parts, "  "))
}

func (s *Stats) writeStatusClasses(b *strings.Builder) {
	if len(s.statusClass) == 0 {
		return
	}
	classes := make([]int, 0, len(s.statusClass))
	for c := range s.statusClass {
		classes = append(classes, c)
	}
	sort.Ints(classes)
	parts := make([]string, 0, len(classes))
	for _, c := range classes {
		parts = append(parts, fmt.Sprintf("%dxx %d", c, s.statusClass[c]))
	}
	fmt.Fprintf(b, "status: %s\n", strings.Join(parts, "  "))
}

func (s *Stats) writeTopMessages(b *strings.Builder, n int) {
	if len(s.messages) == 0 {
		return
	}
	type kv struct {
		msg   string
		count int
	}
	ranked := make([]kv, 0, len(s.messages))
	for m, c := range s.messages {
		ranked = append(ranked, kv{m, c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].msg < ranked[j].msg
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	fmt.Fprintln(b, "top messages:")
	for _, r := range ranked {
		fmt.Fprintf(b, "  %5d  %s\n", r.count, r.msg)
	}
}
