package logging

import (
	"fmt"
	"regexp"
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
	for _, k := range set {
		if strings.EqualFold(key, k) {
			return true
		}
	}
	return false
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
	key    string
	op     predicateOp
	val    string
	num    float64
	hasNum bool
	re     *regexp.Regexp
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

	fp := FieldPredicate{key: key, op: op, val: val}
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
	}
	return fp, nil
}

// operatorRunes are the characters that make up the comparison/match operators.
// A field key composed solely of these is a parsing artifact, not a real name.
const operatorRunes = "<>=!~"

// isPureOperatorKey reports whether key is non-empty and made up entirely of
// operator characters, e.g. ">" left over from parsing ">=5".
func isPureOperatorKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if !strings.ContainsRune(operatorRunes, r) {
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
	if keyIn(fp.key, levelKeys) {
		return fp.evalLevel(entry.Level)
	}

	raw, ok := resolveField(entry, fp.key)
	if !ok {
		// A missing field is "not equal" to any value, and fails every other test.
		return fp.op == opNeq
	}
	str := fmt.Sprintf("%v", raw)

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

// equals compares numerically when both sides are numeric (so 500 == 500.0),
// otherwise by exact string.
func (fp FieldPredicate) equals(str string) bool {
	if fp.hasNum {
		if n, err := strconv.ParseFloat(strings.TrimSpace(str), 64); err == nil {
			return n == fp.num
		}
	}
	return str == fp.val
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
	switch fp.op {
	case opGt:
		return n > fp.num
	case opGte:
		return n >= fp.num
	case opLt:
		return n < fp.num
	case opLte:
		return n <= fp.num
	}
	return false
}

// evalLevel compares against the entry's level by severity order, so
// `level>=WARN` and `level==ERROR` both work. A numeric or unknown value falls
// back to comparing the level name.
func (fp FieldPredicate) evalLevel(level LogLevel) bool {
	if fp.op == opMatch {
		return fp.re.MatchString(level.String())
	}
	want, err := ParseLogLevel(fp.val)
	if err != nil {
		switch fp.op {
		case opEq:
			return strings.EqualFold(level.String(), fp.val)
		case opNeq:
			return !strings.EqualFold(level.String(), fp.val)
		default:
			return false
		}
	}
	switch fp.op {
	case opEq:
		return level == want
	case opNeq:
		return level != want
	case opGt:
		return level > want
	case opGte:
		return level >= want
	case opLt:
		return level < want
	case opLte:
		return level <= want
	}
	return false
}

// resolveField returns the raw value for a key, handling the virtual keys
// (message/logger/timestamp) before falling back to a structured field
// (dot-path aware).
func resolveField(entry LogEntry, key string) (interface{}, bool) {
	switch {
	case keyIn(key, messageKeys):
		if entry.Message != "" {
			return entry.Message, true
		}
		return entry.RawLine, entry.RawLine != ""
	case keyIn(key, loggerKeys):
		return entry.Logger, entry.Logger != ""
	case keyIn(key, tsKeys):
		if entry.Timestamp.IsZero() {
			return nil, false
		}
		return entry.Timestamp.UTC().Format(time.RFC3339), true
	}
	if entry.Fields != nil {
		if v, ok := fieldValue(entry.Fields, key); ok {
			return v, true
		}
	}
	return nil, false
}
