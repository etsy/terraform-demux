package wrapper

import (
	"bytes"
	"io"
	"log"
)

// SetupLogging configures the destination of the standard library log
// package for the lifetime of the process. When verbose is true, log
// output streams to errSink immediately. When verbose is false, output
// is held in an in-memory buffer and only flushed to errSink when the
// returned func is called — typically on the error path, so a successful
// run stays silent but a failure shows the full trace that led to it.
//
// SetupLogging mutates log's package-level state, so callers (and tests
// that use it) must serialize their use.
func SetupLogging(verbose bool, errSink io.Writer) (flush func()) {
	if verbose {
		log.SetOutput(errSink)
		return func() {}
	}

	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	return func() {
		_, _ = io.Copy(errSink, buf)
		buf.Reset()
	}
}
