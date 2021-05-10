package releaseapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/pkg/errors"
)

const (
	releaseCacheTTL = 1 * time.Hour
	releasesURL     = "https://releases.hashicorp.com/terraform/index.json"
)

type ListReleasesResponse struct {
	ETag     string    `json:"etag"`
	Releases []Release `json:"releases"`
}

type Release struct {
	Version *version.Version `json:"version"`
	Builds  []Build          `json:"builds"`
}

func (r *Release) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version string
		Builds  []Build
	}{r.Version.String(), r.Builds})
}

func (r *Release) UnmarshalJSON(value []byte) error {
	raw := struct {
		Version string
		Builds  []Build
	}{}

	if err := json.Unmarshal(value, &raw); err != nil {
		return err
	}

	parsedVersion, err := version.NewVersion(raw.Version)

	if err != nil {
		return errors.Wrap(err, "could not parse version from string")
	}

	r.Version = parsedVersion
	r.Builds = raw.Builds

	return nil
}

type Build struct {
	VersionString string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	URL           string `json:"url"`
}

type ReleaseList []Release

func (l ReleaseList) Len() int           { return len(l) }
func (l ReleaseList) Less(i, j int) bool { return l[i].Version.LessThan(l[j].Version) }
func (l ReleaseList) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

type Client struct {
	httpclient *http.Client
	cacheDir   string
}

func NewClient(httpclient *http.Client, cacheDir string) *Client {
	return &Client{httpclient, cacheDir}
}

func (c *Client) ListReleases(ctx context.Context) (ListReleasesResponse, error) {
	releases, isFresh, err := c.getCachedReleaseList()

	if err != nil {
		return releases, err
	} else if isFresh {
		log.Printf("using cached release list")

		return releases, nil
	}

	req, err := http.NewRequest("GET", releasesURL, nil)

	if err != nil {
		return releases, errors.Wrap(err, "could not create request")
	}

	if releases.ETag != "" {
		req.Header.Set(`If-None-Match`, releases.ETag)
	}

	log.Printf("downloading release index from Hashicorp")

	resp, err := c.httpclient.Do(req)

	if err != nil {
		return releases, errors.Wrap(err, "could not send request")
	}

	if resp.StatusCode == http.StatusOK {
		releases, err = c.parseReleaseIndexResponse(resp)

		if err != nil {
			return releases, err
		}
	} else if resp.StatusCode != http.StatusNotModified {
		return releases, errors.Errorf("unexpected status code in response: %v", resp)
	}

	if err := c.saveCachedReleaseList(releases); err != nil {
		return releases, err
	}

	return releases, nil
}

func (c *Client) getCachedReleaseList() (ListReleasesResponse, bool, error) {
	var cachedList ListReleasesResponse

	file, err := os.Open(cachedReleaseListPath(c.cacheDir))

	if err != nil {
		if os.IsNotExist(err) {
			return cachedList, false, nil
		}

		return cachedList, false, errors.Wrap(err, "could not open cached releases file")
	}

	decoder := json.NewDecoder(file)

	if err := decoder.Decode(&cachedList); err != nil {
		return cachedList, false, errors.Wrap(err, "could not parse cached releases file")
	}

	// check freshness

	finfo, err := file.Stat()

	if err != nil {
		return cachedList, false, errors.Wrap(err, "could not stat cached releases file")
	}

	isFresh := time.Now().Sub(finfo.ModTime()) <= releaseCacheTTL

	return cachedList, isFresh, nil
}

func (c *Client) saveCachedReleaseList(resp ListReleasesResponse) error {
	file, err := os.Create(cachedReleaseListPath(c.cacheDir))

	if err != nil {
		return errors.Wrap(err, "could not create cached releases file")
	}

	encoder := json.NewEncoder(file)

	if err := encoder.Encode(resp); err != nil {
		return errors.Wrap(err, "could not write out cached releases file")
	}

	return nil
}

func (c *Client) parseReleaseIndexResponse(resp *http.Response) (ListReleasesResponse, error) {
	defer resp.Body.Close()

	var parsedResponse ListReleasesResponse

	parsedResponse.ETag = resp.Header.Get("ETag")

	expectedBody := struct {
		Versions map[string]struct {
			Builds []Build `json:"builds"`
		} `json:"versions"`
	}{}

	decoder := json.NewDecoder(resp.Body)

	if err := decoder.Decode(&expectedBody); err != nil {
		return parsedResponse, errors.Wrap(err, "could not unmarshal release index JSON")
	}

	for vString, vStruct := range expectedBody.Versions {
		parsedVersion, err := version.NewVersion(vString)

		if err != nil {
			return parsedResponse, errors.Wrap(err, "got invalid version in release index")
		}

		parsedResponse.Releases = append(
			parsedResponse.Releases,
			Release{parsedVersion, vStruct.Builds},
		)
	}

	sort.Sort(sort.Reverse(ReleaseList(parsedResponse.Releases)))

	return parsedResponse, nil
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

func (c *Client) downloadBuild(b Build) (string, error) {
	path := cachedExecutablePath(c.cacheDir, b)

	if _, err := os.Stat(path); err == nil {
		log.Printf("found cached Terraform executable at %s", path)

		return path, nil
	} else if !os.IsNotExist(err) {
		return "", errors.Wrap(err, "could not stat Terraform executable")
	}

	log.Printf("dowloading release archive from %s", b.URL)

	zip, zipCleanupFunc, err := c.downloadZip(b.URL)

	defer zipCleanupFunc()

	if err != nil {
		return "", err
	}

	destination, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0755)

	defer destination.Close()

	if err != nil {
		return "", errors.Wrap(err, "could not create destination file")
	}

	var copied bool

	for _, f := range zip.File {
		if filepath.Base(f.Name) != "terraform" {
			continue
		}

		source, err := f.Open()

		defer source.Close()

		if err != nil {
			return "", errors.Wrap(err, "could not open executable in release archive")
		}

		if _, err := io.Copy(destination, source); err != nil {
			return "", errors.Wrap(err, "could not copy executable to destination")
		}

		copied = true
	}

	if !copied {
		return "", errors.New("could not find executable named 'terraform' in release archive")
	}

	return path, nil
}

func (c *Client) downloadZip(url string) (*zip.Reader, func() error, error) {
	cleanupFunc := func() error { return nil }

	resp, err := c.httpclient.Get(url)

	defer resp.Body.Close()

	if err != nil {
		return nil, cleanupFunc, errors.Wrap(err, "could not download release archive")
	} else if resp.StatusCode != http.StatusOK {
		return nil, cleanupFunc, errors.Errorf("unexpected status code in response: %v", resp)
	}

	tmp, err := ioutil.TempFile("", filepath.Base(url))

	cleanupFunc = tmp.Close

	if err != nil {
		return nil, cleanupFunc, errors.Wrap(err, "could not create temporary file")
	}

	length, err := io.Copy(tmp, resp.Body)

	if err != nil {
		return nil, cleanupFunc, errors.Wrap(err, "could not copy release zip")
	}

	reader, err := zip.NewReader(tmp, length)

	if err != nil {
		return nil, cleanupFunc, errors.Wrap(err, "could not read release archive")
	}

	return reader, cleanupFunc, nil
}

func cachedReleaseListPath(cacheDir string) string {
	return filepath.Join(cacheDir, "terraform-releases.json")
}

func cachedExecutablePath(cacheDir string, b Build) string {
	return filepath.Join(cacheDir, executableName(b))
}

func executableName(b Build) string {
	return fmt.Sprintf("terraform_%s_%s_%s", b.VersionString, b.OS, b.Arch)
}
