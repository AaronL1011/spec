package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// apiTimeout bounds GitHub REST API calls (per AGENTS.md network defaults).
const apiTimeout = 10 * time.Second

// Asset is a single downloadable file attached to a release.
type Asset struct {
	Name string
	URL  string
}

// Release is the subset of a GitHub release that `spec update` needs.
type Release struct {
	Tag    string
	Assets []Asset
}

// asset returns the asset with the given name, if present.
func (r Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// releaseSource fetches release metadata. It is an interface so the update
// engine can be tested without real network access.
type releaseSource interface {
	Latest(ctx context.Context) (Release, error)
	ByTag(ctx context.Context, tag string) (Release, error)
}

// githubReleases is the live releaseSource backed by the GitHub REST API.
type githubReleases struct {
	owner  string
	repo   string
	token  string
	client *http.Client
}

func newGitHubReleases(owner, repo, token string) *githubReleases {
	return &githubReleases{
		owner:  owner,
		repo:   repo,
		token:  token,
		client: &http.Client{Timeout: apiTimeout},
	}
}

func (g *githubReleases) Latest(ctx context.Context) (Release, error) {
	return g.fetch(ctx, "releases/latest")
}

func (g *githubReleases) ByTag(ctx context.Context, tag string) (Release, error) {
	return g.fetch(ctx, "releases/tags/"+tag)
}

// fetch retrieves and decodes a release from a repo-relative API path.
func (g *githubReleases) fetch(ctx context.Context, path string) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/%s", g.owner, g.repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("building release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetching release: %w — check your network connection", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, fmt.Errorf("no matching release found for %s/%s", g.owner, g.repo)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, fmt.Errorf("GitHub API returned %s: %s", resp.Status, string(body))
	}
	return decodeRelease(resp.Body)
}

// decodeRelease parses the GitHub release JSON payload into a Release.
func decodeRelease(r io.Reader) (Release, error) {
	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decoding release payload: %w", err)
	}
	rel := Release{Tag: payload.TagName}
	for _, a := range payload.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return rel, nil
}
