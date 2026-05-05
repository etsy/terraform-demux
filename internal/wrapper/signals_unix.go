//go:build !windows

package wrapper

import (
	"os"
	"syscall"
)

// forwardedSignals lists the signals we propagate from the wrapper to the
// child Terraform process. Terraform itself decides what to do with them
// (e.g., SIGINT/SIGTERM trigger graceful shutdown). Without SIGHUP the
// child is orphaned when the parent terminal closes.
var forwardedSignals = []os.Signal{
	os.Interrupt,
	syscall.SIGTERM,
	syscall.SIGHUP,
	syscall.SIGQUIT,
}
