// Package jira implements PMAdapter using the Jira REST API v3.
//
// Uses raw HTTP rather than a third-party Jira client library to keep
// dependencies minimal and avoid CGo issues with some Jira packages.
//
// Design goals (see docs/JIRA_HARDENING_PLAN.md):
//   - Idempotent linking: find-or-create by a stable spec-id marker label.
//   - Deterministic status sync: explicit stage->status map, no fuzzy matching.
//   - Bidirectional discovery: epics carry a remote link back to the spec.
//   - Resilience: bounded retry/backoff on transient (429/5xx) errors.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// specIDLabel is the marker label that links a Jira issue back to a spec.
// FindEpic searches on it, making create idempotent.
func specIDLabel(specID string) string { return "spec-id-" + sanitizeLabel(specID) }

// specStepLabel marks a story with its originating build step.
func specStepLabel(stepID string) string { return "spec-step-" + sanitizeLabel(stepID) }

// sanitizeLabel makes a value safe as a Jira label. Jira labels cannot contain
// spaces and reject a range of punctuation; reduce to a conservative
// [A-Za-z0-9._-] alphabet so create and JQL search agree on the exact string.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// Options configures a Jira PM client. Only the first four fields are
// required; the rest tune linking, status sync, and story creation.
type Options struct {
	BaseURL        string
	Email          string
	Token          string
	ProjectKey     string
	BoardID        int
	TeamID         string
	EpicIssueType  string
	StoryIssueType string
	Fields         map[string]string // logical name -> customfield id
	Labels         []string
	Components     []string
	StatusMap      map[string]string // spec stage -> jira status
	Timeout        time.Duration
}

// Client implements adapter.PMAdapter using the Jira REST API.
type Client struct {
	opts Options
	http *http.Client
}

const defaultTimeout = 10 * time.Second

// NewClient creates a Jira PMAdapter from typed options.
func NewClient(opts Options) *Client {
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.EpicIssueType == "" {
		opts.EpicIssueType = "Epic"
	}
	if opts.StoryIssueType == "" {
		opts.StoryIssueType = "Story"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{opts: opts, http: &http.Client{Timeout: timeout}}
}

// FindEpic returns the key of the epic already linked to the spec, or "".
// It searches by the spec-id marker label so repeated runs never duplicate.
func (c *Client) FindEpic(ctx context.Context, specID string) (string, error) {
	jql := fmt.Sprintf("project = %q AND labels = %q ORDER BY created ASC",
		c.opts.ProjectKey, specIDLabel(specID))
	keys, err := c.search(ctx, jql, 1)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], nil
}

// CreateEpic creates an Epic in Jira linked to the spec and returns its key.
// It applies the spec-id marker label (for idempotent FindEpic), configured
// labels/components/team, the Epic Name field on company-managed projects, and
// best-effort sets a remote link back to the spec.
func (c *Client) CreateEpic(ctx context.Context, spec adapter.SpecMeta) (string, error) {
	fields := c.baseFields(spec.ID, c.opts.EpicIssueType,
		fmt.Sprintf("[%s] %s", spec.ID, spec.Title), epicDescription(spec), spec.Labels)

	// Epic Name is required on classic/company-managed projects.
	if f := c.opts.Fields["epic_name"]; f != "" {
		fields[f] = spec.Title
	}

	key, err := c.createIssue(ctx, fields)
	if err != nil {
		return "", err
	}

	// Best-effort back-link; a failed link must not orphan a created epic.
	if spec.URL != "" {
		if linkErr := c.LinkEpic(ctx, key, spec.ID, spec.URL); linkErr != nil {
			return key, fmt.Errorf("epic %s created but back-link failed: %w", key, linkErr)
		}
	}
	return key, nil
}

// LinkEpic records a remote link on the issue pointing back to the spec so
// board consumers can navigate Jira -> spec.
func (c *Client) LinkEpic(ctx context.Context, epicKey, specID, specURL string) error {
	if epicKey == "" || specURL == "" {
		return nil
	}
	payload := map[string]interface{}{
		"globalId": "spec=" + specID,
		"object": map[string]interface{}{
			"url":     specURL,
			"title":   "Spec " + specID,
			"summary": "Canonical spec document",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling remote link: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/3/issue/%s/remotelink", epicKey), data, true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error linking %s (HTTP %d): %s", epicKey, resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

// UpdateStatus transitions the issue to the Jira status mapped from the spec
// stage. Resolution is explicit (via the status map) and idempotent: an
// already-correct issue is a no-op, and an unmapped stage makes no API call.
func (c *Client) UpdateStatus(ctx context.Context, epicKey string, stage string) error {
	if epicKey == "" {
		return nil // no linked epic — skip silently
	}
	target, ok := c.mappedStatus(stage)
	if !ok {
		return nil // stage intentionally not mapped to the board — no-op
	}

	current, err := c.currentStatus(ctx, epicKey)
	if err != nil {
		return err
	}
	if strings.EqualFold(current, target) {
		return nil // already in the target status — idempotent no-op
	}

	transitions, err := c.transitions(ctx, epicKey)
	if err != nil {
		return err
	}
	for _, t := range transitions {
		if strings.EqualFold(t.To.Name, target) || strings.EqualFold(t.Name, target) {
			return c.doTransition(ctx, epicKey, t.ID)
		}
	}

	// No direct transition reaches the target. Report exactly what is missing
	// so an admin can add the workflow transition, rather than guessing a path
	// through unknown intermediate states (which could fire other automation).
	return fmt.Errorf("no Jira transition from %q to %q for %s — available: %s; add the workflow transition or adjust pm.status_map",
		current, target, epicKey, describeTransitions(transitions))
}

// FetchUpdates returns the issue's current status, assignee, and update time.
func (c *Client) FetchUpdates(ctx context.Context, epicKey string) (*adapter.PMUpdate, error) {
	if epicKey == "" {
		return nil, nil
	}
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/3/issue/%s?fields=status,assignee,updated", epicKey), nil, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading issue %s: %w", epicKey, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira API error fetching %s (HTTP %d): %s", epicKey, resp.StatusCode, truncate(string(body), 500))
	}

	var issue issueResponse
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("parsing issue %s: %w", epicKey, err)
	}
	return &adapter.PMUpdate{
		Status:    issue.Fields.Status.Name,
		Assignee:  issue.Fields.Assignee.DisplayName,
		UpdatedAt: parseJiraTime(issue.Fields.Updated),
	}, nil
}

// SyncStories reconciles one Jira story per build step under the epic. Each
// story carries a spec-step marker label so reconciliation is idempotent.
func (c *Client) SyncStories(ctx context.Context, epicKey string, stories []adapter.StorySpec) ([]adapter.StoryLink, error) {
	if epicKey == "" || len(stories) == 0 {
		return nil, nil
	}
	var links []adapter.StoryLink
	for _, s := range stories {
		key, err := c.findStory(ctx, s.StepID)
		if err != nil {
			return links, err
		}
		if key == "" {
			key, err = c.createStory(ctx, epicKey, s)
			if err != nil {
				return links, err
			}
		}
		// Best-effort: move a completed step's story to the mapped done status.
		if strings.EqualFold(s.Status, "complete") {
			if done, ok := c.mappedStatus("done"); ok {
				_ = c.transitionTo(ctx, key, done)
			}
		}
		links = append(links, adapter.StoryLink{StepID: s.StepID, StoryKey: key, Status: s.Status})
	}
	return links, nil
}

// Validate checks credentials and configuration against the live Jira instance:
// authentication, project existence, the configured epic/story issue types,
// and the board id when set. Errors name the specific misconfiguration.
func (c *Client) Validate(ctx context.Context) error {
	// expand=issueTypes guarantees the issueTypes array is populated; it is an
	// expand option on the Get Project resource, not a default field.
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/3/project/%s?expand=issueTypes", c.opts.ProjectKey), nil, true)
	if err != nil {
		return err
	}
	types, err := c.readProject(resp)
	if err != nil {
		return err
	}
	for _, want := range []string{c.opts.EpicIssueType, c.opts.StoryIssueType} {
		if want == "" || containsFold(types, want) {
			continue
		}
		return fmt.Errorf("issue type %q not found in project %s (available: %s) — set pm.epic_issue_type/pm.story_issue_type",
			want, c.opts.ProjectKey, strings.Join(types, ", "))
	}

	if c.opts.BoardID > 0 {
		if err := c.validateBoard(ctx); err != nil {
			return err
		}
	}
	return nil
}

// WorkflowStatuses returns the Jira statuses configured for the project's issue
// types, so `spec config check` can print them to seed pm.status_map. It is an
// optional capability discovered by type assertion, not part of PMAdapter.
func (c *Client) WorkflowStatuses(ctx context.Context) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/3/project/%s/statuses", c.opts.ProjectKey), nil, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading project statuses: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira API error fetching statuses (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	var entries []projectStatuses
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parsing project statuses: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		for _, s := range e.Statuses {
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s.Name)
			}
		}
	}
	return out, nil
}

// --- internal helpers ---

func (c *Client) mappedStatus(stage string) (string, bool) {
	if c.opts.StatusMap == nil {
		return "", false
	}
	if v, ok := c.opts.StatusMap[stage]; ok {
		return v, true
	}
	for k, v := range c.opts.StatusMap {
		if strings.EqualFold(k, stage) {
			return v, true
		}
	}
	return "", false
}

// baseFields builds the common create payload shared by epics and stories.
func (c *Client) baseFields(specID, issueType, summary, description string, extraLabels []string) map[string]interface{} {
	labels := append([]string{specIDLabel(specID)}, c.opts.Labels...)
	labels = append(labels, sanitizeLabels(extraLabels)...)
	fields := map[string]interface{}{
		"project":     map[string]string{"key": c.opts.ProjectKey},
		"summary":     summary,
		"issuetype":   map[string]string{"name": issueType},
		"labels":      dedupe(labels),
		"description": adfDoc(description),
	}
	if len(c.opts.Components) > 0 {
		comps := make([]map[string]string, 0, len(c.opts.Components))
		for _, name := range c.opts.Components {
			comps = append(comps, map[string]string{"name": name})
		}
		fields["components"] = comps
	}
	if f := c.opts.Fields["team"]; f != "" && c.opts.TeamID != "" {
		fields[f] = c.opts.TeamID
	}
	return fields
}

func (c *Client) createStory(ctx context.Context, epicKey string, s adapter.StorySpec) (string, error) {
	summary := s.Description
	if summary == "" {
		summary = "Build step"
	}
	if s.Repo != "" {
		summary = fmt.Sprintf("[%s] %s", s.Repo, summary)
	}
	fields := c.baseFields(s.StepID, c.opts.StoryIssueType, summary, summary, nil)
	// Mark the originating step for idempotent reconciliation.
	fields["labels"] = append(fields["labels"].([]string), specStepLabel(s.StepID))
	// Associate the story with its epic. Company-managed (classic) projects link
	// via the Epic Link custom field; team-managed and modern projects use the
	// parent field. Prefer the configured Epic Link field when present.
	if epicLink := c.opts.Fields["epic_link"]; epicLink != "" {
		fields[epicLink] = epicKey
	} else {
		fields["parent"] = map[string]string{"key": epicKey}
	}
	return c.createIssue(ctx, fields)
}

func (c *Client) findStory(ctx context.Context, stepID string) (string, error) {
	jql := fmt.Sprintf("project = %q AND labels = %q", c.opts.ProjectKey, specStepLabel(stepID))
	keys, err := c.search(ctx, jql, 1)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], nil
}

func (c *Client) createIssue(ctx context.Context, fields map[string]interface{}) (string, error) {
	data, err := json.Marshal(map[string]interface{}{"fields": fields})
	if err != nil {
		return "", fmt.Errorf("marshalling create issue request: %w", err)
	}
	// Create is not idempotent: a 5xx behind a successful create must not be
	// retried here (find-or-create at the call site dedupes across invocations).
	resp, err := c.doRequest(ctx, http.MethodPost, "/rest/api/3/issue", data, false)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading create issue response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", c.createError(resp.StatusCode, body)
	}
	var result createIssueResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing create issue response: %w", err)
	}
	return result.Key, nil
}

// createError turns a non-201 create response into an actionable error,
// naming a missing/unknown custom field when Jira reports one (common on
// company-managed projects that require Epic Name).
func (c *Client) createError(status int, body []byte) error {
	var je jiraErrors
	if err := json.Unmarshal(body, &je); err == nil && len(je.Errors) > 0 {
		var parts []string
		for field, msg := range je.Errors {
			parts = append(parts, fmt.Sprintf("%s: %s", field, msg))
		}
		return fmt.Errorf("jira rejected issue (HTTP %d): %s — check pm.fields custom-field ids", status, strings.Join(parts, "; "))
	}
	return fmt.Errorf("jira API error creating issue (HTTP %d): %s", status, truncate(string(body), 500))
}

func (c *Client) currentStatus(ctx context.Context, key string) (string, error) {
	update, err := c.FetchUpdates(ctx, key)
	if err != nil {
		return "", err
	}
	if update == nil {
		return "", nil
	}
	return update.Status, nil
}

// transitionTo finds and executes a direct transition to the named status,
// idempotently. Used by story sync where a precise error is not warranted.
func (c *Client) transitionTo(ctx context.Context, key, status string) error {
	current, err := c.currentStatus(ctx, key)
	if err != nil {
		return err
	}
	if strings.EqualFold(current, status) {
		return nil
	}
	transitions, err := c.transitions(ctx, key)
	if err != nil {
		return err
	}
	for _, t := range transitions {
		if strings.EqualFold(t.To.Name, status) || strings.EqualFold(t.Name, status) {
			return c.doTransition(ctx, key, t.ID)
		}
	}
	return nil
}

func (c *Client) transitions(ctx context.Context, key string) ([]transition, error) {
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/3/issue/%s/transitions", key), nil, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading transitions for %s: %w", key, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira API error fetching transitions (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	var result transitionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing transitions: %w", err)
	}
	return result.Transitions, nil
}

func (c *Client) doTransition(ctx context.Context, key, transitionID string) error {
	data, err := json.Marshal(transitionRequest{Transition: transitionRef{ID: transitionID}})
	if err != nil {
		return fmt.Errorf("marshalling transition request: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/3/issue/%s/transitions", key), data, false)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error transitioning %s (HTTP %d): %s", key, resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

// search runs a JQL query and returns the matching issue keys. It targets the
// enhanced JQL search endpoint (POST /rest/api/3/search/jql) that replaced the
// deprecated/removed /rest/api/3/search on Jira Cloud, falling back to the old
// path for Jira Server/Data Center instances that lack the enhanced endpoint.
func (c *Client) search(ctx context.Context, jql string, maxResults int) ([]string, error) {
	keys, status, err := c.searchVia(ctx, "/rest/api/3/search/jql", jql, maxResults)
	if status == http.StatusNotFound || status == http.StatusGone {
		keys, _, err = c.searchVia(ctx, "/rest/api/3/search", jql, maxResults)
	}
	return keys, err
}

// searchVia executes a search against a specific endpoint, returning the keys,
// the HTTP status (0 on transport error), and any error. Both the enhanced and
// legacy endpoints accept the same request body and return issues[].key.
func (c *Client) searchVia(ctx context.Context, path, jql string, maxResults int) ([]string, int, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"jql":        jql,
		"maxResults": maxResults,
		"fields":     []string{"key"},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshalling search: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, payload, true)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading search response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("jira API error searching (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	var result searchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parsing search response: %w", err)
	}
	keys := make([]string, 0, len(result.Issues))
	for _, i := range result.Issues {
		keys = append(keys, i.Key)
	}
	return keys, resp.StatusCode, nil
}

func (c *Client) readProject(resp *http.Response) ([]string, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading project response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("jira authentication failed (HTTP %d) — check pm.email and pm.token", resp.StatusCode)
	case http.StatusNotFound:
		return nil, fmt.Errorf("jira project %q not found — check pm.project_key", c.opts.ProjectKey)
	default:
		return nil, fmt.Errorf("jira API error validating project (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	var proj projectResponse
	if err := json.Unmarshal(body, &proj); err != nil {
		return nil, fmt.Errorf("parsing project response: %w", err)
	}
	types := make([]string, 0, len(proj.IssueTypes))
	for _, it := range proj.IssueTypes {
		types = append(types, it.Name)
	}
	return types, nil
}

func (c *Client) validateBoard(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodGet,
		fmt.Sprintf("/rest/agile/1.0/board/%d", c.opts.BoardID), nil, true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("jira board %d not found — check pm.board_id", c.opts.BoardID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error validating board %d (HTTP %d): %s", c.opts.BoardID, resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

// maxRetries bounds transient-failure retries; backoff is linear and short so
// a CLI command never hangs.
const maxRetries = 3

// doRequest issues an authenticated Jira request with bounded retries.
//
// Retry policy is correctness-driven, not blanket: a 429 means the request was
// rejected before processing, so it is always safe to retry (honouring the
// Retry-After header). A 5xx or transport error may have been processed, so it
// is retried only when the operation is idempotent. This prevents duplicate
// issues from retrying a create that actually succeeded behind a 5xx.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte, idempotent bool) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.opts.BaseURL+path, reader)
		if err != nil {
			return nil, fmt.Errorf("creating Jira request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.SetBasicAuth(c.opts.Email, c.opts.Token)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("calling Jira API: %w", err)
			if !idempotent || attempt == maxRetries-1 || !sleepCtx(ctx, backoffDur(attempt)) {
				return nil, lastErr
			}
			continue
		}

		retryable := resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= 500 && resp.StatusCode <= 599 && idempotent)
		if retryable && attempt < maxRetries-1 {
			wait := backoffDur(attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				wait = retryAfter(resp, attempt)
			}
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("jira transient response HTTP %d", resp.StatusCode)
			if !sleepCtx(ctx, wait) {
				return nil, ctx.Err()
			}
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// backoffDur returns the linear backoff delay for a retry attempt.
func backoffDur(attempt int) time.Duration {
	return time.Duration(attempt+1) * 500 * time.Millisecond
}

// retryAfter honours a 429 Retry-After header (delta-seconds), falling back to
// the linear backoff when the header is absent or non-numeric.
func retryAfter(resp *http.Response, attempt int) time.Duration {
	if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return backoffDur(attempt)
}

// sleepCtx waits for d or until ctx is cancelled, returning false on cancel.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// parseJiraTime tolerates the common Jira timestamp formats.
func parseJiraTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z0700",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func epicDescription(spec adapter.SpecMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Spec: %s\nStatus: %s", spec.ID, spec.Status)
	if spec.Cycle != "" {
		fmt.Fprintf(&b, "\nCycle: %s", spec.Cycle)
	}
	if len(spec.Repos) > 0 {
		fmt.Fprintf(&b, "\nRepos: %s", strings.Join(spec.Repos, ", "))
	}
	if spec.Problem != "" {
		fmt.Fprintf(&b, "\n\n%s", spec.Problem)
	}
	if spec.URL != "" {
		fmt.Fprintf(&b, "\n\nSpec document: %s", spec.URL)
	}
	return b.String()
}

func describeTransitions(ts []transition) string {
	if len(ts) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, fmt.Sprintf("%s->%s", t.Name, t.To.Name))
	}
	return strings.Join(parts, ", ")
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func sanitizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l = sanitizeLabel(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// --- API types ---

// adfDoc builds a minimal Atlassian Document Format document from plain text.
// Each non-empty line becomes its own paragraph: ADF text nodes must be
// non-empty (Jira rejects empty text nodes) and embedded "\n" inside a single
// text node does not render as a line break, so splitting into paragraphs is
// both valid and readable.
func adfDoc(text string) *adfDocument {
	doc := &adfDocument{Type: "doc", Version: 1}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		doc.Content = append(doc.Content, adfBlock{
			Type:    "paragraph",
			Content: []adfInline{{Type: "text", Text: line}},
		})
	}
	if len(doc.Content) == 0 {
		doc.Content = []adfBlock{{Type: "paragraph", Content: []adfInline{{Type: "text", Text: "—"}}}}
	}
	return doc
}

type adfDocument struct {
	Type    string     `json:"type"`
	Version int        `json:"version"`
	Content []adfBlock `json:"content"`
}

type adfBlock struct {
	Type    string      `json:"type"`
	Content []adfInline `json:"content,omitempty"`
}

type adfInline struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type createIssueResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

type transitionRequest struct {
	Transition transitionRef `json:"transition"`
}

type transitionRef struct {
	ID string `json:"id"`
}

type transitionsResponse struct {
	Transitions []transition `json:"transitions"`
}

type transition struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	To   transitionTo `json:"to"`
}

type transitionTo struct {
	Name string `json:"name"`
}

type issueResponse struct {
	Key    string `json:"key"`
	Fields struct {
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Updated string `json:"updated"`
	} `json:"fields"`
}

type searchResponse struct {
	Issues []struct {
		Key string `json:"key"`
	} `json:"issues"`
}

type projectResponse struct {
	Key        string `json:"key"`
	IssueTypes []struct {
		Name string `json:"name"`
	} `json:"issueTypes"`
}

type projectStatuses struct {
	Name     string `json:"name"`
	Statuses []struct {
		Name string `json:"name"`
	} `json:"statuses"`
}

type jiraErrors struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors"`
}
