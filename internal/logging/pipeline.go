package logging

import (
	"fmt"
	"io"
	"regexp"
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
	// Include keeps only lines matching at least one pattern (logical OR). An
	// empty slice keeps everything. Patterns match against the original raw line.
	Include []*regexp.Regexp
	// Exclude drops any line matching at least one pattern. It is applied after
	// Include.
	Exclude []*regexp.Regexp
	// Highlight, when true, reverse-video-highlights the Include matches in the
	// rendered output (only when color is enabled).
	Highlight bool
	// Where holds field predicates; an entry must satisfy all of them (AND).
	Where []FieldPredicate
	// Fields, when non-empty, projects output to just these keys (in order)
	// instead of the full formatted line.
	Fields []string
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
	if !p.keep(entry, rawLine) {
		return "", false
	}
	return p.render(entry), true
}

// keep reports whether an entry passes all configured filters. Level filtering
// keeps multi-line entries together (continuation lines inherit their parent's
// level via the tracker); content filters (Include/Exclude) match per line,
// which is the least surprising behavior for a grep-style filter.
func (p *Pipeline) keep(entry LogEntry, rawLine string) bool {
	if entry.Level < p.opts.MinLevel {
		return false
	}
	if len(p.opts.Include) > 0 && !matchesAny(p.opts.Include, rawLine) {
		return false
	}
	if matchesAny(p.opts.Exclude, rawLine) {
		return false
	}
	for _, pred := range p.opts.Where {
		if !pred.Eval(entry) {
			return false
		}
	}
	return true
}

// render turns a kept entry into its output line: either the full formatted line
// or, when Fields is set, a projection of just those keys. Match highlighting is
// applied last so it works in both modes.
func (p *Pipeline) render(entry LogEntry) string {
	var out string
	if len(p.opts.Fields) > 0 {
		out = FormatProjectedEntry(entry, p.opts.Fields)
	} else {
		out = FormatLogEntry(entry)
	}
	if p.opts.Highlight && len(p.opts.Include) > 0 {
		out = highlightMatches(out, p.opts.Include)
	}
	return out
}

// matchesAny reports whether s matches any of the patterns.
func matchesAny(patterns []*regexp.Regexp, s string) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
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
