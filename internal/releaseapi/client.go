package releaseapi

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/google/renameio"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/pkg/errors"
)

const (
	releasesURL = "https://releases.hashicorp.com/terraform/index.json"
)

type ReleaseIndex struct {
	Versions map[string]Release `json:"versions"`
}

type Release struct {
	Version *semver.Version `json:"version"`
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

	return c.downloadBuild(matchingBuild)
}

func (c *Client) downloadBuild(build Build) (string, error) {
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

	zipReader, err := zip.NewReader(zipFile, zipLength)

	if err != nil {
		return "", errors.Wrap(err, "could not unzip release archive")
	}

	destination, err := renameio.TempFile("", path)

	if err != nil {
		return "", errors.Wrap(err, "could not create temporary file for executable")
	}

	defer destination.Close()

	if err := destination.Chmod(0700); err != nil {
		return "", errors.Wrap(err, "could not make temporary file executable")
	}

	var found bool

	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != "terraform" {
			continue
		}

		source, err := f.Open()

		if err != nil {
			return "", errors.Wrap(err, "could not read executable in release archive")
		}

		defer source.Close()

		if _, err := io.Copy(destination, source); err != nil {
			return "", errors.Wrap(err, "could not copy executable to temporary file")
		}

		if err := destination.CloseAtomicallyReplace(); err != nil {
			return "", errors.Wrap(err, "could not move executable to destination")
		}

		found = true
	}

	if !found {
		return "", errors.New("could not find executable named 'terraform' in release archive")
	}

	return path, nil
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
	return filepath.Join(cacheDir, executableName(b))
}

func executableName(b Build) string {
	return fmt.Sprintf("terraform_%s_%s_%s", b.Version.String(), b.OS, b.Arch)
}
