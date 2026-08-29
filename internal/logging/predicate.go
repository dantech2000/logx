package logging

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Virtual field-name groups: keys that map to a parsed entry's well-known parts
// rather than to a structured field. Kept as one source of truth so the
// predicate engine and field projection agree on what "msg" or "ts" means.
var (
	messageKeys = []string{"message", "msg", "log"}
	loggerKeys  = []string{"logger", "component", "source"}
	tsKeys      = []string{"timestamp", "ts", "time", "@timestamp"}
	levelKeys   = []string{"level", "lvl", "severity"}
)

func keyIn(key string, set []string) bool {
	return slices.ContainsFunc(set, func(k string) bool { return strings.EqualFold(key, k) })
}

// fieldKind classifies a predicate's key once (at parse time) into one of the
// virtual field groups or a plain structured field, so Eval can dispatch with a
// single comparison per line instead of re-scanning the virtual-key groups.
type fieldKind int

const (
	fieldKindStructured fieldKind = iota
	fieldKindLevel
	fieldKindMessage
	fieldKindLogger
	fieldKindTimestamp
)

func classifyKey(key string) fieldKind {
	switch {
	case keyIn(key, levelKeys):
		return fieldKindLevel
	case keyIn(key, messageKeys):
		return fieldKindMessage
	case keyIn(key, loggerKeys):
		return fieldKindLogger
	case keyIn(key, tsKeys):
		return fieldKindTimestamp
	default:
		return fieldKindStructured
	}
}

type predicateOp int

const (
	opEq predicateOp = iota
	opNeq
	opGt
	opGte
	opLt
	opLte
	opMatch
)

// predicateTokens lists the operators recognized in a --where expression.
// Parsing picks the left-most operator, breaking ties by the longest token so
// ">=" wins over ">" and "==" over "=".
var predicateTokens = []struct {
	token string
	op    predicateOp
}{
	{"~=", opMatch},
	{">=", opGte},
	{"<=", opLte},
	{"!=", opNeq},
	{"==", opEq},
	{">", opGt},
	{"<", opLt},
	{"=", opEq},
}

// FieldPredicate is one parsed --where condition: a field key, an operator, and
// a value. Numeric comparisons require a numeric value; ~= matches the field's
// string form against a regex. The key "level" (and lvl/severity) compares by
// severity order, so `level>=WARN` works.
type FieldPredicate struct {
	key      string
	op       predicateOp
	val      string
	num      float64
	hasNum   bool
	level    LogLevel
	hasLevel bool
	re       *regexp.Regexp
	kind     fieldKind
	ts       time.Time
	hasTS    bool
}

// predicateTimeFormats are the layouts accepted for a timestamp literal in a
// --where expression, in addition to the parser's own timeFormats. A bare date
// is included so `ts>=2026-06-24` works without spelling out a time.
var predicateTimeFormats = []string{
	"2006-01-02",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
}

// parsePredicateTime parses a timestamp literal for a ts comparison.
func parsePredicateTime(val string) (time.Time, bool) {
	if t, err := parseTimestamp(val); err == nil {
		return t, true
	}
	for _, layout := range predicateTimeFormats {
		if t, err := time.Parse(layout, val); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseFieldPredicate parses an expression like "status>=500", "path~=/api", or
// "level==ERROR".
func ParseFieldPredicate(expr string) (FieldPredicate, error) {
	token, op, idx := leftmostOperator(expr)
	if idx <= 0 {
		return FieldPredicate{}, fmt.Errorf("no operator in %q (use ==, !=, >, >=, <, <=, or ~=)", expr)
	}
	key := strings.TrimSpace(expr[:idx])
	val := strings.TrimSpace(expr[idx+len(token):])
	if key == "" {
		return FieldPredicate{}, fmt.Errorf("missing field name in %q", expr)
	}
	// Guard against an expression like ">=5", where the chosen operator ("=") leaves
	// a key made only of operator characters (">"). That is never a real field name;
	// reject it instead of silently filtering on a field named ">".
	if isPureOperatorKey(key) {
		return FieldPredicate{}, fmt.Errorf("missing field name in %q (the field name reads as an operator; write field%svalue, e.g. status>=500)", expr, token)
	}

	fp := FieldPredicate{key: key, op: op, val: val, kind: classifyKey(key)}
	switch op {
	case opMatch:
		re, err := regexp.Compile(val)
		if err != nil {
			return FieldPredicate{}, fmt.Errorf("invalid regex in %q: %w", expr, err)
		}
		fp.re = re
	default:
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			fp.num, fp.hasNum = n, true
		}
		if fp.kind == fieldKindLevel {
			if lvl, err := ParseLogLevel(val); err == nil {
				fp.level, fp.hasLevel = lvl, true
			}
		}
		if fp.kind == fieldKindTimestamp {
			fp.ts, fp.hasTS = parsePredicateTime(val)
		}
	}
	return fp, nil
}

// PredicateOperatorChars are the characters that make up the --where
// comparison/match operators. Exported so shell completion (cmd) can detect
// when the user has moved past the field name without duplicating the set.
const PredicateOperatorChars = "<>=!~"

// isPureOperatorKey reports whether key is non-empty and made up entirely of
// operator characters, e.g. ">" left over from parsing ">=5". Such a key is a
// parsing artifact, not a real field name.
func isPureOperatorKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if !strings.ContainsRune(PredicateOperatorChars, r) {
			return false
		}
	}
	return true
}

// leftmostOperator returns the operator that appears earliest in expr (longest
// token wins on a tie), along with its index, or idx -1 if none is present.
func leftmostOperator(expr string) (string, predicateOp, int) {
	bestIdx := -1
	bestToken := ""
	var bestOp predicateOp
	for _, cand := range predicateTokens {
		idx := strings.Index(expr, cand.token)
		if idx <= 0 {
			continue
		}
		if bestIdx == -1 || idx < bestIdx || (idx == bestIdx && len(cand.token) > len(bestToken)) {
			bestIdx, bestToken, bestOp = idx, cand.token, cand.op
		}
	}
	return bestToken, bestOp, bestIdx
}

// Eval reports whether entry satisfies the predicate.
func (fp FieldPredicate) Eval(entry LogEntry) bool {
	if fp.kind == fieldKindLevel {
		return fp.evalLevel(entry.Level)
	}
	// Timestamp comparisons are chronological. Falling through to the generic
	// path compared an RFC3339 string against a non-numeric literal, so
	// compareNumeric bailed out and `ts>=...` — a documented feature — silently
	// matched nothing. ~= still matches against the formatted string.
	if fp.kind == fieldKindTimestamp && fp.hasTS && fp.op != opMatch {
		if entry.Timestamp.IsZero() {
			return fp.op == opNeq
		}
		return compareOrdered(fp.op, entry.Timestamp.UTC().UnixNano(), fp.ts.UTC().UnixNano())
	}

	raw, ok := fp.resolveValue(entry)
	if !ok {
		// A missing field is "not equal" to any value, and fails every other test.
		return fp.op == opNeq
	}
	str := stringValue(raw)

	switch fp.op {
	case opMatch:
		return fp.re.MatchString(str)
	case opEq:
		return fp.equals(str)
	case opNeq:
		return !fp.equals(str)
	default:
		return fp.compareNumeric(str)
	}
}

// equals compares by exact string first, then numerically so 500 still equals
// 500.0. The string comparison leads because it is both the common case (a JSON
// number stringifies to its plain form, and logfmt values are already strings)
// and the safe one: coercing first made numeric-looking identifiers collide.
func (fp FieldPredicate) equals(str string) bool {
	str = strings.TrimSpace(str)
	if str == fp.val {
		return true
	}
	// A leading zero marks an identifier — a zero-padded account or request ID —
	// rather than a quantity. Coercing it made account==0123 match 123, 123.0,
	// and 1.23e2 alike.
	if !fp.hasNum || hasLeadingZero(str) || hasLeadingZero(fp.val) {
		return false
	}
	// Integers are compared as integers so that a 19-digit trace or span ID is
	// not collapsed by float64's 53-bit mantissa, where ...992 and ...993 are the
	// same value and a predicate would match the wrong span. The int path only
	// applies when both sides are integer literals: returning early here also
	// skipped the float comparison below, so `status==404.0` never matched a
	// field of 404.
	if b, berr := strconv.ParseInt(fp.val, 10, 64); berr == nil {
		if a, aerr := strconv.ParseInt(str, 10, 64); aerr == nil {
			return a == b
		}
	}
	n, err := strconv.ParseFloat(str, 64)
	return err == nil && n == fp.num
}

// hasLeadingZero reports whether s is a zero-padded integer such as "0123",
// which identifies a value as an identifier rather than a number. "0" and "0.5"
// are ordinary numbers and are not treated as padded.
func hasLeadingZero(s string) bool {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	return len(s) > 1 && s[0] == '0' && s[1] != '.'
}

// compareOrdered evaluates a ==, !=, >, >=, <, or <= comparison between two
// ordered values. Shared by compareNumeric (float64) and evalLevel (LogLevel)
// so the operator switch exists in exactly one place instead of being repeated
// per compared type.
func compareOrdered[T cmp.Ordered](op predicateOp, a, b T) bool {
	switch op {
	case opEq:
		return a == b
	case opNeq:
		return a != b
	case opGt:
		return a > b
	case opGte:
		return a >= b
	case opLt:
		return a < b
	case opLte:
		return a <= b
	}
	return false
}

// compareNumeric handles >, >=, <, <=. Both the field and the value must be
// numeric; otherwise the predicate does not hold.
func (fp FieldPredicate) compareNumeric(str string) bool {
	if !fp.hasNum {
		return false
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
	if err != nil {
		return false
	}
	return compareOrdered(fp.op, n, fp.num)
}

// evalLevel compares against the entry's level by severity order, so
// `level>=WARN` and `level==ERROR` both work. The wanted level is parsed once
// at ParseFieldPredicate time; a value that never parsed as a level falls back
// to comparing the level name.
func (fp FieldPredicate) evalLevel(level LogLevel) bool {
	if fp.op == opMatch {
		return fp.re.MatchString(level.String())
	}
	if !fp.hasLevel {
		switch fp.op {
		case opEq:
			return strings.EqualFold(level.String(), fp.val)
		case opNeq:
			return !strings.EqualFold(level.String(), fp.val)
		default:
			return false
		}
	}
	return compareOrdered(fp.op, level, fp.level)
}

// resolveByKind returns the raw value for a key already classified into a
// fieldKind, handling the virtual keys (level/message/logger/timestamp) before
// falling back to a structured field (dot-path aware). A level kind resolves to
// the parsed level's name; the predicate engine never asks for it (Eval routes
// level keys to evalLevel for severity-ordered comparison first).
func resolveByKind(entry LogEntry, kind fieldKind, key string) (any, bool) {
	switch kind {
	case fieldKindLevel:
		return entry.Level.String(), true
	case fieldKindMessage:
		if entry.Message != "" {
			return entry.Message, true
		}
		return entry.RawLine, entry.RawLine != ""
	case fieldKindLogger:
		// A field literally named logger/component/source in the line wins over
		// entry.Logger, which is only a guessed display label and is often derived
		// from a different key entirely. Without this, a log carrying its own
		// "source" field could not be filtered on at all: `source==kafka` compared
		// against the guessed label and never matched, while `source==logrus`
		// matched every line.
		if entry.Fields != nil {
			if v, ok := fieldValue(entry.Fields, key); ok {
				return v, true
			}
		}
		return entry.Logger, entry.Logger != ""
	case fieldKindTimestamp:
		if entry.Timestamp.IsZero() {
			return nil, false
		}
		return entry.Timestamp.UTC().Format(time.RFC3339), true
	default:
		if entry.Fields != nil {
			if v, ok := fieldValue(entry.Fields, key); ok {
				return v, true
			}
		}
		return nil, false
	}
}

// resolveValue resolves the predicate's key against an entry, dispatching on
// the key's precomputed kind instead of re-scanning the virtual-key groups on
// every call, which matters here since Eval runs once per predicate per log line.
func (fp FieldPredicate) resolveValue(entry LogEntry) (any, bool) {
	return resolveByKind(entry, fp.kind, fp.key)
}
