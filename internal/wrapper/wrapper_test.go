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

// H3 regression: a syntactically-broken terraform.tf must not be silently
// swallowed and treated as "no constraint" (which would then resolve to the
// latest stable Terraform). The error has to surface.
func TestGetTerraformVersionConstraints_SurfacesParseError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.tf"), []byte("this is not valid hcl {{{"), 0644); err != nil {
		t.Fatalf("write tf: %v", err)
	}

	_, err := getTerraformVersionConstraints(dir)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// H-A regression: an unrelated parent directory with a broken .tf must
// not block the wrapper from running in the (clean) child directory. We
// only care about the user's actual cwd, not arbitrary ancestors.
func TestGetTerraformVersionConstraints_TolerantOfParentParseErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "terraform.tf"), []byte("totally broken {{{"), 0644); err != nil {
		t.Fatalf("write parent tf: %v", err)
	}
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	constraints, err := getTerraformVersionConstraints(child)
	if err != nil {
		t.Fatalf("expected parent parse error to be tolerated, got: %v", err)
	}
	if constraints != nil {
		t.Errorf("expected nil constraints, got %v", constraints)
	}
}

func TestFilterDemuxEnv_StripsTFDemuxKeys(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"TF_DEMUX_LOG=1",
		"HOME=/tmp",
		"TF_DEMUX_ALLOW_STATE_COMMANDS=true",
		"TF_DEMUX_ARCH=amd64",
		"TF_DEMUX_CACHE_HOME=/tmp/cache",
		"USER=alice",
	}
	out := filterDemuxEnv(in)

	want := map[string]bool{"PATH=/usr/bin": true, "HOME=/tmp": true, "USER=alice": true}
	if len(out) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(out), out)
	}
	for _, e := range out {
		if !want[e] {
			t.Errorf("unexpected entry %q in filtered env", e)
		}
	}
}

func TestEnsureCacheDirectory_HonorsOverrideEnv(t *testing.T) {
	override := filepath.Join(t.TempDir(), "td-cache")
	t.Setenv(cacheOverrideEnv, override)

	got, err := ensureCacheDirectory()
	if err != nil {
		t.Fatalf("ensureCacheDirectory: %v", err)
	}
	if got != override {
		t.Errorf("expected cache dir %q, got %q", override, got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("override path %q is not a directory", got)
	}
}

// H2 regression: a release with a nil Version (e.g., the upstream JSON
// omits or fails to decode the "version" field) must be skipped, not
// dereferenced.
func TestFilterReleases_SkipsNilVersion(t *testing.T) {
	idx := releaseapi.ReleaseIndex{Versions: map[string]releaseapi.Release{
		"1.5.0": {Version: mustVersion(t, "1.5.0")},
		"bogus": {Version: nil},
	}}

	got, err := filterReleases(idx, []*semver.Constraints{mustConstraint(t, ">= 1.0.0")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.String() != "1.5.0" {
		t.Errorf("expected 1.5.0, got %s", got.Version)
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
