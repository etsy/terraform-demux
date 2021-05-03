package wrapper

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/etsy/terraform-demux/internal/releaseapi"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"github.com/pkg/errors"
)

func RunTerraform(args []string) (int, error) {
	cacheDirectory, err := ensureCacheDirectory()

	if err != nil {
		return 1, err
	}

	workingDirectory, err := os.Getwd()

	if err != nil {
		return 1, errors.Wrap(err, "could not get working directory")
	}

	terraformVersionConstraints, err := getTerraformVersionConstraints(workingDirectory)

	if err != nil {
		return 1, err
	}

	client := releaseapi.NewClient(&http.Client{}, cacheDirectory)

	listResponse, err := client.ListReleases(context.TODO())

	if err != nil {
		return 1, err
	}

	matchingRelease, err := filterReleases(listResponse.Releases, terraformVersionConstraints)

	if err != nil {
		return 1, err
	}

	executablePath, err := client.DownloadRelease(matchingRelease, runtime.GOOS, runtime.GOARCH)

	if err != nil {
		return 1, err
	}

	return runTerraform(executablePath, args)
}

func ensureCacheDirectory() (string, error) {
	userCacheDir, err := os.UserCacheDir()

	if err != nil {
		return "", errors.Wrap(err, "could not determine user's cache directory")
	}

	wrapperCacheDir := filepath.Join(userCacheDir, "terraform-demux")

	if err := os.MkdirAll(wrapperCacheDir, 0755); err != nil {
		return "", errors.Wrapf(err, "could not create cache directory '%s'", wrapperCacheDir)
	}

	return wrapperCacheDir, nil
}

func getTerraformVersionConstraints(directory string) ([]version.Constraints, error) {
	currentDirectory := directory

	for {
		module, diags := tfconfig.LoadModule(directory)

		if !diags.HasErrors() && len(module.RequiredCore) > 0 {
			var allConstraints []version.Constraints

			for _, constraintString := range module.RequiredCore {
				constraints, err := version.NewConstraint(constraintString)

				if err != nil {
					return nil, errors.Wrap(err, "could not determine constraint from string")
				}

				allConstraints = append(allConstraints, constraints)
			}

			return allConstraints, nil
		}

		parentDirectory := filepath.Dir(currentDirectory)

		if parentDirectory == currentDirectory {
			return nil, nil
		}

		currentDirectory = parentDirectory
	}
}

func filterReleases(releases []releaseapi.Release, constraints []version.Constraints) (releaseapi.Release, error) {
ReleaseVersionLoop:
	for _, release := range releases {
		for _, constraint := range constraints {
			if !constraint.Check(release.Version) {
				continue ReleaseVersionLoop
			}
		}

		// this version matched all of the constraints, so we return early
		return release, nil
	}

	// no version matches all constraints
	return releaseapi.Release{}, errors.Errorf(
		"no Terraform releases appear to satisfy all of the following constraints: %v", constraints,
	)
}

// runTerraform executes Terraform and returns the exit code as an integer.
// Based on https://github.com/bazelbuild/bazelisk/blob/97a0d60468dc696cea3cf1d252b526f1ac6a9090/core/core.go#L405.
func runTerraform(executable string, args []string) (int, error) {
	cmd := makeTerraformCmd(executable, args)

	err := cmd.Start()

	if err != nil {
		return 1, errors.Errorf("could not start Terraform: %v", err)
	}

	c := make(chan os.Signal)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		s := <-c

		if runtime.GOOS != "windows" {
			cmd.Process.Signal(s)
		} else {
			cmd.Process.Kill()
		}
	}()

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.Sys().(syscall.WaitStatus).ExitStatus(), nil
		}

		return 1, fmt.Errorf("error running Terraform: %v", err)
	}

	return 0, nil
}

// makeTerraformCmd returns an exec.Cmd suitable for running Terraform.
// Based on https://github.com/bazelbuild/bazelisk/blob/97a0d60468dc696cea3cf1d252b526f1ac6a9090/core/core.go#L390.
func makeTerraformCmd(executable string, args []string) *exec.Cmd {
	cmd := exec.Command(executable, args...)

	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}
