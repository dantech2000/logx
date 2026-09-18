package logging

import (
	"context"
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
	opts PipelineOptions
	// fields is opts.Fields with each key's fieldKind classified once at
	// construction, so the per-line projection renderers don't re-scan the
	// virtual-key groups for every field on every line.
	fields  []projectedField
	tracker LevelTracker
	stats   *Stats
	// filter applies level and content filtering with multi-line entries kept
	// as units; shared with the --timeline view so both filter identically.
	filter GroupFilter
	// tail, when non-nil, retains the last N anchor-led groups instead of
	// emitting lines immediately, so --tail counts complete entries after
	// filtering rather than raw lines before it. Enable via EnableTail; nil
	// (the default) preserves the immediate streaming behavior.
	tail *TailBuffer[retainedLine]
}

// retainedLine is one buffered tail entry: the parsed entry (for a deferred
// --stats digest over exactly the window shown) plus its rendered form.
type retainedLine struct {
	entry    LogEntry
	rendered string
}

// EnableTail switches the pipeline to windowed mode: kept lines are retained
// and FlushRetained releases them. Stats, when collected, are recorded at
// flush time so the digest covers the retained window rather than the whole
// stream read to fill it.
func (p *Pipeline) EnableTail(maxAnchors int) {
	p.tail = NewTailBuffer[retainedLine](maxAnchors)
}

// FlushRetained releases the tail window: the rendered lines in stream order,
// or nil in stats mode (where it records the digest over the retained window
// instead) and when tail mode is off. Call once after the stream ends; the
// window drains, so a second call yields nothing.
func (p *Pipeline) FlushRetained() []string {
	if p.tail == nil {
		return nil
	}
	retained := p.tail.Lines()
	p.tail.Clear()
	if p.stats != nil {
		for _, line := range retained {
			p.stats.Record(line.entry)
		}
		return nil
	}
	out := make([]string, 0, len(retained))
	for _, line := range retained {
		out = append(out, line.rendered)
	}
	return out
}

// projectedField pairs a --fields projection key with its precomputed kind.
type projectedField struct {
	key  string
	kind fieldKind
}

// classifyFields classifies each projection key once.
func classifyFields(keys []string) []projectedField {
	if len(keys) == 0 {
		return nil
	}
	out := make([]projectedField, len(keys))
	for i, k := range keys {
		out[i] = projectedField{key: k, kind: classifyKey(k)}
	}
	return out
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
	// Output selects the rendering format (text or JSON/NDJSON).
	Output OutputFormat
	// CollectStats accumulates a digest over kept entries and suppresses normal
	// per-line output; the caller writes the summary after the run via Stats().
	CollectStats bool
}

// NewPipeline returns a Pipeline configured by opts.
func NewPipeline(opts PipelineOptions) *Pipeline {
	p := &Pipeline{opts: opts, fields: classifyFields(opts.Fields), filter: NewGroupFilter(opts.MinLevel, opts)}
	if opts.CollectStats {
		p.stats = NewStats()
	}
	return p
}

// NewPipelineWithStats returns a Pipeline that records its digest into the
// provided shared Stats instead of a private one. Several pipelines (one per
// concurrent stream in --all-containers/--selector mode) can share a single
// thread-safe Stats so --stats aggregates across every stream. CollectStats is
// implied, so per-line output is suppressed exactly as in single-stream stats
// mode.
func NewPipelineWithStats(opts PipelineOptions, stats *Stats) *Pipeline {
	opts.CollectStats = true
	return &Pipeline{opts: opts, fields: classifyFields(opts.Fields), filter: NewGroupFilter(opts.MinLevel, opts), stats: stats}
}

// Stats returns the accumulated digest, or nil if CollectStats was not set.
func (p *Pipeline) Stats() *Stats { return p.stats }

// ProcessLine handles a single raw log line (without a trailing newline) and
// returns the rendered output and whether it should be emitted. Blank or
// whitespace-only lines are dropped. The line may carry a kubelet --timestamps
// prefix, which is recognized and used as the entry timestamp.
//
// The full (untrimmed) line is passed to the parser and the level tracker so
// leading indentation—which marks a continuation line—is preserved.
func (p *Pipeline) ProcessLine(rawLine string) (string, bool) {
	out, ok, _ := p.ProcessLineDetail(rawLine)
	return out, ok
}

// ProcessLineDetail behaves like ProcessLine and additionally reports the
// line's group role. Callers that must keep multi-line entries whole (notably
// the --tail window, which counts entries rather than raw lines) use the role
// to buffer anchors and continuations together. A dropped line reports its
// role anyway, but callers only buffer emitted lines.
func (p *Pipeline) ProcessLineDetail(rawLine string) (string, bool, GroupRole) {
	if strings.TrimSpace(rawLine) == "" {
		return "", false, GroupAnchor
	}
	entry := ParseKubernetesLogEntry(rawLine)
	level, role := p.tracker.Classify(entry, rawLine)
	entry.Level = level
	if !p.keep(entry, rawLine, role) {
		return "", false, role
	}
	if p.tail != nil {
		// Windowed mode defers both rendering and stats: the digest must
		// cover the retained window, and lines emit at flush time.
		if p.stats == nil {
			out, ok := p.render(entry)
			if !ok {
				return "", false, role
			}
			p.tail.Add(retainedLine{entry: entry, rendered: out}, role)
		} else {
			p.tail.Add(retainedLine{entry: entry}, role)
		}
		return "", false, role
	}
	if p.stats != nil {
		p.stats.Record(entry)
		// In stats mode the digest is the output; suppress the per-line render.
		return "", false, role
	}
	out, ok := p.render(entry)
	return out, ok, role
}

// keep reports whether an entry passes all configured filters. It delegates
// to a GroupFilter so the --timeline view filters entries exactly the same
// way rather than reimplementing the join rules.
func (p *Pipeline) keep(entry LogEntry, rawLine string, role GroupRole) bool {
	return p.filter.Keep(entry, rawLine, role)
}

// MatchesContent reports whether an entry passes the content filters — Include
// (logical OR), then Exclude, then every Where predicate (AND). Level filtering
// is deliberately not included: callers apply their own level floor, and the
// timeline in particular tracks it separately from these options.
//
// Exported so the --timeline view filters identically to the main pipeline
// rather than reimplementing (or, as it once did, silently skipping) --grep,
// --exclude, and --where.
func (o PipelineOptions) MatchesContent(entry LogEntry, rawLine string) bool {
	if len(o.Include) > 0 && !matchesAny(o.Include, rawLine) {
		return false
	}
	if matchesAny(o.Exclude, rawLine) {
		return false
	}
	for _, pred := range o.Where {
		if !pred.Eval(entry) {
			return false
		}
	}
	return true
}

// render turns a kept entry into its output line: either the full formatted line
// or, when Fields is set, a projection of just those keys. Match highlighting is
// applied last so it works in both modes.
//
// It reports false when a projection resolved none of the requested keys, so the
// entry is skipped rather than emitted as an empty line (text) or a bare "{}"
// (JSON) — `--fields user` over logs that carry no user field produced one
// content-free record per line.
func (p *Pipeline) render(entry LogEntry) (string, bool) {
	if p.opts.Output == OutputJSON {
		if len(p.fields) > 0 {
			return marshalProjectedJSON(entry, p.fields)
		}
		return MarshalEntryJSON(entry), true
	}

	var out string
	if len(p.fields) > 0 {
		out = formatProjectedEntry(entry, p.fields)
		if out == "" {
			return "", false
		}
	} else {
		out = FormatLogEntry(entry)
	}
	if p.opts.Highlight && len(p.opts.Include) > 0 {
		out = highlightMatches(out, p.opts.Include)
	}
	return out, true
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
//
// It stops early when ctx is cancelled and returns ctx.Err(). Without that check
// the loop was uninterruptible: signal.NotifyContext removes the process's
// default kill-on-SIGINT behavior, so a Ctrl-C during a long `logx parse` was
// caught, cancelled a context nobody observed, and left no way to stop the run.
func (p *Pipeline) Run(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := NewLineReader(r)
	for scanner.Scan() {
		// Checked per line rather than per byte: lines are bounded at 1 MiB, so
		// this bounds the response to a cancellation without measurable cost.
		if err := ctx.Err(); err != nil {
			return err
		}
		out, ok := p.ProcessLine(scanner.Text())
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(w, out); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// Windowed (--tail) pipelines buffer instead of emitting; release the
	// window here. Inactive when tail mode is off (nil).
	for _, out := range p.FlushRetained() {
		if _, err := fmt.Fprintln(w, out); err != nil {
			return err
		}
	}
	return nil
}
