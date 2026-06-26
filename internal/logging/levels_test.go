package logging

import "testing"

func TestLogLevelOrdering(t *testing.T) {
	// The spectrum must be strictly increasing in severity; filters rely on it.
	ordered := []LogLevel{TRACE, DEBUG, INFO, WARN, ERROR, FATAL}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1] >= ordered[i] {
			t.Fatalf("level %s is not less than %s", ordered[i-1], ordered[i])
		}
	}
	// TRACE sits below the default DEBUG filter so trace lines are opt-in.
	if TRACE >= DEBUG {
		t.Fatalf("TRACE (%d) must be below DEBUG (%d)", TRACE, DEBUG)
	}
}

func TestLogLevelString(t *testing.T) {
	cases := map[LogLevel]string{
		TRACE:          "TRACE",
		DEBUG:          "DEBUG",
		INFO:           "INFO",
		WARN:           "WARN",
		ERROR:          "ERROR",
		FATAL:          "FATAL",
		LogLevel(-1):   "UNKNOWN",
		LogLevel(1000): "UNKNOWN",
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("LogLevel(%d).String() = %q, want %q", int(level), got, want)
		}
	}
}
