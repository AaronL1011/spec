package adapter

import "context"

// RepoAdapter manages code repository integration.
type RepoAdapter interface {
	// ListPRs returns open PRs matching a spec's branch pattern across its repos.
	ListPRs(ctx context.Context, repos []string, specID string) ([]PullRequest, error)
	// PRStatus returns the review/CI status of a specific PR.
	PRStatus(ctx context.Context, repo string, prNumber int) (*PRDetail, error)
	// SetPRDescription updates a PR's description.
	SetPRDescription(ctx context.Context, repo string, prNumber int, body string) error
	// RequestedReviews returns PRs where the current user is a requested reviewer.
	RequestedReviews(ctx context.Context, user string) ([]PullRequest, error)
	// OpenDraftPR opens a DRAFT pull request from head into base, returning its
	// number and URL. Draft-only by design: merge stays a human action, so no
	// merge call is exposed on this interface.
	OpenDraftPR(ctx context.Context, repo, head, base, title, body string) (number int, url string, err error)
	// SetPRBase retargets an open PR's base branch, used to re-chain a stack as
	// parent PRs merge.
	SetPRBase(ctx context.Context, repo string, prNumber int, base string) error
}
