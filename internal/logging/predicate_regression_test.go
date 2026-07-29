package logging

import (
	"testing"
)

func evalWhere(t *testing.T, expr, line string) bool {
	t.Helper()
	pred, err := ParseFieldPredicate(expr)
	if err != nil {
		t.Fatalf("ParseFieldPredicate(%q): %v", expr, err)
	}
	return pred.Eval(ParseLogEntry(line))
}

// TestWhereTimestampComparison pins chronological ts comparisons. The README
// documents ts as a virtual key supporting >, >=, <, and <=, but the value
// resolved to an RFC3339 string and the ordered operators ran through
// compareNumeric, which requires a numeric literal. A date literal is not
// numeric, so every such predicate silently matched nothing — with no error to
// suggest the filter had not been applied.
func TestWhereTimestampComparison(t *testing.T) {
	const early = `{"level":"info","msg":"early","time":"2026-06-24T09:00:00Z"}`
	const late = `{"level":"info","msg":"late","time":"2026-06-24T11:00:00Z"}`

	tests := []struct {
		expr string
		line string
		want bool
	}{
		{"ts>=2026-06-24T10:00:00Z", late, true},
		{"ts>=2026-06-24T10:00:00Z", early, false},
		{"ts<2026-06-24T10:00:00Z", early, true},
		{"ts<2026-06-24T10:00:00Z", late, false},
		{"ts>2026-06-24T09:00:00Z", late, true},
		{"ts<=2026-06-24T09:00:00Z", early, true},
		// A bare date is accepted so the common case needs no time component.
		{"ts>=2026-06-24", late, true},
		{"ts<2026-06-24", late, false},
		// Equality and regex matching kept working before and must keep working.
		{"ts==2026-06-24T11:00:00Z", late, true},
		{"ts~=2026-06-24", late, true},
	}

	for _, tc := range tests {
		t.Run(tc.expr+"/"+tc.line[:0]+tc.expr, func(t *testing.T) {
			if got := evalWhere(t, tc.expr, tc.line); got != tc.want {
				t.Errorf("%q against %s = %v, want %v", tc.expr, tc.line, got, tc.want)
			}
		})
	}
}

// TestWhereEqualsDoesNotCoerceIdentifiers pins that numeric-looking identifiers
// compare exactly. equals coerced both sides to float64 whenever the literal
// parsed as a number, so a zero-padded account matched its unpadded form and a
// 19-digit span ID matched its neighbour — float64's 53-bit mantissa cannot
// distinguish 9007199254740992 from 9007199254740993.
func TestWhereEqualsDoesNotCoerceIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		expr string
		line string
		want bool
	}{
		{"padded id matches itself", "account==0123", `{"account":"0123"}`, true},
		{"padded id does not match unpadded", "account==0123", `{"account":"123"}`, false},
		{"padded id does not match decimal", "account==0123", `{"account":"123.0"}`, false},
		{"padded id does not match exponent", "account==0123", `{"account":"1.23e2"}`, false},
		{"span id matches itself", "span_id==9007199254740993", `{"span_id":"9007199254740993"}`, true},
		{"span id does not match neighbour", "span_id==9007199254740992", `{"span_id":"9007199254740993"}`, false},

		// Numeric equality must still work for genuine quantities.
		{"json number", "status==500", `{"status":500}`, true},
		{"json number mismatch", "status==500", `{"status":404}`, false},
		{"logfmt string number", "status==500", `level=error status=500`, true},
		{"float equals integer literal", "status==500", `{"status":500.0}`, true},
		{"zero is a number not padding", "retries==0", `{"retries":0}`, true},
		{"decimal fraction", "ratio==0.5", `{"ratio":0.5}`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalWhere(t, tc.expr, tc.line); got != tc.want {
				t.Errorf("%q against %s = %v, want %v", tc.expr, tc.line, got, tc.want)
			}
		})
	}
}

// TestWhereRealFieldBeatsGuessedLogger pins that a field literally present in
// the line is reachable even when its name collides with a virtual key. `source`
// is one of the logger aliases, so it resolved to entry.Logger — a label guessed
// by detectLoggerLabel from a different key. The result was backwards: filtering
// on the value actually in the log matched nothing, while filtering on the
// guessed label matched everything.
func TestWhereRealFieldBeatsGuessedLogger(t *testing.T) {
	const line = `{"level":"info","msg":"consumed batch","source":"kafka","time":"2026-06-24T10:00:00Z"}`

	if got := evalWhere(t, "source==kafka", line); !got {
		t.Error("source==kafka did not match a line whose source field is exactly kafka")
	}
	if got := evalWhere(t, "source~=kaf", line); !got {
		t.Error("source~=kaf did not match a line whose source field is kafka")
	}
	if got := evalWhere(t, "source==logrus", line); got {
		t.Error("source==logrus matched; the guessed logger label must not shadow the real field")
	}

	// With no competing field, the virtual key still resolves to the logger.
	const noSource = `{"level":"info","msg":"hi"}`
	if got := evalWhere(t, "logger==logrus", noSource); !got {
		t.Error("logger virtual key stopped resolving to the guessed label when no field shadows it")
	}
}
