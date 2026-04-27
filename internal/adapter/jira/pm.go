// Package jira implements PMAdapter using the Jira REST API v3.
//
// Uses raw HTTP rather than a third-party Jira client library to keep
// dependencies minimal and avoid CGo issues with some Jira packages.
// The API surface we need is small: create issue, update status, fetch updates.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Client implements adapter.PMAdapter using the Jira REST API.
type Client struct {
	baseURL    string // e.g. "https://myorg.atlassian.net"
	projectKey string // e.g. "PLAT"
	email      string // Atlassian account email for basic auth
	token      string // API token
	http       *http.Client
}

// NewClient creates a Jira PMAdapter.
// baseURL is the Jira instance URL (e.g., "https://myorg.atlassian.net").
// projectKey is the Jira project key (e.g., "PLAT").
// email and token are used for basic authentication.
func NewClient(baseURL, projectKey, email, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL:    baseURL,
		projectKey: projectKey,
		email:      email,
		token:      token,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// CreateEpic creates a new Epic in Jira linked to the given spec.
// Returns the issue key (e.g., "PLAT-123").
func (c *Client) CreateEpic(ctx context.Context, spec adapter.SpecMeta) (string, error) {
	payload := createIssueRequest{
		Fields: issueFields{
			Project: projectRef{Key: c.projectKey},
			Summary: fmt.Sprintf("[%s] %s", spec.ID, spec.Title),
			Description: &adfDocument{
				Type:    "doc",
				Version: 1,
				Content: []adfBlock{{
					Type: "paragraph",
					Content: []adfInline{{
						Type: "text",
						Text: fmt.Sprintf("Spec: %s\nStatus: %s", spec.ID, spec.Status),
					}},
				}},
			},
			IssueType: issueTypeRef{Name: "Epic"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshalling create epic request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/rest/api/3/issue", data)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading create epic response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("jira API error creating epic (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result createIssueResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing create epic response: %w", err)
	}

	return result.Key, nil
}

// UpdateStatus transitions the Jira issue to match the spec's pipeline status.
// It finds the matching transition by name and executes it.
func (c *Client) UpdateStatus(ctx context.Context, epicKey string, status string) error {
	if epicKey == "" {
		return nil // no linked epic — skip silently
	}

	// Fetch available transitions
	transitionID, err := c.findTransition(ctx, epicKey, status)
	if err != nil {
		return err
	}
	if transitionID == "" {
		return fmt.Errorf("no Jira transition found for %s to status %q — align pipeline status names with Jira workflow transitions", epicKey, status)
	}

	payload := transitionRequest{
		Transition: transitionRef{ID: transitionID},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling transition request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/rest/api/3/issue/%s/transitions", epicKey), data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error transitioning %s (HTTP %d): %s", epicKey, resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

// FetchUpdates returns status changes from the Jira issue since the last sync.
func (c *Client) FetchUpdates(ctx context.Context, epicKey string) (*adapter.PMUpdate, error) {
	if epicKey == "" {
		return nil, nil
	}

	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/rest/api/3/issue/%s?fields=status,assignee,updated", epicKey), nil)
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

	updatedAt, _ := time.Parse("2006-01-02T15:04:05.000-0700", issue.Fields.Updated)

	return &adapter.PMUpdate{
		Status:    issue.Fields.Status.Name,
		Assignee:  issue.Fields.Assignee.DisplayName,
		UpdatedAt: updatedAt,
	}, nil
}

// findTransition looks for a transition whose name matches the spec pipeline status.
// Jira transition names are workflow-specific; we do a case-insensitive contains match.
func (c *Client) findTransition(ctx context.Context, issueKey, targetStatus string) (string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/rest/api/3/issue/%s/transitions", issueKey), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading transitions for %s: %w", issueKey, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jira API error fetching transitions (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result transitionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing transitions: %w", err)
	}

	target := strings.ToLower(targetStatus)
	for _, t := range result.Transitions {
		name := strings.ToLower(t.Name)
		// Match by exact name, or by the target status being contained in the transition name.
		// E.g., status "build" matches transition "Start Build" or "Build".
		if name == target || strings.Contains(name, target) {
			return t.ID, nil
		}
		// Also match by the destination status name
		toName := strings.ToLower(t.To.Name)
		if toName == target || strings.Contains(toName, target) {
			return t.ID, nil
		}
	}
	return "", nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating Jira request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.email, c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Jira API: %w", err)
	}
	return resp, nil
}

// --- API types ---

type createIssueRequest struct {
	Fields issueFields `json:"fields"`
}

type issueFields struct {
	Project     projectRef   `json:"project"`
	Summary     string       `json:"summary"`
	Description *adfDocument `json:"description,omitempty"`
	IssueType   issueTypeRef `json:"issuetype"`
}

type projectRef struct {
	Key string `json:"key"`
}

type issueTypeRef struct {
	Name string `json:"name"`
}

// adfDocument is a minimal Atlassian Document Format document.
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
