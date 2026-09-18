package logging

// TailBuffer keeps the last maxAnchors anchor-led groups of items so --tail
// applies after filtering and grouping instead of before them. A server-side
// tail slices raw lines: with a level or content filter set the window usually
// holds no matches at all, and a sliced multi-line entry leaves orphaned
// frames behind. Buffering the output and counting anchors (not lines) shows
// the last N complete entries: single-line logs behave exactly like a line
// tail, while a stack trace is never decapitated.
//
// Continuations that arrive before any anchor (a group sliced by --since, or a
// stream whose history starts mid-trace) form a leading partial group that is
// always kept and never counts toward the budget — there is nothing better to
// show for them.
//
// A TailBuffer is not safe for concurrent use; multi-stream fan-out gives each
// stream its own. Use NewTailBuffer; a nil *TailBuffer must not be used.
type TailBuffer[T any] struct {
	maxAnchors int
	head       []T
	groups     [][]T
	open       []T
	hasAnchor  bool
}

// NewTailBuffer returns a buffer retaining the last maxAnchors entries.
// A maxAnchors of zero retains nothing.
func NewTailBuffer[T any](maxAnchors int) *TailBuffer[T] {
	return &TailBuffer[T]{maxAnchors: maxAnchors}
}

// Add records an item with its group role. Anchors seal the previous group
// (evicting the oldest retained one past the budget) and open a new one;
// continuations extend the open group, or the leading partial group when no
// anchor has been seen yet.
func (b *TailBuffer[T]) Add(item T, role GroupRole) {
	if b.maxAnchors <= 0 {
		return
	}
	if role == GroupAnchor {
		if b.open != nil || b.hasAnchor {
			b.groups = append(b.groups, b.open)
			b.hasAnchor = true
			// The group being opened counts toward the budget too: retained
			// sealed groups plus the open one must not exceed maxAnchors.
			for len(b.groups)+1 > b.maxAnchors {
				b.groups = b.groups[1:]
			}
		}
		b.open = []T{item}
		return
	}
	if !b.hasAnchor && b.open == nil {
		b.head = append(b.head, item)
		return
	}
	b.open = append(b.open, item)
}

// Lines returns the retained items in stream order: the leading partial group
// (if any) followed by the retained anchor-led groups.
func (b *TailBuffer[T]) Lines() []T {
	if b.maxAnchors <= 0 {
		return nil
	}
	var out []T
	out = append(out, b.head...)
	for _, group := range b.groups {
		out = append(out, group...)
	}
	out = append(out, b.open...)
	return out
}

// Clear empties the buffer, keeping its capacity. Flush-style callers drain
// once; without this a second flush would re-emit (or, for a stats digest,
// double-count) the same window.
func (b *TailBuffer[T]) Clear() {
	b.head = nil
	b.groups = nil
	b.open = nil
	b.hasAnchor = false
}
