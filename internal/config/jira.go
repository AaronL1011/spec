package config

import (
	"strconv"
	"strings"
	"time"
)

// Default Jira issue types and request timeout when the team config leaves
// them unset. Epics group a spec; stories track individual build steps.
const (
	defaultEpicIssueType  = "Epic"
	defaultStoryIssueType = "Story"
	defaultPMTimeout      = 10 * time.Second
)

// JiraConfig is the typed Jira PM configuration parsed from the generic
// ProviderConfig. It binds a spec workspace to a specific project, board, and
// team, and carries the custom-field ids and status map the integration needs
// to land issues where the board's analytics expect them.
//
// Every field is explicit: custom-field ids vary per Jira instance and must
// never be guessed (see docs/JIRA_HARDENING_PLAN.md §P1).
type JiraConfig struct {
	BaseURL    string
	Email      string
	Token      string
	ProjectKey string

	// BoardID scopes board analytics; 0 means unset.
	BoardID int
	// TeamID is the Jira Team field value (Advanced Roadmaps / Plans).
	TeamID string

	EpicIssueType  string
	StoryIssueType string

	// Fields maps logical field names (epic_name, team, sprint, story_points)
	// to instance-specific custom-field ids (e.g. customfield_10011).
	Fields map[string]string

	// Labels are applied to every spec-created issue; Components likewise.
	Labels     []string
	Components []string

	// SyncStories opts into creating Jira stories from build steps.
	SyncStories bool

	// StatusMap maps spec pipeline stage names to Jira status names. A stage
	// absent from the map is a no-op for status sync.
	StatusMap map[string]string

	RequestTimeout time.Duration
}

// IsComplete reports whether the minimum fields required to talk to Jira are
// present. Missing required fields degrade the integration to a noop.
func (j JiraConfig) IsComplete() bool {
	return j.BaseURL != "" && j.ProjectKey != "" && j.Email != "" && j.Token != ""
}

// Field returns the configured custom-field id for a logical name, or "".
func (j JiraConfig) Field(name string) string {
	if j.Fields == nil {
		return ""
	}
	return j.Fields[name]
}

// MappedStatus returns the Jira status mapped to a spec stage and whether a
// mapping exists. Lookups are case-insensitive on the stage name.
func (j JiraConfig) MappedStatus(stage string) (string, bool) {
	if j.StatusMap == nil {
		return "", false
	}
	if v, ok := j.StatusMap[stage]; ok {
		return v, true
	}
	lower := strings.ToLower(stage)
	for k, v := range j.StatusMap {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return "", false
}

// Jira parses the generic PM ProviderConfig into a typed JiraConfig. Scalar
// keys come from the flattened Extra map; nested keys (fields, status_map,
// labels, components) are read from the preserved raw YAML so structured
// values survive (Extra stringifies them).
func (p ProviderConfig) Jira() JiraConfig {
	j := JiraConfig{
		BaseURL:        p.Get("base_url"),
		Email:          p.Get("email"),
		Token:          p.Get("token"),
		ProjectKey:     p.Get("project_key"),
		TeamID:         p.Get("team_id"),
		EpicIssueType:  p.Get("epic_issue_type"),
		StoryIssueType: p.Get("story_issue_type"),
		Fields:         rawStringMap(p.raw, "fields"),
		Labels:         rawStringSlice(p.raw, "labels"),
		Components:     rawStringSlice(p.raw, "components"),
		StatusMap:      rawStringMap(p.raw, "status_map"),
		SyncStories:    parseBool(p.Get("sync_stories")),
		RequestTimeout: defaultPMTimeout,
	}
	if j.EpicIssueType == "" {
		j.EpicIssueType = defaultEpicIssueType
	}
	if j.StoryIssueType == "" {
		j.StoryIssueType = defaultStoryIssueType
	}
	if v := p.Get("board_id"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			j.BoardID = n
		}
	}
	if v := p.Get("request_timeout"); v != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			j.RequestTimeout = d
		}
	}
	return j
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1", "on":
		return true
	default:
		return false
	}
}

// rawStringMap coerces raw[key] into a map[string]string, tolerating the
// map[string]interface{} that yaml.v3 produces. Returns nil when absent.
func rawStringMap(raw map[string]interface{}, key string) map[string]string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = stringify(val)
	}
	return out
}

// rawStringSlice coerces raw[key] into a []string. Returns nil when absent.
func rawStringSlice(raw map[string]interface{}, key string) []string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, stringify(item))
	}
	return out
}

func stringify(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(stringifyNonString(v))
}

func stringifyNonString(v interface{}) string {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(n)
	default:
		return ""
	}
}
