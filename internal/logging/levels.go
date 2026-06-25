package logging

// LogLevel represents the severity of a log entry
type LogLevel int

// Log severity levels in increasing order of importance. TRACE sits below DEBUG
// and FATAL above ERROR so the set spans the conventions used by the loggers logx
// parses (zap, bunyan/pino, klog, syslog). TRACE is intentionally below the
// default DEBUG filter, so trace-level lines are opt-in via `--level TRACE`.
const (
	TRACE LogLevel = iota
	DEBUG
	INFO
	WARN
	ERROR
	FATAL
)

// String returns the string representation of a LogLevel
func (l LogLevel) String() string {
	switch l {
	case TRACE:
		return "TRACE"
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}
