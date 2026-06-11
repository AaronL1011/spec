package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gh "github.com/google/go-github/v62/github"
)

// route maps a method+path-substring to a canned response. The harness matches
// the first route whose substring is contained in the request path.
type route struct {
	method   string
	contains string
	status   int
	// file is a testdata filename; body wins when non-empty.
	file   string
	body   string
	header map[string]string
}

// fixtureServer spins up an httptest.Server replaying golden fixtures and
// returns a RepoClient and DeployClient wired to it. Every request that does
// not match a route fails the test loudly, so unexpected calls never silently
// pass.
func fixtureServer(t *testing.T, routes []route) (*RepoClient, *DeployClient) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range routes {
			if rt.method != "" && rt.method != r.Method {
				continue
			}
			if !strings.Contains(r.URL.Path, rt.contains) {
				continue
			}
			for k, v := range rt.header {
				w.Header().Set(k, v)
			}
			status := rt.status
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if rt.body != "" {
				_, _ = w.Write([]byte(rt.body))
				return
			}
			if rt.file != "" {
				_, _ = w.Write(readFixture(t, rt.file))
				return
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotImplemented)
	}))
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL + "/")
	repo := NewRepoClient("test-token", "o")
	repo.client.BaseURL = base
	dep := NewDeployClient("test-token", "o", "deploy.yml")
	dep.client.BaseURL = base
	return repo, dep
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestListPRs_FiltersBySpecBranch(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/repos/o/auth-service/pulls", file: "pulls_list.json"},
	})

	prs, err := repo.ListPRs(context.Background(), []string{"auth-service"}, "SPEC-042")
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1 (only the spec-042 branch)", len(prs))
	}
	if prs[0].Number != 101 || prs[0].Branch != "spec-042/step-1-limiter" {
		t.Errorf("PR = %+v, want #101 on spec-042/step-1-limiter", prs[0])
	}
}

func TestPRStatus_AggregatesReviewsCommentsCI(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/repos/o/auth-service/pulls/101/reviews", file: "pull_reviews.json"},
		{method: http.MethodGet, contains: "/repos/o/auth-service/pulls/101/comments", file: "pull_comments.json"},
		{method: http.MethodGet, contains: "/repos/o/auth-service/pulls/101", file: "pull_get.json"},
		{method: http.MethodGet, contains: "/repos/o/auth-service/commits/abc123/status", file: "combined_status.json"},
	})

	detail, err := repo.PRStatus(context.Background(), "auth-service", 101)
	if err != nil {
		t.Fatalf("PRStatus: %v", err)
	}
	if !detail.Approved {
		t.Error("expected Approved=true (dave approved)")
	}
	if detail.CIStatus != "passing" {
		t.Errorf("CIStatus = %q, want passing", detail.CIStatus)
	}
	if detail.ReviewComments != 2 {
		t.Errorf("ReviewComments = %d, want 2", detail.ReviewComments)
	}
	// carol requested changes with no later approval → 1 unresolved.
	if detail.UnresolvedThreads != 1 {
		t.Errorf("UnresolvedThreads = %d, want 1", detail.UnresolvedThreads)
	}
}

func TestRequestedReviews_ParsesSearchResults(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/search/issues", file: "search_issues.json"},
	})

	prs, err := repo.RequestedReviews(context.Background(), "alice")
	if err != nil {
		t.Fatalf("RequestedReviews: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	if prs[0].Repo != "auth-service" || prs[0].Number != 101 {
		t.Errorf("PR = %+v, want auth-service #101", prs[0])
	}
}

func TestSetPRDescription_PatchesBody(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodPatch, contains: "/repos/o/auth-service/pulls/101", body: `{"number":101}`},
	})

	if err := repo.SetPRDescription(context.Background(), "auth-service", 101, "new body"); err != nil {
		t.Fatalf("SetPRDescription: %v", err)
	}
}

func TestListPRs_RateLimited_ReturnsActionableError(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{
			method:   http.MethodGet,
			contains: "/repos/o/auth-service/pulls",
			status:   http.StatusForbidden,
			body:     `{"message":"API rate limit exceeded","documentation_url":"https://docs.github.com/rest"}`,
			header: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     strconv.FormatInt(2000000000, 10),
				"Retry-After":           "60",
			},
		},
	})

	_, err := repo.ListPRs(context.Background(), []string{"auth-service"}, "SPEC-042")
	if err == nil {
		t.Fatal("expected an error on rate limit, got nil")
	}
	if !strings.Contains(err.Error(), "Pull Requests read access") {
		t.Errorf("error = %q, want the actionable next-step suffix", err)
	}
}

func TestPRStatus_ServerError_ReturnsActionableError(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodGet, contains: "/repos/o/auth-service/pulls/101", status: http.StatusInternalServerError, body: `{"message":"boom"}`},
	})

	_, err := repo.PRStatus(context.Background(), "auth-service", 101)
	if err == nil {
		t.Fatal("expected an error on 5xx, got nil")
	}
	if !strings.Contains(err.Error(), "PR #101") {
		t.Errorf("error = %q, want it to name the PR", err)
	}
}

func TestOpenDraftPR_Golden(t *testing.T) {
	repo, _ := fixtureServer(t, []route{
		{method: http.MethodPost, contains: "/repos/o/auth-service/pulls", status: http.StatusCreated,
			body: `{"number":77,"html_url":"https://github.com/o/auth-service/pull/77"}`},
	})

	num, url, err := repo.OpenDraftPR(context.Background(), "auth-service", "spec-042/step-2", "spec-042/step-1", "T", "B")
	if err != nil {
		t.Fatalf("OpenDraftPR: %v", err)
	}
	if num != 77 || !strings.HasSuffix(url, "/pull/77") {
		t.Errorf("got (%d, %q), want (77, .../pull/77)", num, url)
	}
}

// ensure gh import is used even if all helpers inline.
var _ = gh.Client{}
