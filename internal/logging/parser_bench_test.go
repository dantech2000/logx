package logging

import (
	"testing"

	"github.com/fatih/color"
)

// benchLines covers the formats a real stream mixes, so a change to the parser
// chain or to field lookup shows up here rather than in production. Field lookup
// in particular has a case-insensitive fallback that must stay off the hot path
// for the common (exact-match) case.
var benchLines = []string{
	`{"level":"info","msg":"request served","status":200,"path":"/api/v1/items","duration_ms":12,"time":"2026-06-24T10:00:00Z"}`,
	`{"Level":"error","Msg":"payment gateway down","Timestamp":"2026-06-24T10:00:01Z","svc":"pay"}`,
	`level=warn msg="disk almost full" component=storage pct=91`,
	`2026-06-24T10:00:02Z INFO scheduler tick`,
	`I0624 10:00:03.123456       1 controller.go:42] reconciling`,
	`127.0.0.1 - - [24/Jun/2026:10:00:04 +0000] "GET /health HTTP/1.1" 200 12`,
	`<log level="ERROR" ts="2026-06-24T10:00:05Z">boom</log>`,
	"\tat com.example.Main.run(Main.java:42)",
	`plain prose line with no structure at all`,
}

func BenchmarkParseLogEntry(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = ParseLogEntry(benchLines[i%len(benchLines)])
	}
}

// BenchmarkParseJSONWideFields exercises the case-insensitive fallback's worst
// case: a wide JSON object with no timestamp field, so all seven jsonTimeFields
// probes miss and each scans the full field set.
//
// Measured cost of the fallback (Apple M4, -benchtime=1s -count=3):
//
//	BenchmarkParseLogEntry        1497 ns/op with fold vs 1531 without  (no regression)
//	BenchmarkParseJSONWideFields  5268 ns/op with fold vs 3796 without  (+39%)
//
// The mixed-format benchmark above — the realistic shape — shows no regression,
// because a line that actually carries a timestamp hits an exact match early.
// The +39% is confined to this adversarial shape and still leaves ~190k lines/s
// on one core. That is the accepted price for the correctness it buys: without
// the fallback a Serilog/.NET line ({"Level":"error"}) kept the DEBUG default and
// was invisible at --level ERROR. hasAnyField deliberately uses fieldValueExact
// so the 16-name HTTP-shape heuristic does not pay this cost.
func BenchmarkParseJSONWideFields(b *testing.B) {
	line := `{"level":"info","msg":"wide","f01":1,"f02":2,"f03":3,"f04":4,"f05":5,` +
		`"f06":6,"f07":7,"f08":8,"f09":9,"f10":10,"f11":11,"f12":12,"f13":13,` +
		`"f14":14,"f15":15,"f16":16,"f17":17,"f18":18,"f19":19,"f20":20}`
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseLogEntry(line)
	}
}

// BenchmarkPipelineProcessLine measures the full parse → group → filter → render
// path, which is what actually runs per line of a live stream.
func BenchmarkPipelineProcessLine(b *testing.B) {
	restoreColorBench(b)
	ApplyColorMode(ColorNever)
	p := NewPipeline(PipelineOptions{MinLevel: DEBUG})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_, _ = p.ProcessLine(benchLines[i%len(benchLines)])
	}
}

// restoreColorBench mirrors restoreColor for benchmarks, which take a *testing.B.
func restoreColorBench(b *testing.B) {
	b.Helper()
	prevNoColor := noColorState()
	prevTheme := activeTheme
	b.Cleanup(func() {
		setNoColorState(prevNoColor)
		activeTheme = prevTheme
	})
}

func noColorState() bool     { return color.NoColor }
func setNoColorState(v bool) { color.NoColor = v }
