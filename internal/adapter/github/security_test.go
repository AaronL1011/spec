package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

const secPage1 = `[
  {
    "number": 1131,
    "state": "open",
    "html_url": "https://github.com/NEXL-LTS/outlook_plugin/security/dependabot/1131",
    "created_at": "2026-06-24T00:00:00Z",
    "dependency": {"package": {"ecosystem": "npm", "name": "http-proxy-middleware"}, "manifest_path": "outlook_plugin/package-lock.json"},
    "security_advisory": {"ghsa_id": "GHSA-xxxx", "cve_id": "CVE-2026-0001", "summary": "http-proxy-middleware routing bypass", "severity": "medium"},
    "repository": {"name": "outlook_plugin", "full_name": "NEXL-LTS/outlook_plugin"}
  }
]`

const secPage2 = `[
  {
    "number": 689,
    "state": "open",
    "html_url": "https://github.com/NEXL-LTS/outlook_plugin/security/dependabot/689",
    "created_at": "2026-02-01T00:00:00Z",
    "dependency": {"package": {"ecosystem": "npm", "name": "webpack-dev-server"}, "manifest_path": "outlook_plugin/package-lock.json"},
    "security_advisory": {"ghsa_id": "GHSA-yyyy", "summary": "webpack-dev-server source theft", "severity": "critical"},
    "repository": {"name": "outlook_plugin", "full_name": "NEXL-LTS/outlook_plugin"}
  }
]`

func TestSecurityClient_Alerts_OrgScopePaginates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("expected state=open query, got %q", r.URL.RawQuery)
		}
		// First page carries a Link header pointing at the cursor'd next page.
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("Link", fmt.Sprintf(
				`<%s/orgs/NEXL-LTS/dependabot/alerts?state=open&per_page=100&after=CURSOR>; rel="next"`, srv.URL))
			_, _ = w.Write([]byte(secPage1))
			return
		}
		_, _ = w.Write([]byte(secPage2))
	}))
	defer srv.Close()

	c := NewSecurityClient("test-token", "NEXL-LTS", nil, "org")
	c.baseURL = srv.URL

	alerts, err := c.Alerts(context.Background())
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2 (pagination should follow Link header)", len(alerts))
	}

	a0 := alerts[0]
	if a0.Number != 1131 {
		t.Errorf("alert[0].Number = %d, want 1131", a0.Number)
	}
	if a0.Severity != adapter.SeverityMedium {
		t.Errorf("alert[0].Severity = %q, want medium (from 'medium')", a0.Severity)
	}
	if a0.Package != "http-proxy-middleware" {
		t.Errorf("alert[0].Package = %q", a0.Package)
	}
	if a0.Repo != "outlook_plugin" {
		t.Errorf("alert[0].Repo = %q, want outlook_plugin", a0.Repo)
	}
	if a0.Identifier != "CVE-2026-0001" {
		t.Errorf("alert[0].Identifier = %q, want CVE-2026-0001", a0.Identifier)
	}
	if a0.Manifest != "outlook_plugin/package-lock.json" {
		t.Errorf("alert[0].Manifest = %q", a0.Manifest)
	}

	a1 := alerts[1]
	if a1.Severity != adapter.SeverityCritical {
		t.Errorf("alert[1].Severity = %q, want critical", a1.Severity)
	}
	// Falls back to GHSA id when cve_id is absent.
	if a1.Identifier != "GHSA-yyyy" {
		t.Errorf("alert[1].Identifier = %q, want GHSA-yyyy (fallback)", a1.Identifier)
	}
}

func TestSecurityClient_Alerts_ForbiddenIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewSecurityClient("bad-token", "NEXL-LTS", nil, "org")
	c.baseURL = srv.URL

	if _, err := c.Alerts(context.Background()); err == nil {
		t.Fatal("expected an error on HTTP 403, got nil")
	}
}

func TestSecurityClient_RepoScopeRequiresRepo(t *testing.T) {
	c := NewSecurityClient("t", "NEXL-LTS", nil, "repo")
	if _, err := c.Alerts(context.Background()); err == nil {
		t.Fatal("expected an error for repo scope with no repositories")
	}
}

func TestSecurityClient_RepoScopeMultipleRepos(t *testing.T) {
	// Each configured repo is queried at its own per-repo endpoint; results
	// concatenate. Repo-scoped responses omit the repository object, so Repo is
	// filled from the requested repo name.
	alertFor := func(number int, pkg string) string {
		return fmt.Sprintf(`[{"number":%d,"state":"open","html_url":"https://github.com/x","created_at":"2026-06-01T00:00:00Z","dependency":{"package":{"ecosystem":"npm","name":%q}},"security_advisory":{"summary":"adv %s","severity":"high"}}]`, number, pkg, pkg)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/repos/NEXL-LTS/code-libs/"):
			_, _ = w.Write([]byte(alertFor(1, "lodash")))
		case strings.Contains(r.URL.Path, "/repos/NEXL-LTS/nexl-ai-agent/"):
			_, _ = w.Write([]byte(alertFor(2, "axios")))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer srv.Close()

	c := NewSecurityClient("t", "NEXL-LTS", []string{"code-libs", "nexl-ai-agent"}, "repo")
	c.baseURL = srv.URL

	alerts, err := c.Alerts(context.Background())
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2 (one per repo)", len(alerts))
	}
	repos := map[string]bool{alerts[0].Repo: true, alerts[1].Repo: true}
	if !repos["code-libs"] || !repos["nexl-ai-agent"] {
		t.Errorf("alerts should carry their source repos, got %v", repos)
	}
}
