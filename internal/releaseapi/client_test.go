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

// fakeReleaseServer serves a single Terraform "release" so we can exercise
// the full ListReleases + DownloadRelease pipeline without hitting
// releases.hashicorp.com. It returns the server, the zip's sha256, and the
// raw zip bytes (useful for tampering tests).
func fakeReleaseServer(t *testing.T, version string) (*httptest.Server, string, []byte) {
	t.Helper()

	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	binName := "terraform"
	if runtime.GOOS == "windows" {
		binName = "terraform.exe"
	}

	// Build a minimal zip containing a "terraform" binary.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create(binName)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte("#!/bin/sh\necho fake terraform " + version + "\n")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zipBytes := zipBuf.Bytes()

	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	shasumsName := fmt.Sprintf("terraform_%s_SHA256SUMS", version)
	shasumsBody := fmt.Sprintf("%s  %s\n", hexSum, zipName)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)

	indexPath := "/index.json"
	zipPath := fmt.Sprintf("/%s/%s", version, zipName)
	shasumsPath := fmt.Sprintf("/%s/%s", version, shasumsName)

	mux.HandleFunc(indexPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
  "versions": {
    %q: {
      "version": %q,
      "shasums": %q,
      "builds": [
        {"version": %q, "os": %q, "arch": %q, "url": %q}
      ]
    }
  }
}`, version, version, shasumsName, version, runtime.GOOS, runtime.GOARCH, srv.URL+zipPath)
	})
	mux.HandleFunc(shasumsPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(shasumsBody))
	})
	mux.HandleFunc(zipPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	})

	t.Cleanup(srv.Close)
	return srv, hexSum, zipBytes
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

func TestListReleases_ParsesIndex(t *testing.T) {
	srv, _, _ := fakeReleaseServer(t, "1.5.0")
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
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
	srv, _, _ := fakeReleaseServer(t, "1.5.0")
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
	// Stand up a server where the zip body doesn't match the published
	// SHA256SUMS — DownloadRelease must refuse it.
	version := "1.5.0"
	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	shasumsName := fmt.Sprintf("terraform_%s_SHA256SUMS", version)

	// Pretend the zip has this checksum, but actually serve different bytes.
	bogusSum := strings.Repeat("a", 64)
	shasumsBody := fmt.Sprintf("%s  %s\n", bogusSum, zipName)

	// Build a real-looking zip with arbitrary content.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create("terraform")
	_, _ = w.Write([]byte("not the right bytes"))
	_ = zw.Close()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
  "versions": {
    %q: {
      "version": %q,
      "shasums": %q,
      "builds": [
        {"version": %q, "os": %q, "arch": %q, "url": %q}
      ]
    }
  }
}`, version, version, shasumsName, version, runtime.GOOS, runtime.GOARCH,
			srv.URL+"/"+version+"/"+zipName)
	})
	mux.HandleFunc("/"+version+"/"+shasumsName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(shasumsBody))
	})
	mux.HandleFunc("/"+version+"/"+zipName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBuf.Bytes())
	})

	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions[version]

	_, err = c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum error, got: %v", err)
	}
}

func TestDownloadRelease_NoBuildForOSArch(t *testing.T) {
	srv, _, _ := fakeReleaseServer(t, "1.5.0")
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions["1.5.0"]

	_, err = c.DownloadRelease(rel, "plan9", "mips")
	if err == nil {
		t.Fatal("expected error for unknown os/arch, got nil")
	}
}

// TestDownloadRelease_FailsWhenSumNotInChecksumFile is a regression test for
// C1: previously a missing entry in SHA256SUMS caused checkSha256Sum to stay
// "" and the verification was silently skipped. Now the entire flow must
// fail.
func TestDownloadRelease_FailsWhenSumNotInChecksumFile(t *testing.T) {
	version := "1.5.0"
	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	shasumsName := fmt.Sprintf("terraform_%s_SHA256SUMS", version)

	// SHA256SUMS deliberately lists a different filename, so no entry
	// matches our build's zip.
	shasumsBody := fmt.Sprintf("%s  some_other_file.zip\n", strings.Repeat("a", 64))

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create("terraform")
	_, _ = w.Write([]byte("payload"))
	_ = zw.Close()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"versions":{%q:{"version":%q,"shasums":%q,"builds":[{"version":%q,"os":%q,"arch":%q,"url":%q}]}}}`,
			version, version, shasumsName, version, runtime.GOOS, runtime.GOARCH,
			srv.URL+"/"+version+"/"+zipName)
	})
	mux.HandleFunc("/"+version+"/"+shasumsName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(shasumsBody))
	})
	mux.HandleFunc("/"+version+"/"+zipName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBuf.Bytes())
	})

	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions[version]

	_, err = c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
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
	version := "1.5.0"
	zipName := fmt.Sprintf("terraform_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
	shasumsName := fmt.Sprintf("terraform_%s_SHA256SUMS", version)

	binName := "terraform"
	if runtime.GOOS == "windows" {
		binName = "terraform.exe"
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create(binName)
	_, _ = w.Write([]byte("payload"))
	_ = zw.Close()
	zipBytes := zipBuf.Bytes()
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])

	// Mix in: blank lines, a malformed single-token line, extra whitespace,
	// a legitimate line for our zip.
	shasumsBody := strings.Join([]string{
		"",
		"   ",
		"malformedline",
		fmt.Sprintf("%s  some_other.zip", strings.Repeat("b", 64)),
		fmt.Sprintf("%s  %s", hexSum, zipName),
		"",
	}, "\n")

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"versions":{%q:{"version":%q,"shasums":%q,"builds":[{"version":%q,"os":%q,"arch":%q,"url":%q}]}}}`,
			version, version, shasumsName, version, runtime.GOOS, runtime.GOARCH,
			srv.URL+"/"+version+"/"+zipName)
	})
	mux.HandleFunc("/"+version+"/"+shasumsName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(shasumsBody))
	})
	mux.HandleFunc("/"+version+"/"+zipName, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	})

	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	idx, err := c.ListReleases()
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rel := idx.Versions[version]

	_, err = c.DownloadRelease(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("expected success despite messy SHA256SUMS, got: %v", err)
	}
}

// TestDownloadRelease_FallsBackToReleaseVersion: when the upstream JSON has
// no "version" field on a Build (or it fails to decode), DownloadRelease
// must not panic in executableName(). It falls back to Release.Version.
func TestDownloadRelease_FallsBackToReleaseVersion(t *testing.T) {
	srv, _, _ := fakeReleaseServer(t, "1.5.0")
	withTestURLs(t, srv.URL)

	c, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
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
