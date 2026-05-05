// Package releaseapi talks to releases.hashicorp.com to discover and
// download Terraform releases. It also maintains a local cache for both
// the parsed release index (with a short TTL) and extracted binaries.
package releaseapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/gofrs/flock"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/natefinch/atomic"
)

// These are var rather than const so tests can override them to point at an
// httptest server.
var (
	releasesURL    = "https://releases.hashicorp.com/terraform/index.json"
	releaseRootURL = "https://releases.hashicorp.com/terraform"
)

const (
	httpCacheSubdir = "http"
	binCacheSubdir  = "bin"

	// indexCacheFile holds a parsed copy of the upstream release index so
	// repeated invocations within a short window skip the network fetch
	// entirely. This is layered on top of httpcache: httpcache still
	// revalidates with HashiCorp on every call (which is a 304 round-trip),
	// while this local TTL avoids even that round-trip when the cache is
	// fresh.
	indexCacheFile = "index.json"

	// indexCacheTTL is short enough that a freshly-released Terraform
	// version becomes visible within a few minutes, but long enough that
	// rapid-fire invocations (shell completion, scripts) don't hit the
	// network repeatedly.
	indexCacheTTL = 5 * time.Minute
)

// ReleaseIndex is the parsed shape of releases.hashicorp.com/terraform/index.json.
type ReleaseIndex struct {
	Versions map[string]Release `json:"versions"`
}

// Release is one Terraform version, with the per-platform builds and a
// pointer to the SHA256SUMS file that signs them.
type Release struct {
	Version *semver.Version `json:"version"`
	// Shasums is the *filename* (not URL) of the SHA256SUMS file for this
	// release, e.g. "terraform_1.5.0_SHA256SUMS". Combine with
	// ShaSumsURL to get a full URL.
	Shasums string  `json:"shasums"`
	Builds  []Build `json:"builds"`
}

// Build is one (os, arch) variant of a Release. URL points at the .zip
// archive containing the terraform binary.
type Build struct {
	Version *semver.Version `json:"version"`
	OS      string          `json:"os"`
	Arch    string          `json:"arch"`
	URL     string          `json:"url"`
}

// Client downloads Terraform releases and caches them on disk.
type Client struct {
	cacheDir   string
	binDir     string
	httpClient *http.Client
}

// NewClient prepares a Client that writes httpcache entries and downloaded
// binaries into separate subdirectories of cacheDir, so the two never
// commingle.
func NewClient(cacheDir string) (*Client, error) {
	httpDir := filepath.Join(cacheDir, httpCacheSubdir)
	binDir := filepath.Join(cacheDir, binCacheSubdir)

	if err := os.MkdirAll(httpDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create http cache dir %q: %w", httpDir, err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create bin cache dir %q: %w", binDir, err)
	}

	httpClient := httpcache.NewTransport(diskcache.New(httpDir)).Client()
	return &Client{cacheDir: cacheDir, binDir: binDir, httpClient: httpClient}, nil
}

// ListReleases returns the parsed Terraform release index, preferring a
// fresh local cache copy over a network round-trip.
func (c *Client) ListReleases() (ReleaseIndex, error) {
	if idx, ok := c.readIndexCache(); ok {
		return idx, nil
	}

	idx, err := c.fetchIndex()
	if err != nil {
		return idx, err
	}

	c.writeIndexCache(idx)
	return idx, nil
}

func (c *Client) readIndexCache() (ReleaseIndex, bool) {
	var idx ReleaseIndex
	path := filepath.Join(c.cacheDir, indexCacheFile)

	info, err := os.Stat(path)
	if err != nil {
		return idx, false
	}
	if time.Since(info.ModTime()) > indexCacheTTL {
		return idx, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return idx, false
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		// Corrupt cache file: log so the user can debug "why is my cache
		// not working", and fall through to a network fetch which will
		// rewrite a clean index.
		log.Printf("ignoring corrupt index cache at %s: %v", path, err)
		return idx, false
	}

	log.Printf("using local index cache (age %s)", time.Since(info.ModTime()).Truncate(time.Second))
	return idx, true
}

func (c *Client) writeIndexCache(idx ReleaseIndex) {
	path := filepath.Join(c.cacheDir, indexCacheFile)
	data, err := json.Marshal(idx)
	if err != nil {
		log.Printf("could not serialize index for local cache: %v", err)
		return
	}
	// atomic.WriteFile ensures a process killed mid-write doesn't leave a
	// half-written file that subsequent reads will treat as the cache.
	if err := atomic.WriteFile(path, bytes.NewReader(data)); err != nil {
		log.Printf("could not write index cache to %s: %v", path, err)
	}
}

func (c *Client) fetchIndex() (ReleaseIndex, error) {
	var releaseIndex ReleaseIndex

	log.Printf("downloading Terraform release index")

	request, err := http.NewRequest("GET", releasesURL, nil)
	if err != nil {
		return releaseIndex, fmt.Errorf("could not create request for Terraform release index: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return releaseIndex, fmt.Errorf("could not send request for Terraform release index: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return releaseIndex, fmt.Errorf("unexpected status code %d when fetching Terraform release index", response.StatusCode)
	}

	if response.Header.Get(httpcache.XFromCache) != "" {
		log.Printf("using cached response")
	}

	if err := json.NewDecoder(response.Body).Decode(&releaseIndex); err != nil {
		return releaseIndex, fmt.Errorf("could not unmarshal release index JSON: %w", err)
	}

	return releaseIndex, nil
}

// DownloadRelease resolves and downloads the (goos, arch) variant of r,
// returning the on-disk path of the cached, verified Terraform binary.
func (c *Client) DownloadRelease(r Release, goos, arch string) (string, error) {
	build, err := findBuild(r, goos, arch)
	if err != nil {
		return "", err
	}

	expectedSum, err := c.expectedChecksumFor(r, &build)
	if err != nil {
		return "", err
	}

	return c.downloadBuild(build, expectedSum)
}

// findBuild picks the matching (goos, arch) variant of r and fills in any
// nil Version field from the parent Release. The cache filename for the
// installed binary uses Build.Version.String(), so a nil pointer here would
// panic later in executableName().
func findBuild(r Release, goos, arch string) (Build, error) {
	for _, build := range r.Builds {
		if build.OS == goos && build.Arch == arch {
			if build.Version == nil {
				build.Version = r.Version
			}
			if build.Version == nil {
				return Build{}, fmt.Errorf("release for OS %q arch %q has no version", goos, arch)
			}
			return build, nil
		}
	}
	return Build{}, fmt.Errorf("could not find matching build for OS %q and arch %q", goos, arch)
}

// expectedChecksumFor downloads the SHA256SUMS file for the release and looks
// up the entry matching this build's archive filename. It is a hard failure
// if the SHA256SUMS file cannot be downloaded or contains no entry for the
// archive — checksum verification must never silently no-op.
func (c *Client) expectedChecksumFor(r Release, build *Build) (string, error) {
	body, err := c.getReleaseCheckSums(r)
	if err != nil {
		return "", fmt.Errorf("could not download checksum file: %w", err)
	}

	target := build.zipFileName()
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line is "<hex-sha256>  <filename>" — Fields tolerates the
		// canonical two-space separator and any unexpected whitespace
		// without panicking on malformed entries.
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == target {
			return parts[0], nil
		}
	}

	return "", fmt.Errorf("no checksum entry for %q in SHA256SUMS for Terraform %s", target, r.Version)
}

func (c *Client) getReleaseCheckSums(release Release) (string, error) {
	request, err := http.NewRequest("GET", release.ShaSumsURL(), nil)
	if err != nil {
		return "", fmt.Errorf("could not create request for Terraform release checksum: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("could not send request for Terraform release checksum: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d when fetching SHA256SUMS", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("could not read SHA256SUMS body: %w", err)
	}
	return string(body), nil
}

// ShaSumsURL returns the absolute URL of the SHA256SUMS file for r.
func (r *Release) ShaSumsURL() string {
	return fmt.Sprintf("%s/%s/%s", releaseRootURL, r.Version, r.Shasums)
}

func (c *Client) downloadBuild(build Build, expectedSum string) (string, error) {
	path := cachedExecutablePath(c.binDir, build)

	if cached, err := executableCached(path); err != nil {
		return "", err
	} else if cached {
		log.Printf("found cached Terraform executable at %s", path)
		return path, nil
	}

	// Serialize parallel processes downloading the same binary. Without
	// this, two `terraform-demux` invocations both miss the cache, both
	// download the archive, and both write to it (last-writer wins via
	// atomic.WriteFile). Worse, the loser deletes its tempfile on exit,
	// which is fine, but the network bandwidth is wasted.
	lockPath := path + ".lock"
	fileLock := flock.New(lockPath)
	if err := fileLock.Lock(); err != nil {
		return "", fmt.Errorf("could not acquire download lock %q: %w", lockPath, err)
	}
	defer func() {
		if err := fileLock.Unlock(); err != nil {
			log.Printf("could not release download lock %q: %v", lockPath, err)
		}
	}()

	// Re-check the cache after acquiring the lock — another process may
	// have downloaded while we were waiting.
	if cached, err := executableCached(path); err != nil {
		return "", err
	} else if cached {
		log.Printf("found cached Terraform executable at %s (after lock)", path)
		return path, nil
	}

	log.Printf("downloading release archive from %s", build.URL)

	zipFile, err := c.downloadReleaseArchive(build)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	if err := verifyChecksum(zipFile, expectedSum, build.URL); err != nil {
		return "", err
	}

	zipReader, err := openZipReader(zipFile)
	if err != nil {
		return "", err
	}

	if err := extractTerraformBinary(zipReader, build.archiveBinaryName(), path); err != nil {
		return "", err
	}
	return path, nil
}

// verifyChecksum reads zipFile fully (re-opening it so the read position
// doesn't disturb later zip parsing) and compares its SHA256 to expected.
func verifyChecksum(zipFile *os.File, expected, url string) error {
	f, err := os.Open(zipFile.Name())
	if err != nil {
		return fmt.Errorf("could not open zip archive: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("could not compute sha256 for zip archive: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if expected != actual {
		return fmt.Errorf("checksum for %s should be %s, got %s", url, expected, actual)
	}
	log.Printf("checksum match")
	return nil
}

func openZipReader(zipFile *os.File) (*zip.Reader, error) {
	// Use the on-disk size rather than response.ContentLength: chunked
	// responses report -1, which would make zip.NewReader fail.
	info, err := zipFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("could not stat zip archive: %w", err)
	}
	r, err := zip.NewReader(zipFile, info.Size())
	if err != nil {
		return nil, fmt.Errorf("could not unzip release archive: %w", err)
	}
	return r, nil
}

// extractTerraformBinary writes the binary named binaryName from the zip
// archive to dest with executable permissions. If the destination cannot
// be made executable, the partial file is removed so the next invocation
// will re-download cleanly instead of forever cache-hitting a non-exec
// file.
func extractTerraformBinary(zipReader *zip.Reader, binaryName, dest string) error {
	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}
		// Delegate the per-entry work so the source close is scoped
		// to its own function — `defer` inside the for loop would
		// stack closes until extractTerraformBinary returns, and
		// holding the zip entry open across atomic.WriteFile + Chmod
		// is unnecessary.
		return writeZipEntryAsExecutable(f, dest)
	}
	return errors.New("could not find executable named 'terraform' in release archive")
}

func writeZipEntryAsExecutable(f *zip.File, dest string) error {
	source, err := f.Open()
	if err != nil {
		return fmt.Errorf("could not read binary in release archive: %w", err)
	}
	defer source.Close()

	if err := atomic.WriteFile(dest, source); err != nil {
		return fmt.Errorf("could not write binary to the cache directory: %w", err)
	}
	if err := os.Chmod(dest, 0700); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("could not make binary executable: %w", err)
	}
	return nil
}

func executableCached(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("could not stat Terraform executable: %w", err)
}

func (c *Client) downloadReleaseArchive(build Build) (*os.File, error) {
	request, err := http.NewRequest("GET", build.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request for release archive: %w", err)
	}
	request.Header.Set("Cache-Control", "no-store")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("could not download release archive: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d when downloading release archive", response.StatusCode)
	}

	tmp, err := os.CreateTemp("", filepath.Base(build.URL))
	if err != nil {
		return nil, fmt.Errorf("could not create temporary file for release archive: %w", err)
	}
	if _, err := io.Copy(tmp, response.Body); err != nil {
		return nil, fmt.Errorf("could not copy release archive to temporary file: %w", err)
	}
	return tmp, nil
}

func cachedExecutablePath(binDir string, b Build) string {
	return filepath.Join(binDir, b.executableName())
}

func (b *Build) archiveBinaryName() string {
	if b.OS == "windows" {
		return "terraform.exe"
	}
	return "terraform"
}

func (b *Build) executableName() string {
	extension := ""
	if b.OS == "windows" {
		extension = ".exe"
	}
	return fmt.Sprintf("terraform_%s_%s_%s%s", b.Version.String(), b.OS, b.Arch, extension)
}

func (b *Build) zipFileName() string {
	return filepath.Base(b.URL)
}
