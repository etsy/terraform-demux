package releaseapi

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/natefinch/atomic"
	"github.com/pkg/errors"
)

const (
	releasesURL    = "https://releases.hashicorp.com/terraform/index.json"
	releaseRootURL = "https://releases.hashicorp.com/terraform"
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
	cacheDir   string
	httpClient *http.Client
}

func NewClient(cacheDir string) *Client {
	httpClient := httpcache.NewTransport(
		diskcache.New(cacheDir),
	).Client()

	return &Client{cacheDir, httpClient}
}

func (c *Client) ListReleases() (ReleaseIndex, error) {
	var releaseIndex ReleaseIndex

	log.Printf("downloading Terraform release index")

	request, err := http.NewRequest("GET", releasesURL, nil)

	if err != nil {
		return releaseIndex, errors.Wrap(err, "could not create request for Terraform release index")
	}

	response, err := c.httpClient.Do(request)

	if err != nil {
		return releaseIndex, errors.Wrap(err, "could not send request for Terraform release index")
	} else if response.StatusCode != http.StatusOK {
		return releaseIndex, errors.Errorf("error: unexpected status code '%s' in response", response.StatusCode)
	}

	if response.Header.Get(httpcache.XFromCache) != "" {
		log.Printf("using cached response")
	}

	defer response.Body.Close()

	decoder := json.NewDecoder(response.Body)

	if err := decoder.Decode(&releaseIndex); err != nil {
		return releaseIndex, errors.Wrap(err, "could not unmarshal release index JSON")
	}

	return releaseIndex, nil
}

func (c *Client) DownloadRelease(r Release, os, arch string) (string, error) {
	var matchingBuild Build
	var checkSha256Sum string
	for _, build := range r.Builds {
		if build.OS == os && build.Arch == arch {
			matchingBuild = build
			break
		}
	}

	if matchingBuild.URL == "" {
		return "", errors.Errorf(
			"could not find matching build for OS '%s' and arch '%s'", os, arch,
		)
	}

	checkSums, err := c.getReleaseCheckSums(r)
	if err != nil {
		return "", errors.Wrap(err, "could not download checksum file")
	}

	for _, line := range strings.Split(checkSums, "\n") {
		checksum := strings.Split(line, "  ")
		if checksum[1] == matchingBuild.zipFileName() {
			checkSha256Sum = checksum[0]
			break
		}
	}

	build, err := c.downloadBuild(matchingBuild, checkSha256Sum)
	return build, err
}

func (c *Client) getReleaseCheckSums(release Release) (string, error) {
	request, err := http.NewRequest("GET", release.ShaSumsURL(), nil)
	if err != nil {
		return "", errors.Wrap(err, "could not create request for Terraform release checksum")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", errors.Wrap(err, "could not send request for Terraform release checksum")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.Errorf("error: unexpected status code '%s' in response", response.StatusCode)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", errors.Wrap(err, "error: could not read response")
	}
	checkSums := string(bodyBytes)
	return checkSums, nil
}

func (r *Release) ShaSumsURL() string {
	return fmt.Sprintf("%s/%s/%s", releaseRootURL, r.Version, r.Shasums)
}

func (c *Client) downloadBuild(build Build, checkSha256Sum string) (string, error) {
	path := cachedExecutablePath(c.cacheDir, build)

	if _, err := os.Stat(path); err == nil {
		log.Printf("found cached Terraform executable at %s", path)
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", errors.Wrap(err, "could not stat Terraform executable")
	}

	log.Printf("dowloading release archive from %s", build.URL)

	zipFile, zipLength, err := c.downloadReleaseArchive(build)

	if err != nil {
		return "", err
	}

	defer os.Remove(zipFile.Name())
	defer zipFile.Close()

	f, err := os.Open(zipFile.Name())

	if err != nil {
		return "", errors.Wrap(err, "could not open zip archive")
	}

	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.Wrap(err, "could not check sha256sum for zip archive")
	}
	sha256Sum := h.Sum(nil)

	if checkSha256Sum != "" {
		if checkSha256Sum != hex.EncodeToString(sha256Sum) {
			return "", errors.Errorf(
				"checksum for %s should be %s, got %s", build.URL, checkSha256Sum, hex.EncodeToString(sha256Sum),
			)
		} else {
			log.Printf("checksum match\n")
		}
	}

	zipReader, err := zip.NewReader(zipFile, zipLength)

	if err != nil {
		return "", errors.Wrap(err, "could not unzip release archive")
	}

	binaryName := build.archiveBinaryName()
	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}

		source, err := f.Open()

		if err != nil {
			return "", errors.Wrap(err, "could not read binary in release archive")
		}

		defer source.Close()

		if err := atomic.WriteFile(path, source); err != nil {
			return "", errors.Wrap(err, "could not write binary to the cache directory")
		}

		if err := os.Chmod(path, 0700); err != nil {
			return "", errors.Wrap(err, "could not make binary executable")
		}

		return path, nil
	}

	return "", errors.New("could not find executable named 'terraform' in release archive")
}

func (c *Client) downloadReleaseArchive(build Build) (*os.File, int64, error) {
	request, err := http.NewRequest("GET", build.URL, nil)

	if err != nil {
		return nil, 0, errors.Wrap(err, "could not create request for release archive")
	}

	request.Header.Set("Cache-Control", "no-store")

	response, err := c.httpClient.Do(request)

	if err != nil {
		return nil, 0, errors.Wrap(err, "could not download release archive")
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, 0, errors.Errorf("unexpected status code '%s' in response", response.StatusCode)
	}

	tmp, err := ioutil.TempFile("", filepath.Base(build.URL))

	if err != nil {
		return nil, 0, errors.Wrap(err, "could not create temporary file for release archive")
	}

	if _, err := io.Copy(tmp, response.Body); err != nil {
		return nil, 0, errors.Wrap(err, "could not copy release archive to temporary file")
	}
	return tmp, response.ContentLength, nil
}

func cachedExecutablePath(cacheDir string, b Build) string {
	return filepath.Join(cacheDir, b.executableName())
}

func (b *Build) archiveBinaryName() string {
	extension := ""
	if b.OS == "windows" {
		extension = ".exe"
	}
	return "terraform" + extension
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
