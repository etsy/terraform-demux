//go:build windows

package wrapper

import "os"

// On Windows, os.Interrupt is the only meaningful signal to forward — the
// rest of the Unix signal set isn't delivered to console processes the
// same way. The runTerraform goroutine kills the child on any forwarded
// signal anyway because Windows doesn't honor cmd.Process.Signal for
// arbitrary signals.
var forwardedSignals = []os.Signal{os.Interrupt}
