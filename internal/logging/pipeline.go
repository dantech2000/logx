package logging

import (
	"fmt"
	"io"
	"strings"
)

// Pipeline is the single parse → group → filter → render path shared by both
// `logx parse` and `logx logs`. Centralizing it means every filtering and
// formatting feature (level, and—built on this—grep, field predicates,
// projection, JSON output) applies identically to a file, a pipe, or a live pod
// stream.
//
// A Pipeline is stateful: it carries a LevelTracker so the indented frames of a
// multi-line entry (e.g. a stack trace) inherit their parent's level and are
// kept or dropped as a unit. It is therefore NOT safe for concurrent use; create
// one Pipeline per log stream.
type Pipeline struct {
	opts    PipelineOptions
	tracker LevelTracker
}

// PipelineOptions configures a Pipeline. The zero value emits every non-blank
// line (MinLevel TRACE) rendered with the default formatter.
type PipelineOptions struct {
	// MinLevel drops entries whose effective level is below it.
	MinLevel LogLevel
}

// NewPipeline returns a Pipeline configured by opts.
func NewPipeline(opts PipelineOptions) *Pipeline {
	return &Pipeline{opts: opts}
}

// ProcessLine handles a single raw log line (without a trailing newline) and
// returns the rendered output and whether it should be emitted. Blank or
// whitespace-only lines are dropped. The line may carry a kubelet --timestamps
// prefix, which is recognized and used as the entry timestamp.
//
// The full (untrimmed) line is passed to the parser and the level tracker so
// leading indentation—which marks a continuation line—is preserved.
func (p *Pipeline) ProcessLine(rawLine string) (string, bool) {
	if strings.TrimSpace(rawLine) == "" {
		return "", false
	}
	entry := ParseKubernetesLogEntry(rawLine)
	entry.Level = p.tracker.Effective(entry, rawLine)
	if !p.keep(entry) {
		return "", false
	}
	return p.render(entry), true
}

// keep reports whether an entry passes all configured filters.
func (p *Pipeline) keep(entry LogEntry) bool {
	return entry.Level >= p.opts.MinLevel
}

// render turns a kept entry into its output line.
func (p *Pipeline) render(entry LogEntry) string {
	return FormatLogEntry(entry)
}

// Run reads newline-delimited lines from r, processes each, and writes the
// emitted lines to w. It uses the bounded LineReader, so an over-long line is
// truncated rather than aborting the stream.
func (p *Pipeline) Run(r io.Reader, w io.Writer) error {
	scanner := NewLineReader(r)
	for scanner.Scan() {
		out, ok := p.ProcessLine(scanner.Text())
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(w, out); err != nil {
			return err
		}
	}
	return scanner.Err()
}
