package wrapper

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"

	"github.com/etsy/terraform-demux/internal/releaseapi"

	"github.com/Masterminds/semver/v3"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"github.com/pkg/errors"
)

func RunTerraform(args []string, arch string) (int, error) {
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

	client := releaseapi.NewClient(cacheDirectory)

	releaseIndex, err := client.ListReleases()

	if err != nil {
		return 1, err
	}

	matchingRelease, err := filterReleases(releaseIndex, terraformVersionConstraints)

	if err != nil {
		return 1, err
	}

	log.Printf("version '%s' matches all constraints", matchingRelease.Version)

	if err := checkStateCommand(args, matchingRelease.Version); err != nil {
		return 1, err
	}

	executablePath, err := client.DownloadRelease(matchingRelease, runtime.GOOS, arch)

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

func getTerraformVersionConstraints(directory string) ([]*semver.Constraints, error) {
	currentDirectory := directory

	for {
		log.Printf("inspecting terraform module in %s", currentDirectory)

		module, diags := tfconfig.LoadModule(currentDirectory)

		if diags.HasErrors() {
			log.Printf("encountered error parsing configuration: %v", diags.Err())
		} else if len(module.RequiredCore) > 0 {
			var allConstraints []*semver.Constraints

			for _, constraintString := range module.RequiredCore {
				constraints, err := semver.NewConstraint(constraintString)

				if err != nil {
					return nil, errors.Wrap(err, "could not determine constraint from string")
				}

				allConstraints = append(allConstraints, constraints)
			}

			log.Printf("found constraints: %v", allConstraints)

			return allConstraints, nil
		}

		parentDirectory := filepath.Dir(currentDirectory)

		if parentDirectory == currentDirectory {
			log.Printf("no constraints found")

			return nil, nil
		}

		currentDirectory = parentDirectory
	}
}

func filterReleases(index releaseapi.ReleaseIndex, constraints []*semver.Constraints) (releaseapi.Release, error) {
	var versions semver.Collection

	for _, release := range index.Versions {
		if release.Version.Prerelease() != "" {
			continue
		}

		versions = append(versions, release.Version)
	}

	sort.Sort(sort.Reverse(versions))

ReleaseVersionLoop:
	for _, version := range versions {
		for _, constraint := range constraints {
			if !constraint.Check(version) {
				continue ReleaseVersionLoop
			}
		}

		// this version matched all of the constraints, so we return early
		return index.Versions[version.String()], nil
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
