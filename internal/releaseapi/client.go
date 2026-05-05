package releaseapi

import (
	"archive/zip"
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

	"github.com/Masterminds/semver/v3"
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
)

type ReleaseIndex struct {
	Versions map[string]Release `json:"versions"`
}

type Release struct {
	Version *semver.Version `json:"version"`
	Shasums string          `json:"shasums"`
	Builds  []Build         `json:"builds"`
}

type Build struct {
	Version *semver.Version `json:"version"`
	OS      string          `json:"os"`
	Arch    string          `json:"arch"`
	URL     string          `json:"url"`
}

type Client struct {
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
	return &Client{binDir: binDir, httpClient: httpClient}, nil
}

func (c *Client) ListReleases() (ReleaseIndex, error) {
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

func (c *Client) DownloadRelease(r Release, os, arch string) (string, error) {
	var matchingBuild Build
	for _, build := range r.Builds {
		if build.OS == os && build.Arch == arch {
			matchingBuild = build
			break
		}
	}
	if matchingBuild.URL == "" {
		return "", fmt.Errorf("could not find matching build for OS %q and arch %q", os, arch)
	}
	// Mirrors the nil-version skipping done in filterReleases: the cache
	// path uses Build.Version.String(), so a missing version field would
	// otherwise panic later in executableName().
	if matchingBuild.Version == nil {
		matchingBuild.Version = r.Version
	}
	if matchingBuild.Version == nil {
		return "", fmt.Errorf("release for OS %q arch %q has no version", os, arch)
	}

	expectedSum, err := c.expectedChecksumFor(r, &matchingBuild)
	if err != nil {
		return "", err
	}

	return c.downloadBuild(matchingBuild, expectedSum)
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

func (r *Release) ShaSumsURL() string {
	return fmt.Sprintf("%s/%s/%s", releaseRootURL, r.Version, r.Shasums)
}

func (c *Client) downloadBuild(build Build, expectedSum string) (string, error) {
	path := cachedExecutablePath(c.binDir, build)

	if _, err := os.Stat(path); err == nil {
		log.Printf("found cached Terraform executable at %s", path)
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("could not stat Terraform executable: %w", err)
	}

	log.Printf("downloading release archive from %s", build.URL)

	zipFile, zipLength, err := c.downloadReleaseArchive(build)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	f, err := os.Open(zipFile.Name())
	if err != nil {
		return "", fmt.Errorf("could not open zip archive: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("could not compute sha256 for zip archive: %w", err)
	}
	actualSum := hex.EncodeToString(h.Sum(nil))

	if expectedSum != actualSum {
		return "", fmt.Errorf("checksum for %s should be %s, got %s", build.URL, expectedSum, actualSum)
	}
	log.Printf("checksum match")

	zipReader, err := zip.NewReader(zipFile, zipLength)
	if err != nil {
		return "", fmt.Errorf("could not unzip release archive: %w", err)
	}

	binaryName := build.archiveBinaryName()
	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}

		source, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("could not read binary in release archive: %w", err)
		}
		defer source.Close()

		if err := atomic.WriteFile(path, source); err != nil {
			return "", fmt.Errorf("could not write binary to the cache directory: %w", err)
		}
		if err := os.Chmod(path, 0700); err != nil {
			return "", fmt.Errorf("could not make binary executable: %w", err)
		}
		return path, nil
	}

	return "", errors.New("could not find executable named 'terraform' in release archive")
}

func (c *Client) downloadReleaseArchive(build Build) (*os.File, int64, error) {
	request, err := http.NewRequest("GET", build.URL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("could not create request for release archive: %w", err)
	}
	request.Header.Set("Cache-Control", "no-store")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("could not download release archive: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("unexpected status code %d when downloading release archive", response.StatusCode)
	}

	tmp, err := os.CreateTemp("", filepath.Base(build.URL))
	if err != nil {
		return nil, 0, fmt.Errorf("could not create temporary file for release archive: %w", err)
	}
	if _, err := io.Copy(tmp, response.Body); err != nil {
		return nil, 0, fmt.Errorf("could not copy release archive to temporary file: %w", err)
	}
	return tmp, response.ContentLength, nil
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
