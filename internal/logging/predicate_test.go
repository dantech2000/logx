package logging

import "testing"

func TestParseFieldPredicate(t *testing.T) {
	tests := []struct {
		expr    string
		wantKey string
		wantOp  predicateOp
		wantVal string
		wantErr bool
	}{
		{"status>=500", "status", opGte, "500", false},
		{"status<=200", "status", opLte, "200", false},
		{"status>400", "status", opGt, "400", false},
		{"status<300", "status", opLt, "300", false},
		{"level==ERROR", "level", opEq, "ERROR", false},
		{"level=ERROR", "level", opEq, "ERROR", false},
		{"user!=root", "user", opNeq, "root", false},
		{"path~=/api", "path", opMatch, "/api", false},
		{"  status >= 500 ", "status", opGte, "500", false},
		{"noop", "", opEq, "", true},
		{"=novalue", "", opEq, "", true},
		{"bad~=(", "", opEq, "", true}, // invalid regex
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			fp, err := ParseFieldPredicate(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseFieldPredicate(%q) = nil error, want error", tt.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFieldPredicate(%q) error = %v", tt.expr, err)
			}
			if fp.key != tt.wantKey || fp.op != tt.wantOp || fp.val != tt.wantVal {
				t.Fatalf("ParseFieldPredicate(%q) = {key:%q op:%d val:%q}, want {key:%q op:%d val:%q}",
					tt.expr, fp.key, fp.op, fp.val, tt.wantKey, tt.wantOp, tt.wantVal)
			}
		})
	}
}

func mustPredicate(t *testing.T, expr string) FieldPredicate {
	t.Helper()
	fp, err := ParseFieldPredicate(expr)
	if err != nil {
		t.Fatalf("ParseFieldPredicate(%q) error: %v", expr, err)
	}
	return fp
}

func TestFieldPredicateEval(t *testing.T) {
	tests := []struct {
		name string
		expr string
		line string
		want bool
	}{
		{"numeric gte true", "status>=500", `{"status":503,"msg":"x"}`, true},
		{"numeric gte false", "status>=500", `{"status":200,"msg":"x"}`, false},
		{"numeric eq matches int", "status==200", `{"status":200}`, true},
		{"string field match", "path~=/api", `{"path":"/api/v1/users"}`, true},
		{"string field no match", "path~=/api", `{"path":"/health"}`, false},
		{"level severity gte", "level>=WARN", `{"level":"error","msg":"boom"}`, true},
		{"level severity gte false", "level>=WARN", `{"level":"info","msg":"ok"}`, false},
		{"level eq", "level==INFO", `{"level":"info"}`, true},
		{"message contains", "msg~=timeout", `{"msg":"upstream timeout"}`, true},
		{"missing field neq is true", "region!=us", `{"msg":"x"}`, true},
		{"missing field eq is false", "region==us", `{"msg":"x"}`, false},
		{"missing field gte is false", "status>=1", `{"msg":"x"}`, false},
		{"non-numeric field gt is false", "msg>5", `{"msg":"hello"}`, false},
		{"nested dot path", "http.status>=500", `{"http":{"status":502}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ParseLogEntry(tt.line)
			if got := mustPredicate(t, tt.expr).Eval(entry); got != tt.want {
				t.Fatalf("Eval(%q) on %s = %v, want %v", tt.expr, tt.line, got, tt.want)
			}
		})
	}
}

func TestFieldPredicateLeftmostLongestOperator(t *testing.T) {
	// The value side contains characters that look like operators; parsing must
	// split on the first real operator only.
	fp := mustPredicate(t, "path~=/v2>=1")
	if fp.key != "path" || fp.op != opMatch || fp.val != "/v2>=1" {
		t.Fatalf("got {key:%q op:%d val:%q}, want path ~= /v2>=1", fp.key, fp.op, fp.val)
	}
}
