// Package github implements RepoAdapter and DeployAdapter using the GitHub API.
package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	gh "github.com/google/go-github/v62/github"
)

// RepoClient implements adapter.RepoAdapter using the GitHub REST API.
type RepoClient struct {
	client *gh.Client
	owner  string
}

// NewRepoClient creates a GitHub RepoAdapter from a token and org/owner name.
func NewRepoClient(token string, owner string) *RepoClient {
	var httpClient *http.Client
	if token != "" {
		httpClient = &http.Client{
			Transport: &tokenTransport{token: token},
			Timeout:   10 * time.Second,
		}
	} else {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	client := gh.NewClient(httpClient)
	return &RepoClient{client: client, owner: owner}
}

// ListPRs returns open PRs matching a spec's branch pattern across its repos.
// It searches for branches prefixed with "spec-<id>/" (lowercased, stripped of the SPEC- prefix).
func (r *RepoClient) ListPRs(ctx context.Context, repos []string, specID string) ([]adapter.PullRequest, error) {
	branchPrefix := specBranchPrefix(specID)
	var result []adapter.PullRequest

	for _, repo := range repos {
		prs, _, err := r.client.PullRequests.List(ctx, r.owner, repo, &gh.PullRequestListOptions{
			State:     "open",
			Sort:      "created",
			Direction: "desc",
			ListOptions: gh.ListOptions{
				PerPage: 100,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("listing PRs for %s/%s: %w — token needs Pull Requests read access", r.owner, repo, err)
		}
		for _, pr := range prs {
			branch := pr.GetHead().GetRef()
			if strings.HasPrefix(branch, branchPrefix) {
				result = append(result, toPullRequest(pr, repo))
			}
		}
	}
	return result, nil
}

// PRStatus returns the review/CI status of a specific PR.
func (r *RepoClient) PRStatus(ctx context.Context, repo string, prNumber int) (*adapter.PRDetail, error) {
	pr, _, err := r.client.PullRequests.Get(ctx, r.owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("getting PR #%d in %s/%s: %w — token needs Pull Requests read access", prNumber, r.owner, repo, err)
	}

	reviews, _, err := r.client.PullRequests.ListReviews(ctx, r.owner, repo, prNumber, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("listing reviews for PR #%d in %s/%s: %w — token needs Pull Requests read access", prNumber, r.owner, repo, err)
	}

	approved := false
	for _, review := range reviews {
		if review.GetState() == "APPROVED" {
			approved = true
			break
		}
	}

	comments, _, err := r.client.PullRequests.ListComments(ctx, r.owner, repo, prNumber, &gh.PullRequestListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("listing comments for PR #%d in %s/%s: %w — token needs Pull Requests read access", prNumber, r.owner, repo, err)
	}

	ciStatus := fetchCIStatus(ctx, r.client, r.owner, repo, pr.GetHead().GetSHA())

	base := toPullRequest(pr, repo)
	base.Approved = approved
	base.CIStatus = ciStatus

	return &adapter.PRDetail{
		PullRequest:       base,
		ReviewComments:    len(comments),
		UnresolvedThreads: countUnresolved(reviews),
	}, nil
}

// SetPRDescription updates a PR's body text.
func (r *RepoClient) SetPRDescription(ctx context.Context, repo string, prNumber int, body string) error {
	_, _, err := r.client.PullRequests.Edit(ctx, r.owner, repo, prNumber, &gh.PullRequest{
		Body: strPtr(body),
	})
	if err != nil {
		return fmt.Errorf("setting description for PR #%d in %s/%s: %w — token needs Pull Requests write access", prNumber, r.owner, repo, err)
	}
	return nil
}

// RequestedReviews returns PRs where the given user is a requested reviewer.
func (r *RepoClient) RequestedReviews(ctx context.Context, user string) ([]adapter.PullRequest, error) {
	query := fmt.Sprintf("is:pr is:open review-requested:%s org:%s", user, r.owner)
	result, _, err := r.client.Search.Issues(ctx, query, &gh.SearchOptions{
		Sort:  "updated",
		Order: "desc",
		ListOptions: gh.ListOptions{
			PerPage: 50,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("searching review requests for %s: %w — token needs repository metadata and Pull Requests read access", user, err)
	}

	var prs []adapter.PullRequest
	for _, issue := range result.Issues {
		if issue.PullRequestLinks == nil {
			continue
		}
		repo := extractRepoFromURL(issue.GetRepositoryURL())
		prs = append(prs, adapter.PullRequest{
			Number:    issue.GetNumber(),
			Title:     issue.GetTitle(),
			Repo:      repo,
			Author:    issue.GetUser().GetLogin(),
			URL:       issue.GetHTMLURL(),
			Status:    "open",
			CreatedAt: issue.GetCreatedAt().Time,
		})
	}
	return prs, nil
}

// specBranchPrefix returns the branch prefix for a spec ID.
// SPEC-042 → "spec-042/", SPEC-1 → "spec-1/".
func specBranchPrefix(specID string) string {
	id := strings.ToLower(strings.TrimPrefix(specID, "SPEC-"))
	return "spec-" + id + "/"
}

func toPullRequest(pr *gh.PullRequest, repo string) adapter.PullRequest {
	return adapter.PullRequest{
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		Repo:      repo,
		Branch:    pr.GetHead().GetRef(),
		Author:    pr.GetUser().GetLogin(),
		URL:       pr.GetHTMLURL(),
		Status:    pr.GetState(),
		CreatedAt: pr.GetCreatedAt().Time,
	}
}

func fetchCIStatus(ctx context.Context, client *gh.Client, owner, repo, sha string) string {
	if sha == "" {
		return "unknown"
	}
	status, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, nil)
	if err != nil {
		return "unknown"
	}
	switch status.GetState() {
	case "success":
		return "passing"
	case "failure", "error":
		return "failing"
	case "pending":
		return "pending"
	default:
		return "unknown"
	}
}

func countUnresolved(reviews []*gh.PullRequestReview) int {
	// GitHub API doesn't directly expose unresolved thread count.
	// We approximate by counting reviews requesting changes that don't have
	// a subsequent approval from the same reviewer.
	changesRequested := make(map[string]bool)
	for _, r := range reviews {
		user := r.GetUser().GetLogin()
		switch r.GetState() {
		case "CHANGES_REQUESTED":
			changesRequested[user] = true
		case "APPROVED":
			delete(changesRequested, user)
		}
	}
	return len(changesRequested)
}

func extractRepoFromURL(repoURL string) string {
	// https://api.github.com/repos/owner/repo → repo
	parts := strings.Split(repoURL, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return repoURL
}

func strPtr(s string) *string { return &s }

// tokenTransport adds an Authorization header to every request.
type tokenTransport struct {
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req2)
}
