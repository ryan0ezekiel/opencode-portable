// Package release talks to the official OpenCode release distribution
// (anomalyco/opencode GitHub releases) to discover the latest version and
// fetch asset metadata including authoritative sha256 digests.
package release

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultRepo is the official OpenCode repository.
const DefaultRepo = "anomalyco/opencode"

// GitHubAPIBase is the GitHub REST API base URL.
const GitHubAPIBase = "https://api.github.com"

// DefaultTimeout bounds API requests.
const DefaultTimeout = 30 * time.Second

// Asset describes a single release asset.
type Asset struct {
	Name string
	Size int64
	// SHA256 is the asset digest when published by the release ("" if the
	// release did not publish one — such assets are treated as unverifiable).
	SHA256 string
	URL    string
}

// Release describes the latest OpenCode release.
type Release struct {
	Tag    string // e.g. "v1.18.18"
	Assets []Asset
}

// Asset returns the asset with the given name, or nil.
func (r *Release) Asset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// URL builds a deterministic download URL for an artifact of a version.
// This is the fallback path used when the API is unreachable but the
// manifest already pins a version.
//
// When OPENCODE_PORTABLE_RELEASE_URL is set (test override), artifacts are
// expected to be served from the same origin under
// /releases/download/<version>/<artifact>, mirroring the GitHub layout.
func URL(repo, version, artifact string) string {
	if base := os.Getenv("OPENCODE_PORTABLE_RELEASE_URL"); base != "" {
		if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host + "/releases/download/" + version + "/" + artifact
		}
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, artifact)
}

type ghAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// FetchLatest queries the GitHub API for the latest release of the given
// repository. The API base can be overridden for testing via the
// OPENCODE_PORTABLE_RELEASE_URL environment variable (an HTTP(S) URL of a
// GitHub API endpoint returning a release JSON object of the same shape).
func FetchLatest(repo string) (*Release, error) {
	url := os.Getenv("OPENCODE_PORTABLE_RELEASE_URL")
	if url == "" {
		url = GitHubAPIBase + "/repos/" + repo + "/releases/latest"
	}

	client := &http.Client{Timeout: DefaultTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OpenCodePortable/1.0 (+portable launcher)")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenCode release service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenCode release service returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("cannot read release metadata: %w", err)
	}

	var gr ghRelease
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, fmt.Errorf("cannot parse release metadata: %w", err)
	}
	if gr.TagName == "" {
		return nil, fmt.Errorf("release metadata missing version")
	}

	rel := &Release{Tag: gr.TagName}
	for _, a := range gr.Assets {
		sha := ""
		if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
			sha = strings.ToLower(strings.TrimSpace(d))
		}
		rel.Assets = append(rel.Assets, Asset{
			Name:   a.Name,
			Size:   a.Size,
			SHA256: sha,
			URL:    a.URL,
		})
	}
	return rel, nil
}
