//go:build !windows

package wrapper

import (
	"os/exec"
	"syscall"
)

// exitCodeFromError returns the exit code matching what a shell would see
// from Terraform. If the child was killed by a signal, return 128+signum
// (the convention real terraform follows when its child is signaled),
// instead of -1 from exec.ExitError.ExitCode().
func exitCodeFromError(exitError *exec.ExitError) int {
	if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitError.ExitCode()
}
