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
	"strings"

	"github.com/etsy/terraform-demux/internal/releaseapi"

	"github.com/Masterminds/semver/v3"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
)

// cacheOverrideEnv lets users (and the integration test on macOS, where
// XDG_CACHE_HOME is ignored by os.UserCacheDir) point the cache at a
// specific directory instead of $HOME/Library/Caches/terraform-demux.
const cacheOverrideEnv = "TF_DEMUX_CACHE_HOME"

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
	if override := os.Getenv(cacheOverrideEnv); override != "" {
		if err := os.MkdirAll(override, 0755); err != nil {
			return "", fmt.Errorf("could not create cache directory %q: %w", override, err)
		}
		return override, nil
	}

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

// getTerraformVersionConstraints walks from directory up to the filesystem
// root, returning the first set of required_version constraints it finds.
// Parse errors are fatal only at the user's cwd; broken .tf files in
// unrelated parents are logged and skipped so the wrapper still works.
func getTerraformVersionConstraints(directory string) ([]*semver.Constraints, error) {
	currentDirectory := directory
	isCwd := true

	for {
		constraints, found, err := loadConstraintsAt(currentDirectory, isCwd)
		if err != nil {
			return nil, err
		}
		if found {
			return constraints, nil
		}

		parentDirectory := filepath.Dir(currentDirectory)
		if parentDirectory == currentDirectory {
			log.Printf("no constraints found")
			return nil, nil
		}
		currentDirectory = parentDirectory
		isCwd = false
	}
}

// loadConstraintsAt parses any Terraform module in dir. It returns
// (constraints, true, nil) if the module declares required_version,
// (nil, false, nil) if there's nothing here to consider, and
// (nil, false, err) only for fatal parse errors at the user's cwd.
func loadConstraintsAt(dir string, isCwd bool) ([]*semver.Constraints, bool, error) {
	log.Printf("inspecting terraform module in %s", dir)

	module, diags := tfconfig.LoadModule(dir)
	if diags.HasErrors() {
		// Only the user's actual working directory is a hard failure.
		// A broken .tf in some unrelated parent (e.g. ~/scratch/foo)
		// must not brick every wrapper invocation underneath it.
		if isCwd {
			return nil, false, fmt.Errorf("invalid terraform configuration in %s: %w", dir, diags.Err())
		}
		log.Printf("ignoring parse errors in parent %s: %v", dir, diags.Err())
		return nil, false, nil
	}
	if len(module.RequiredCore) == 0 {
		return nil, false, nil
	}

	var allConstraints []*semver.Constraints
	for _, constraintString := range module.RequiredCore {
		c, err := semver.NewConstraint(constraintString)
		if err != nil {
			return nil, false, fmt.Errorf("could not parse required_version %q: %w", constraintString, err)
		}
		allConstraints = append(allConstraints, c)
	}

	log.Printf("found constraints: %v", allConstraints)
	return allConstraints, true, nil
}

func filterReleases(index releaseapi.ReleaseIndex, constraints []*semver.Constraints) (releaseapi.Release, error) {
	var versions semver.Collection

	for _, release := range index.Versions {
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
	signal.Notify(c, forwardedSignals...)
	defer signal.Stop(c)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case s := <-c:
				if runtime.GOOS == "windows" {
					if err := cmd.Process.Kill(); err != nil {
						log.Printf("could not kill Terraform: %v", err)
					}
				} else {
					if err := cmd.Process.Signal(s); err != nil {
						log.Printf("could not forward %s to Terraform: %v", s, err)
					}
				}
			case <-done:
				return
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitCodeFromError(exitError), nil
		}
		return 1, fmt.Errorf("error running Terraform: %w", err)
	}

	return 0, nil
}

// makeTerraformCmd returns an exec.Cmd suitable for running Terraform.
// Based on https://github.com/bazelbuild/bazelisk/blob/97a0d60468dc696cea3cf1d252b526f1ac6a9090/core/core.go#L390.
func makeTerraformCmd(executable string, args []string) *exec.Cmd {
	cmd := exec.Command(executable, args...)

	cmd.Env = filterDemuxEnv(os.Environ())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

// filterDemuxEnv strips TF_DEMUX_* variables before exec'ing the child
// Terraform. They are wrapper-internal config (logging, arch override,
// state-command bypass, cache override) and must not leak into Terraform's
// own subprocesses, which can include another terraform-demux invocation.
func filterDemuxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "TF_DEMUX_") {
			continue
		}
		out = append(out, e)
	}
	return out
}
