//go:build windows

package wrapper

import "os/exec"

// exitCodeFromError returns the child's exit code on Windows. There's no
// equivalent of the Unix "killed by signal" path here — a child that's
// killed shows up as a regular non-zero exit code.
func exitCodeFromError(exitError *exec.ExitError) int {
	return exitError.ExitCode()
}
