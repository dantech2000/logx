package logging

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/dantech2000/logx/internal/terminal"
	"github.com/fatih/color"
)

// Metamorphic tests. Rather than asserting a specific rendering for a specific
// input — which only finds bugs someone already thought to enumerate — these
// assert relationships that must hold between the outputs of *different* runs
// over the *same* input. A violation is a real inconsistency in the engine no
// matter what the "correct" rendering of any single line is.

// corpus mixes every format the parser claims to handle with the degenerate and
// hostile shapes that have historically broken invariants.
var metamorphicCorpus = []string{
	`{"level":"error","msg":"db down","status":500,"time":"2026-06-24T10:00:00Z"}`,
	`{"Level":"warn","Msg":"capitalized keys","Timestamp":"2026-06-24T10:00:01Z"}`,
	`{"level":"info","msg":"","message":"empty alias first"}`,
	`{"level":"debug","msg":null,"log":"null alias"}`,
	`level=warn msg="disk almost full" component=storage pct=91`,
	`{level: error, msg: "yaml flow", ratio: .nan}`,
	`{'level': 'info', 'msg': 'nested', 'ctx': {'user': 'bob'}}`,
	`<log level="ERROR" ts="2026-06-24T10:00:02Z">xml boom</log>`,
	`<entry thread="main">xml without a level</entry>`,
	`I0624 10:00:03.123456       1 controller.go:42] klog reconciling`,
	`127.0.0.1 - - [24/Jun/2026:10:00:04 +0000] "GET /health HTTP/1.1" 200 12`,
	`<134>Jun 24 10:00:05 host app: syslog priority line`,
	`2026-06-24T10:00:06Z,ERROR,payment-svc,csv row`,
	`2026-06-24 10:00:07 INFO plain text with timestamp`,
	`INFO 2026-06-24 10:00:08 level before timestamp`,
	`Uncaught exception, stack trace follows:`,
	"\tat com.example.Main.run(Main.java:42)",
	`java.lang.NullPointerException: STACK TRACE follows`,
	`Segmentation fault in worker 3`,
	`an error occurred while connecting`,
	`TRACE-ID: 9f2b failed with ERROR: declined`,
	`TRACE entering handler`,
	"",
	"   ",
	"\x00\x01 binary prefix",
	"plain line with an \x1b[31m embedded escape",
	"invalid utf8: \xff\xfe end",
	`{"level":"error","msg":`,
	`{: : :}`,
	",,,,,,,,",
	"🔥 emoji and 日本語",
	"del\x7f c1\u009b31m rlo\u202e hostile",
}

// runCorpus pushes the whole corpus through one pipeline configuration and
// returns, for each input index, whether it was kept and what was rendered.
func runCorpus(t *testing.T, opts PipelineOptions) (kept []bool, out []string) {
	t.Helper()
	p := NewPipeline(opts)
	kept = make([]bool, len(metamorphicCorpus))
	out = make([]string, len(metamorphicCorpus))
	for i, line := range metamorphicCorpus {
		s, ok := p.ProcessLine(line)
		kept[i], out[i] = ok, s
	}
	return kept, out
}

// TestMetamorphicLevelMonotonicity asserts that lowering the level floor can
// only ever add lines. If an entry survives --level ERROR it must also survive
// --level DEBUG and --level TRACE. A violation means the level filter is not a
// monotonic threshold — which is what a stateful LevelTracker could break, since
// the tracker's parent state depends on which lines it has already seen.
func TestMetamorphicLevelMonotonicity(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	levels := []LogLevel{TRACE, DEBUG, INFO, WARN, ERROR, FATAL}
	keptAt := make(map[LogLevel][]bool, len(levels))
	for _, lv := range levels {
		keptAt[lv], _ = runCorpus(t, PipelineOptions{MinLevel: lv})
	}

	for i := range metamorphicCorpus {
		for a := 0; a < len(levels)-1; a++ {
			lower, higher := levels[a], levels[a+1]
			if keptAt[higher][i] && !keptAt[lower][i] {
				t.Errorf("line %d %q: kept at --level %v but dropped at the lower --level %v",
					i, metamorphicCorpus[i], higher, lower)
			}
		}
	}
}

// TestMetamorphicGrepExcludePartition asserts that --grep P and --exclude P
// partition the input exactly: every line kept with no content filter is kept by
// exactly one of them. Both match the same raw line with the same regex, so any
// line falling into both or neither means the two filters disagree about what
// "matches" means.
func TestMetamorphicGrepExcludePartition(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	for _, pattern := range []string{`error`, `(?i)error`, `\d+`, `.`, `^`, `msg`, `z{0}`} {
		t.Run(pattern, func(t *testing.T) {
			re := regexp.MustCompile(pattern)
			base, _ := runCorpus(t, PipelineOptions{MinLevel: TRACE})
			inc, _ := runCorpus(t, PipelineOptions{MinLevel: TRACE, Include: []*regexp.Regexp{re}})
			exc, _ := runCorpus(t, PipelineOptions{MinLevel: TRACE, Exclude: []*regexp.Regexp{re}})

			for i := range metamorphicCorpus {
				if !base[i] {
					// A line the engine drops outright (blank) must stay dropped.
					if inc[i] || exc[i] {
						t.Errorf("line %d %q: dropped unfiltered but kept by a content filter", i, metamorphicCorpus[i])
					}
					continue
				}
				if inc[i] == exc[i] {
					t.Errorf("line %d %q: --grep %q kept=%v and --exclude %q kept=%v; they must partition",
						i, metamorphicCorpus[i], pattern, inc[i], pattern, exc[i])
				}
			}
		})
	}
}

// TestMetamorphicTextAndJSONAgree asserts the two renderers make identical
// filtering decisions and report the same level for every entry. They share the
// parse and filter stages and differ only in rendering, so a divergence means
// one of them is second-guessing the engine — exactly the class of bug where the
// formatter re-derived the message and disagreed with the parser.
func TestMetamorphicTextAndJSONAgree(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	for _, minLevel := range []LogLevel{TRACE, DEBUG, WARN} {
		textKept, _ := runCorpus(t, PipelineOptions{MinLevel: minLevel})
		jsonKept, jsonOut := runCorpus(t, PipelineOptions{MinLevel: minLevel, Output: OutputJSON})

		for i := range metamorphicCorpus {
			if textKept[i] != jsonKept[i] {
				t.Errorf("level %v line %d %q: kept in text=%v but json=%v",
					minLevel, i, metamorphicCorpus[i], textKept[i], jsonKept[i])
				continue
			}
			if !jsonKept[i] {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(jsonOut[i]), &obj); err != nil {
				t.Errorf("line %d: invalid JSON %q: %v", i, jsonOut[i], err)
				continue
			}
			// The level the JSON reports must equal the level the engine filtered on.
			lvl, _ := obj["level"].(string)
			parsed, err := ParseLogLevel(lvl)
			if err != nil {
				t.Errorf("line %d: JSON level %q does not parse: %v", i, lvl, err)
				continue
			}
			if parsed < minLevel {
				t.Errorf("line %d %q: emitted at --level %v but reports level %v",
					i, metamorphicCorpus[i], minLevel, parsed)
			}
		}
	}
}

// TestMetamorphicStatsCountsMatchEmittedLines asserts the digest's line count
// equals the number of lines the same configuration would emit. --stats reuses
// the filter path but suppresses rendering, so a mismatch means the two modes
// disagree about which entries are kept.
func TestMetamorphicStatsCountsMatchEmittedLines(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	configs := []struct {
		name string
		opts PipelineOptions
	}{
		{"level TRACE", PipelineOptions{MinLevel: TRACE}},
		{"level WARN", PipelineOptions{MinLevel: WARN}},
		{"grep error", PipelineOptions{MinLevel: TRACE, Include: []*regexp.Regexp{regexp.MustCompile(`(?i)error`)}}},
		{"exclude msg", PipelineOptions{MinLevel: TRACE, Exclude: []*regexp.Regexp{regexp.MustCompile(`msg`)}}},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			kept, _ := runCorpus(t, cfg.opts)
			emitted := 0
			for _, k := range kept {
				if k {
					emitted++
				}
			}

			statsOpts := cfg.opts
			statsOpts.CollectStats = true
			p := NewPipeline(statsOpts)
			for _, line := range metamorphicCorpus {
				p.ProcessLine(line)
			}
			if got := p.Stats().Total(); got != emitted {
				t.Errorf("--stats counted %d entries but the same filters emit %d lines", got, emitted)
			}
		})
	}
}

// TestMetamorphicProjectionIsSubset asserts that --fields never *adds* entries:
// projection changes rendering, not filtering, so the set of kept lines must be
// a subset of the unprojected run (it may be smaller, since an entry resolving
// none of the requested keys is skipped rather than emitted blank).
func TestMetamorphicProjectionIsSubset(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	for _, fields := range [][]string{{"level"}, {"ts", "level", "msg"}, {"status"}, {"nonexistent"}} {
		t.Run(strings.Join(fields, ","), func(t *testing.T) {
			base, _ := runCorpus(t, PipelineOptions{MinLevel: TRACE})
			for _, output := range []OutputFormat{OutputText, OutputJSON} {
				proj, _ := runCorpus(t, PipelineOptions{MinLevel: TRACE, Fields: fields, Output: output})
				for i := range metamorphicCorpus {
					if proj[i] && !base[i] {
						t.Errorf("line %d %q: projection emitted an entry the unprojected run dropped",
							i, metamorphicCorpus[i])
					}
				}
			}
		})
	}
}

// TestMetamorphicPipelineIsDeterministic asserts that two identical runs produce
// identical output. Field resolution walks Go maps, whose iteration order is
// randomized, so any place that picks "a" matching key rather than a
// well-defined one shows up here as flapping between runs.
func TestMetamorphicPipelineIsDeterministic(t *testing.T) {
	restoreColor(t)
	ApplyColorMode(ColorNever)

	opts := []PipelineOptions{
		{MinLevel: TRACE},
		{MinLevel: TRACE, Output: OutputJSON},
		{MinLevel: TRACE, Fields: []string{"level", "msg", "status"}},
	}
	for _, o := range opts {
		_, first := runCorpus(t, o)
		for range 25 {
			_, again := runCorpus(t, o)
			for i := range first {
				if first[i] != again[i] {
					t.Fatalf("nondeterministic output for line %d %q:\n  %q\n  %q",
						i, metamorphicCorpus[i], first[i], again[i])
				}
			}
		}
	}
}

// TestMetamorphicSanitizeIsIdempotent asserts Sanitize is a fixed point. Values
// can pass through it more than once (a field rendered inside an already
// sanitized message, say), so a non-idempotent escape would double-escape and
// corrupt the text. It also asserts the output is unconditionally safe.
func TestMetamorphicSanitizeIsIdempotent(t *testing.T) {
	for _, s := range metamorphicCorpus {
		once := terminal.Sanitize(s)
		twice := terminal.Sanitize(once)
		if once != twice {
			t.Errorf("Sanitize is not idempotent for %q:\n  once:  %q\n  twice: %q", s, once, twice)
		}
		assertNoControlBytes(t, once)
	}
}

// FuzzMetamorphicLevelMonotonicity fuzzes the monotonicity property. Example
// tests can only check inputs someone thought of; this asserts the relationship
// holds for arbitrary input, which is where a stateful filter is most likely to
// break it — LevelTracker carries a parent level across lines, so the decision
// for line N depends on lines 1..N-1.
func FuzzMetamorphicLevelMonotonicity(f *testing.F) {
	for _, l := range metamorphicCorpus {
		f.Add(l)
	}
	f.Add("ERROR boom\n\tindented frame\nplain\nTRACE x")

	f.Fuzz(func(t *testing.T, data string) {
		prev := color.NoColor
		defer func() { color.NoColor = prev }()
		ApplyColorMode(ColorNever)

		lines := strings.Split(data, "\n")
		keptAt := func(min LogLevel) []bool {
			p := NewPipeline(PipelineOptions{MinLevel: min})
			out := make([]bool, len(lines))
			for i, line := range lines {
				_, out[i] = p.ProcessLine(line)
			}
			return out
		}

		order := []LogLevel{TRACE, DEBUG, INFO, WARN, ERROR, FATAL}
		for a := 0; a < len(order)-1; a++ {
			lower, higher := keptAt(order[a]), keptAt(order[a+1])
			for i := range lines {
				if higher[i] && !lower[i] {
					t.Fatalf("line %d %q kept at --level %v but dropped at lower --level %v",
						i, lines[i], order[a+1], order[a])
				}
			}
		}
	})
}

// FuzzMetamorphicTextJSONAgree fuzzes the cross-renderer agreement property:
// text and JSON share parse and filter and differ only in rendering, so they
// must keep exactly the same lines and report the same level.
func FuzzMetamorphicTextJSONAgree(f *testing.F) {
	for _, l := range metamorphicCorpus {
		f.Add(l)
	}

	f.Fuzz(func(t *testing.T, data string) {
		prev := color.NoColor
		defer func() { color.NoColor = prev }()
		ApplyColorMode(ColorNever)

		lines := strings.Split(data, "\n")
		textPipe := NewPipeline(PipelineOptions{MinLevel: TRACE})
		jsonPipe := NewPipeline(PipelineOptions{MinLevel: TRACE, Output: OutputJSON})

		for i, line := range lines {
			_, textOK := textPipe.ProcessLine(line)
			jsonOut, jsonOK := jsonPipe.ProcessLine(line)
			if textOK != jsonOK {
				t.Fatalf("line %d %q: text kept=%v, json kept=%v", i, line, textOK, jsonOK)
			}
			if !jsonOK {
				continue
			}
			if !json.Valid([]byte(jsonOut)) {
				t.Fatalf("line %d %q: invalid JSON %q", i, line, jsonOut)
			}
			assertNoControlBytes(t, jsonOut)
		}
	})
}

// FuzzMetamorphicGrepExcludePartition fuzzes the partition property over both
// arbitrary input and arbitrary user patterns.
func FuzzMetamorphicGrepExcludePartition(f *testing.F) {
	for _, l := range metamorphicCorpus {
		f.Add(l, "error")
		f.Add(l, `\d+`)
	}

	f.Fuzz(func(t *testing.T, data, pattern string) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Skip() // only valid user patterns reach the pipeline
		}
		prev := color.NoColor
		defer func() { color.NoColor = prev }()
		ApplyColorMode(ColorNever)

		lines := strings.Split(data, "\n")
		run := func(opts PipelineOptions) []bool {
			p := NewPipeline(opts)
			out := make([]bool, len(lines))
			for i, line := range lines {
				_, out[i] = p.ProcessLine(line)
			}
			return out
		}
		base := run(PipelineOptions{MinLevel: TRACE})
		inc := run(PipelineOptions{MinLevel: TRACE, Include: []*regexp.Regexp{re}})
		exc := run(PipelineOptions{MinLevel: TRACE, Exclude: []*regexp.Regexp{re}})

		for i := range lines {
			if !base[i] {
				if inc[i] || exc[i] {
					t.Fatalf("line %d %q: dropped unfiltered but kept by a content filter", i, lines[i])
				}
				continue
			}
			if inc[i] == exc[i] {
				t.Fatalf("line %d %q with pattern %q: grep kept=%v exclude kept=%v; must partition",
					i, lines[i], pattern, inc[i], exc[i])
			}
		}
	})
}
