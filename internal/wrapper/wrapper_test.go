package wrapper

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/etsy/terraform-demux/internal/releaseapi"
)

func mustVersion(t *testing.T, s string) *semver.Version {
	t.Helper()
	v, err := semver.NewVersion(s)
	if err != nil {
		t.Fatalf("invalid version %q: %v", s, err)
	}
	return v
}

func mustConstraint(t *testing.T, s string) *semver.Constraints {
	t.Helper()
	c, err := semver.NewConstraint(s)
	if err != nil {
		t.Fatalf("invalid constraint %q: %v", s, err)
	}
	return c
}

func indexFromVersions(t *testing.T, versions ...string) releaseapi.ReleaseIndex {
	t.Helper()
	idx := releaseapi.ReleaseIndex{Versions: map[string]releaseapi.Release{}}
	for _, v := range versions {
		idx.Versions[v] = releaseapi.Release{Version: mustVersion(t, v)}
	}
	return idx
}

func TestFilterReleases_PicksNewestSatisfying(t *testing.T) {
	idx := indexFromVersions(t, "0.11.15", "0.14.11", "1.0.3", "1.5.0", "1.7.2")

	got, err := filterReleases(idx, []*semver.Constraints{mustConstraint(t, "~> 0.14.0")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.String() != "0.14.11" {
		t.Errorf("expected 0.14.11, got %s", got.Version)
	}
}

func TestFilterReleases_MultipleConstraintsAreANDed(t *testing.T) {
	idx := indexFromVersions(t, "1.0.3", "1.4.7", "1.5.0", "1.7.2")

	got, err := filterReleases(idx, []*semver.Constraints{
		mustConstraint(t, ">= 1.4.0"),
		mustConstraint(t, "< 1.7.0"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.String() != "1.5.0" {
		t.Errorf("expected 1.5.0, got %s", got.Version)
	}
}

func TestFilterReleases_NoMatchReturnsError(t *testing.T) {
	idx := indexFromVersions(t, "1.0.3", "1.5.0")

	_, err := filterReleases(idx, []*semver.Constraints{mustConstraint(t, ">= 2.0.0")})
	if err == nil {
		t.Fatal("expected error when no version satisfies constraints")
	}
}

func TestFilterReleases_PrereleaseSkipped(t *testing.T) {
	idx := releaseapi.ReleaseIndex{Versions: map[string]releaseapi.Release{
		"1.6.6":     {Version: mustVersion(t, "1.6.6")},
		"1.7.0-rc1": {Version: mustVersion(t, "1.7.0-rc1")},
	}}

	got, err := filterReleases(idx, []*semver.Constraints{mustConstraint(t, ">= 1.6.0")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.String() != "1.6.6" {
		t.Errorf("expected 1.6.6, got %s", got.Version)
	}
}

func TestFilterReleases_NoConstraintsReturnsLatest(t *testing.T) {
	idx := indexFromVersions(t, "1.0.3", "1.5.0", "1.7.2")

	got, err := filterReleases(idx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.String() != "1.7.2" {
		t.Errorf("expected latest 1.7.2, got %s", got.Version)
	}
}

func writeTerraformConfig(t *testing.T, dir, requiredVersion string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "terraform {\n  required_version = \"" + requiredVersion + "\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "terraform.tf"), []byte(body), 0644); err != nil {
		t.Fatalf("write tf: %v", err)
	}
}

func TestGetTerraformVersionConstraints_FoundInCwd(t *testing.T) {
	dir := t.TempDir()
	writeTerraformConfig(t, dir, "~> 1.5.0")

	constraints, err := getTerraformVersionConstraints(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(constraints))
	}
	if !constraints[0].Check(mustVersion(t, "1.5.7")) {
		t.Errorf("constraint should accept 1.5.7")
	}
	if constraints[0].Check(mustVersion(t, "1.6.0")) {
		t.Errorf("constraint should reject 1.6.0")
	}
}

func TestGetTerraformVersionConstraints_WalksUpToParent(t *testing.T) {
	root := t.TempDir()
	writeTerraformConfig(t, root, ">= 1.0.0")
	child := filepath.Join(root, "modules", "foo")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	constraints, err := getTerraformVersionConstraints(child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(constraints) != 1 {
		t.Fatalf("expected to find parent's constraint, got %d", len(constraints))
	}
}

func TestGetTerraformVersionConstraints_NoneFound(t *testing.T) {
	// Empty tempdir up to filesystem root yields no constraints (assumes no
	// stray /terraform.tf at the root, which is safe in practice).
	dir := t.TempDir()

	constraints, err := getTerraformVersionConstraints(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if constraints != nil {
		t.Errorf("expected nil constraints, got %v", constraints)
	}
}

// TestRunTerraform_ExitCodePropagation regression-tests C3: the wrapper must
// return the same exit code as the wrapped binary on every supported
// platform. The previous syscall.WaitStatus type assertion was not portable
// to Windows. We invoke the test binary itself as a fake "terraform" via the
// TestHelperProcess pattern.
func TestRunTerraform_ExitCodePropagation(t *testing.T) {
	for _, want := range []int{0, 1, 2, 42} {
		want := want
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			t.Setenv("GO_WANT_HELPER_PROCESS", "1")
			t.Setenv("HELPER_EXIT_CODE", strconv.Itoa(want))

			got, err := runTerraform(os.Args[0], []string{
				"-test.run=TestHelperProcess", "--", "fake-terraform",
			})
			if err != nil {
				t.Fatalf("runTerraform: %v", err)
			}
			if got != want {
				t.Errorf("got exit code %d, want %d", got, want)
			}
		})
	}
}

// TestHelperProcess is not a real test — it's the child process spawned by
// TestRunTerraform_ExitCodePropagation. When invoked with
// GO_WANT_HELPER_PROCESS=1 it exits with HELPER_EXIT_CODE.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	code, _ := strconv.Atoi(os.Getenv("HELPER_EXIT_CODE"))
	os.Exit(code)
}
