package logging

import (
	"context"
	"io"
)

// FilterAndFormatLogs reads log lines from reader, parses each one, and writes
// the formatted result to writer for entries at or above filterLevel. It is a
// thin convenience wrapper over Pipeline (the shared parse → group → filter →
// render engine); see Pipeline for the multi-line grouping and kubelet-timestamp
// handling it provides.
//
// It runs to completion; callers that need to abort partway should drive a
// Pipeline directly with a cancellable context.
func FilterAndFormatLogs(reader io.Reader, writer io.Writer, filterLevel LogLevel) error {
	return NewPipeline(PipelineOptions{MinLevel: filterLevel}).Run(context.Background(), reader, writer)
}
