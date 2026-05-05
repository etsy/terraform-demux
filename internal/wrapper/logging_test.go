package wrapper

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// SetupLogging mutates the package-level log destination, so these tests
// must not run in parallel with anything that uses log.

func TestSetupLogging_BuffersUntilFlush(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var sink bytes.Buffer
	flush := SetupLogging(false, &sink)

	log.Print("first message")
	log.Print("second message")

	if sink.Len() != 0 {
		t.Errorf("expected nothing on sink before flush, got: %q", sink.String())
	}

	flush()

	out := sink.String()
	if !strings.Contains(out, "first message") || !strings.Contains(out, "second message") {
		t.Errorf("expected flushed output to contain both messages, got: %q", out)
	}
}

func TestSetupLogging_VerboseStreamsImmediately(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var sink bytes.Buffer
	_ = SetupLogging(true, &sink)

	log.Print("streamed")

	if !strings.Contains(sink.String(), "streamed") {
		t.Errorf("expected verbose mode to stream immediately, got: %q", sink.String())
	}
}
