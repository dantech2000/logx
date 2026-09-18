package logging

// GroupFilter applies level and content filtering with multi-line entries kept
// as units. An anchor line is judged on its own content; a continuation line
// rides with its anchor — a frame is shown when its entry is shown, and hidden
// when its entry is hidden. Without this a --grep for an error showed the
// parent plus only the frames that literally contained the pattern, i.e. trace
// fragments.
//
// Field predicates (--where) evaluate against the anchor's entry: frames carry
// no fields of their own, so judging them on their own content would shred
// every entry a predicate keeps. Regex filters still match each line's own
// text, so a footer-only --grep still rescues that line.
//
// The two grep/exclude modes use mirror-image join rules so they still
// partition the input exactly (a pinned metamorphic invariant): with --grep
// patterns present a continuation joins an already-kept group, while without
// them a continuation leaves with an already-dropped group.
//
// A GroupFilter is stateful — it remembers the current anchor — so it is NOT
// safe for concurrent use. The zero value keeps everything (TRACE floor, no
// content filters); prefer NewGroupFilter.
type GroupFilter struct {
	minLevel LogLevel
	opts     PipelineOptions
	// anchorEntry is the current group's anchor, against which continuations
	// evaluate their field predicates. Every group opens with an anchor
	// (Classify never reports a continuation without a preceding anchor), so
	// it is always set when a continuation is judged.
	anchorEntry LogEntry
	// anchorKept is the keep verdict of the current group's anchor line.
	anchorKept bool
}

// NewGroupFilter returns a filter with the given level floor and content
// options.
func NewGroupFilter(minLevel LogLevel, opts PipelineOptions) GroupFilter {
	return GroupFilter{minLevel: minLevel, opts: opts}
}

// Keep reports whether the entry with the given raw line and group role
// passes the level floor and content filters.
func (f *GroupFilter) Keep(entry LogEntry, rawLine string, role GroupRole) bool {
	if entry.Level < f.minLevel {
		if role == GroupAnchor {
			f.anchorKept = false
		}
		return false
	}
	if role == GroupAnchor {
		f.anchorEntry = entry
		f.anchorKept = f.opts.MatchesContent(entry, rawLine)
		return f.anchorKept
	}
	// Continuations match regexes against their own line but inherit field
	// predicates from the anchor, which alone carries the entry's fields.
	ownVerdict := f.opts.MatchesContent(f.anchorEntry, rawLine)
	if len(f.opts.Include) > 0 {
		return ownVerdict || f.anchorKept
	}
	return ownVerdict && f.anchorKept
}
