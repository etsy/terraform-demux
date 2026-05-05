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
)

func RunTerraform(args []string, arch string) (int, error) {
	cacheDirectory, err := ensureCacheDirectory()
	if err != nil {
		return 1, err
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return 1, fmt.Errorf("could not get working directory: %w", err)
	}

	terraformVersionConstraints, err := getTerraformVersionConstraints(workingDirectory)
	if err != nil {
		return 1, err
	}

	client, err := releaseapi.NewClient(cacheDirectory)
	if err != nil {
		return 1, err
	}

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
		return "", fmt.Errorf("could not determine user's cache directory: %w", err)
	}

	wrapperCacheDir := filepath.Join(userCacheDir, "terraform-demux")
	if err := os.MkdirAll(wrapperCacheDir, 0755); err != nil {
		return "", fmt.Errorf("could not create cache directory %q: %w", wrapperCacheDir, err)
	}

	return wrapperCacheDir, nil
}

func getTerraformVersionConstraints(directory string) ([]*semver.Constraints, error) {
	currentDirectory := directory

	for {
		log.Printf("inspecting terraform module in %s", currentDirectory)

		module, diags := tfconfig.LoadModule(currentDirectory)

		// LoadModule returns success with an empty module when the directory
		// has no .tf files, so HasErrors() means a real parse problem in
		// something the user wrote — surface it instead of silently
		// falling through to the latest stable release.
		if diags.HasErrors() {
			return nil, fmt.Errorf("invalid terraform configuration in %s: %w", currentDirectory, diags.Err())
		}

		if len(module.RequiredCore) > 0 {
			var allConstraints []*semver.Constraints

			for _, constraintString := range module.RequiredCore {
				constraints, err := semver.NewConstraint(constraintString)
				if err != nil {
					return nil, fmt.Errorf("could not parse required_version %q: %w", constraintString, err)
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
		// Defensive: a release may decode without a Version (missing or
		// unparseable field). Skip rather than panic on later access.
		if release.Version == nil {
			continue
		}
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

		return index.Versions[version.String()], nil
	}

	return releaseapi.Release{}, fmt.Errorf("no Terraform releases appear to satisfy all of the following constraints: %v", constraints)
}

// runTerraform executes Terraform and returns its exit code.
// Based on https://github.com/bazelbuild/bazelisk/blob/97a0d60468dc696cea3cf1d252b526f1ac6a9090/core/core.go#L405.
func runTerraform(executable string, args []string) (int, error) {
	cmd := makeTerraformCmd(executable, args)

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("could not start Terraform: %w", err)
	}

	// Buffered so signal.Notify never drops a delivery.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(c)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case s := <-c:
				if runtime.GOOS == "windows" {
					_ = cmd.Process.Kill()
				} else {
					_ = cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode(), nil
		}
		return 1, fmt.Errorf("error running Terraform: %w", err)
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
