package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// defaultGitHubAPIBase is the public GitHub REST API root. Tests override the
// unexported baseURL field to point at an httptest server.
const defaultGitHubAPIBase = "https://api.github.com"

// SecurityClient implements adapter.SecurityAdapter over the GitHub Dependabot
// alerts REST API. Read-only.
type SecurityClient struct {
	httpClient *http.Client
	baseURL    string
	owner      string   // org (org scope) or repository owner (repo scope)
	repos      []string // repositories for repo scope; empty for org scope
	scope      string   // "org" or "repo"
}

// NewSecurityClient builds a Dependabot-alerts client. scope is "org" (default,
// one org-wide call) or "repo" (one call per repository in repos). Org scope
// needs a token authorised for the organization (owner/security-manager); repo
// scope works with a token that has Dependabot-alert read on those repos. The
// token needs the security_events scope on a classic PAT, or fine-grained
// Dependabot-alerts read.
func NewSecurityClient(token, owner string, repos []string, scope string) *SecurityClient {
	var httpClient *http.Client
	if token != "" {
		httpClient = &http.Client{
			Transport: &tokenTransport{token: token},
			Timeout:   10 * time.Second,
		}
	} else {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if scope != "repo" {
		scope = "org"
	}
	return &SecurityClient{
		httpClient: httpClient,
		baseURL:    defaultGitHubAPIBase,
		owner:      owner,
		repos:      repos,
		scope:      scope,
	}
}

// Alerts returns open Dependabot alerts for the configured scope. Repo scope
// queries each configured repository and concatenates the results; org scope
// makes a single org-wide call.
func (c *SecurityClient) Alerts(ctx context.Context) ([]adapter.SecurityAlert, error) {
	if c.scope == "repo" {
		if len(c.repos) == 0 {
			return nil, fmt.Errorf("github security: repo scope requires at least one repository")
		}
		var out []adapter.SecurityAlert
		for _, repo := range c.repos {
			path := fmt.Sprintf("/repos/%s/%s/dependabot/alerts", c.owner, repo)
			alerts, err := c.fetch(ctx, path, repo)
			if err != nil {
				return nil, err
			}
			out = append(out, alerts...)
		}
		return out, nil
	}
	return c.fetch(ctx, fmt.Sprintf("/orgs/%s/dependabot/alerts", c.owner), "")
}

// fetch pages through one Dependabot-alerts listing endpoint via GitHub's
// Link-header pagination. defaultRepo names the repository for repo-scoped
// responses, which omit the repository object org-scoped responses carry.
func (c *SecurityClient) fetch(ctx context.Context, path, defaultRepo string) ([]adapter.SecurityAlert, error) {
	next := c.baseURL + path + "?state=open&per_page=100"
	var out []adapter.SecurityAlert

	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("listing Dependabot alerts for %s: %w", c.owner, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading Dependabot alerts response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf(
				"listing Dependabot alerts for %s: GitHub returned %s — token needs Dependabot alerts (security_events) read access",
				c.owner, resp.Status,
			)
		}

		var page []ghDependabotAlert
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decoding Dependabot alerts: %w", err)
		}
		for _, a := range page {
			out = append(out, a.toSecurityAlert(defaultRepo))
		}

		next = nextLink(resp.Header.Get("Link"))
	}

	return out, nil
}

// ghDependabotAlert is the subset of the Dependabot alert JSON we consume.
type ghDependabotAlert struct {
	Number     int       `json:"number"`
	State      string    `json:"state"`
	HTMLURL    string    `json:"html_url"`
	CreatedAt  time.Time `json:"created_at"`
	Dependency struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		ManifestPath string `json:"manifest_path"`
	} `json:"dependency"`
	SecurityAdvisory struct {
		GHSAID   string `json:"ghsa_id"`
		CVEID    string `json:"cve_id"`
		Summary  string `json:"summary"`
		Severity string `json:"severity"`
	} `json:"security_advisory"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// toSecurityAlert normalizes a raw alert. defaultRepo is used for repo-scoped
// responses, which omit the repository object that org-scoped responses carry.
func (a ghDependabotAlert) toSecurityAlert(defaultRepo string) adapter.SecurityAlert {
	repo := a.Repository.Name
	if repo == "" {
		repo = defaultRepo
	}
	id := a.SecurityAdvisory.CVEID
	if id == "" {
		id = a.SecurityAdvisory.GHSAID
	}
	return adapter.SecurityAlert{
		Number:     a.Number,
		Title:      a.SecurityAdvisory.Summary,
		Severity:   adapter.NormalizeSeverity(a.SecurityAdvisory.Severity),
		Package:    a.Dependency.Package.Name,
		Manifest:   a.Dependency.ManifestPath,
		Repo:       repo,
		State:      a.State,
		CreatedAt:  a.CreatedAt,
		URL:        a.HTMLURL,
		Identifier: id,
	}
}

// linkNextRe extracts the rel="next" URL from a GitHub Link header. GitHub's
// Dependabot alerts endpoint paginates by cursor, but the Link header always
// carries the full next-page URL, so following it works regardless.
var linkNextRe = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="next"`)

func nextLink(header string) string {
	if header == "" {
		return ""
	}
	if m := linkNextRe.FindStringSubmatch(header); len(m) == 2 {
		return m[1]
	}
	return ""
}
