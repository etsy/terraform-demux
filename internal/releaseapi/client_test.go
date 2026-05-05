package releaseapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// serveRelease stands up an httptest server that serves a single Terraform
// release. Tests can pass their own zipBytes and shasumsBody to simulate
// happy paths, corrupted archives, missing checksum entries, or messy
// SHA256SUMS formats. The returned URL paths follow HashiCorp's layout:
//
//	GET /index.json
//	GET /<version>/terraform_<v>_SHA256SUMS
//	GET /<version>/terraform_<v>_<os>_<arch>.zip
func serveRelease(t *testing.T, version string, zipBytes []byte, shasumsBody string) *httptest.Server {
	t.Helper()

	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	shasumsName := fmt.Sprintf("terraform_%s_SHA256SUMS", version)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"versions":{%q:{"version":%q,"shasums":%q,"builds":[{"version":%q,"os":%q,"arch":%q,"url":%q}]}}}`,
			version, version, shasumsName, version, runtime.GOOS, runtime.GOARCH,
			srv.URL+"/"+version+"/"+zipName)
	})
	mux.HandleFunc("/"+version+"/"+shasumsName, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(shasumsBody))
	})
	mux.HandleFunc("/"+version+"/"+zipName, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})

	return srv
}

// terraformZip builds a minimal zip whose only entry is the platform's
// terraform binary, named appropriately for runtime.GOOS.
func terraformZip(t *testing.T, payload string) []byte {
	t.Helper()
	binName := "terraform"
	if runtime.GOOS == "windows" {
		binName = "terraform.exe"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(binName)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// shasumsLine returns the canonical "<sha>  <filename>\n" entry for a
// single archive — what HashiCorp ships in SHA256SUMS files.
func shasumsLine(zipBytes []byte, version string) string {
	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(zipBytes)
	return fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), zipName)
}

// fakeReleaseServer is the happy-path wrapper around serveRelease: it
// produces a release where the SHA256SUMS file correctly signs the zip.
func fakeReleaseServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	zipBytes := terraformZip(t, "#!/bin/sh\necho fake terraform "+version+"\n")
	return serveRelease(t, version, zipBytes, shasumsLine(zipBytes, version))
}

func withTestURLs(t *testing.T, srvURL string) {
	t.Helper()
	origIndex, origRoot := releasesURL, releaseRootURL
	releasesURL = srvURL + "/index.json"
	releaseRootURL = srvURL
	t.Cleanup(func() {
		releasesURL = origIndex
		releaseRootURL = origRoot
	})
}

// newTestClient is a small fixture: stand up the fake server, point the
// package URLs at it, return a fresh Client backed by a temp cache.
func newTestClient(t *testing.T, version string) (*Client, *httptest.Server) {
	t.Helper()
	srv := fakeReleaseServer(t, version)
	withTestURLs(t, srv.URL)
	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func TestListReleases_ParsesIndex(t *testing.T) {
	c, _ := newTestClient(t, "1.5.0")

	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	r, ok := idx.Versions["1.5.0"]
	if !ok {
		t.Fatalf("expected version 1.5.0 in index, got %v", idx.Versions)
	}
	if r.Version.String() != "1.5.0" {
		t.Errorf("got version %s, want 1.5.0", r.Version)
	}
	if len(r.Builds) != 1 {
		t.Fatalf("expected 1 build, got %d", len(r.Builds))
	}
}

func TestDownloadRelease_WritesAndCachesBinary(t *testing.T) {
	srv := fakeReleaseServer(t, "1.5.0")
	withTestURLs(t, srv.URL)

	cacheDir := t.TempDir()
	c, err := NewClient(cacheDir)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions["1.5.0"]

	path, err := c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("DownloadRelease: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("downloaded binary is empty")
	}
	wantDir := filepath.Join(cacheDir, "bin")
	if filepath.Dir(path) != wantDir {
		t.Errorf("expected binary in %s, got %s", wantDir, path)
	}

	// Second call should hit the cached binary (no re-download).
	path2, err := c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("second DownloadRelease: %v", err)
	}
	if path != path2 {
		t.Errorf("expected same cached path, got %s vs %s", path, path2)
	}
}

func TestDownloadRelease_RejectsCorruptedArchive(t *testing.T) {
	// Server signs the zip with a checksum that doesn't match its bytes.
	zipBytes := terraformZip(t, "not the right bytes")
	zipName := fmt.Sprintf("terraform_1.5.0_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	bogusBody := fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), zipName)
	srv := serveRelease(t, "1.5.0", zipBytes, bogusBody)
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}

	_, err = c.DownloadRelease(idx.Versions["1.5.0"], runtime.GOOS, runtime.GOARCH)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum error, got: %v", err)
	}
}

func TestDownloadRelease_NoBuildForOSArch(t *testing.T) {
	c, _ := newTestClient(t, "1.5.0")
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}

	_, err = c.DownloadRelease(idx.Versions["1.5.0"], "plan9", "mips")
	if err == nil {
		t.Fatal("expected error for unknown os/arch, got nil")
	}
}

// TestDownloadRelease_FailsWhenSumNotInChecksumFile is a regression test
// for C1: previously a missing entry in SHA256SUMS caused the verification
// to be silently skipped. Now the entire flow must fail.
func TestDownloadRelease_FailsWhenSumNotInChecksumFile(t *testing.T) {
	zipBytes := terraformZip(t, "payload")
	// SHASUMS lists a different filename, so no entry matches our zip.
	body := fmt.Sprintf("%s  some_other_file.zip\n", strings.Repeat("a", 64))
	srv := serveRelease(t, "1.5.0", zipBytes, body)
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}

	_, err = c.DownloadRelease(idx.Versions["1.5.0"], runtime.GOOS, runtime.GOARCH)
	if err == nil {
		t.Fatal("expected error when SHA256SUMS has no matching entry, got nil")
	}
	if !strings.Contains(err.Error(), "no checksum entry") {
		t.Errorf("expected no-checksum-entry error, got: %v", err)
	}
}

// TestDownloadRelease_TolerantSHASUMSParse is a regression test: a
// SHA256SUMS file with extra blank lines and oddly-spaced entries must not
// panic the parser (the previous strings.Split(line, "  ") + checksum[1]
// path would index past the end of the slice).
func TestDownloadRelease_TolerantSHASUMSParse(t *testing.T) {
	zipBytes := terraformZip(t, "payload")
	zipName := fmt.Sprintf("terraform_1.5.0_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(zipBytes)
	body := strings.Join([]string{
		"",
		"   ",
		"malformedline",
		fmt.Sprintf("%s  some_other.zip", strings.Repeat("b", 64)),
		fmt.Sprintf("%s  %s", hex.EncodeToString(sum[:]), zipName),
		"",
	}, "\n")

	srv := serveRelease(t, "1.5.0", zipBytes, body)
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}

	if _, err = c.DownloadRelease(idx.Versions["1.5.0"], runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("expected success despite messy SHA256SUMS, got: %v", err)
	}
}

// TestDownloadRelease_FallsBackToReleaseVersion: when the upstream JSON has
// no "version" field on a Build (or it fails to decode), DownloadRelease
// must not panic in executableName(). It falls back to Release.Version.
func TestDownloadRelease_FallsBackToReleaseVersion(t *testing.T) {
	c, _ := newTestClient(t, "1.5.0")

	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions["1.5.0"]
	for i := range rel.Builds {
		rel.Builds[i].Version = nil
	}

	path, err := c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("expected fallback to release Version, got: %v", err)
	}
	if !strings.Contains(path, "1.5.0") {
		t.Errorf("expected path to include version, got: %s", path)
	}
}

// TestListReleases_UsesLocalIndexTTL: a second ListReleases call within
// the TTL window must not hit the network. We assert by closing the
// upstream server and verifying the second call still succeeds.
func TestListReleases_UsesLocalIndexTTL(t *testing.T) {
	c, srv := newTestClient(t, "1.5.0")

	if _, err := c.ListReleases(); err != nil {
		t.Fatalf("first ListReleases: %v", err)
	}
	// Take server down — second call must succeed from local cache.
	srv.Close()

	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("second ListReleases (server down): %v", err)
	}
	if _, ok := idx.Versions["1.5.0"]; !ok {
		t.Errorf("expected cached index to contain 1.5.0")
	}
}

func TestNewClient_CreatesSubdirs(t *testing.T) {
	cacheDir := t.TempDir()
	if _, err := NewClient(cacheDir); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, sub := range []string{"http", "bin"} {
		path := filepath.Join(cacheDir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s subdir, got: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s exists but is not a directory", path)
		}
	}
}
