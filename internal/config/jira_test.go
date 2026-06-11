package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestProviderConfig_Jira_ParsesFullSchema(t *testing.T) {
	const y = `
provider: jira
base_url: https://myorg.atlassian.net
email: user@example.com
token: api-token
project_key: PLAT
board_id: 42
team_id: team-abc
epic_issue_type: Initiative
story_issue_type: Story
sync_stories: true
request_timeout: 30s
labels:
  - spec-managed
  - platform
components:
  - backend
fields:
  epic_name: customfield_10011
  team: customfield_10001
status_map:
  draft: To Do
  engineering: In Progress
  done: Done
`
	var pc ProviderConfig
	if err := yaml.Unmarshal([]byte(y), &pc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	j := pc.Jira()

	if !j.IsComplete() {
		t.Fatal("expected complete config")
	}
	if j.BoardID != 42 {
		t.Errorf("board_id = %d, want 42", j.BoardID)
	}
	if j.TeamID != "team-abc" {
		t.Errorf("team_id = %q", j.TeamID)
	}
	if j.EpicIssueType != "Initiative" {
		t.Errorf("epic_issue_type = %q", j.EpicIssueType)
	}
	if !j.SyncStories {
		t.Error("expected sync_stories true")
	}
	if j.RequestTimeout != 30*time.Second {
		t.Errorf("request_timeout = %v, want 30s", j.RequestTimeout)
	}
	if j.Field("epic_name") != "customfield_10011" {
		t.Errorf("epic_name field = %q", j.Field("epic_name"))
	}
	if len(j.Labels) != 2 || j.Labels[0] != "spec-managed" {
		t.Errorf("labels = %v", j.Labels)
	}
	if len(j.Components) != 1 || j.Components[0] != "backend" {
		t.Errorf("components = %v", j.Components)
	}
	if got, ok := j.MappedStatus("engineering"); !ok || got != "In Progress" {
		t.Errorf("MappedStatus(engineering) = %q, %v", got, ok)
	}
	if got, ok := j.MappedStatus("Engineering"); !ok || got != "In Progress" {
		t.Errorf("MappedStatus is not case-insensitive: %q, %v", got, ok)
	}
	if _, ok := j.MappedStatus("triage"); ok {
		t.Error("expected triage to be unmapped")
	}
}

func TestProviderConfig_Jira_Defaults(t *testing.T) {
	const y = `
provider: jira
base_url: https://x.atlassian.net
email: u@x.com
token: t
project_key: ENG
`
	var pc ProviderConfig
	if err := yaml.Unmarshal([]byte(y), &pc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	j := pc.Jira()
	if j.EpicIssueType != "Epic" {
		t.Errorf("default epic issue type = %q, want Epic", j.EpicIssueType)
	}
	if j.StoryIssueType != "Story" {
		t.Errorf("default story issue type = %q, want Story", j.StoryIssueType)
	}
	if j.RequestTimeout != defaultPMTimeout {
		t.Errorf("default timeout = %v", j.RequestTimeout)
	}
	if j.SyncStories {
		t.Error("sync_stories should default false")
	}
}

func TestProviderConfig_Jira_IncompleteIsNoop(t *testing.T) {
	const y = `
provider: jira
base_url: https://x.atlassian.net
`
	var pc ProviderConfig
	if err := yaml.Unmarshal([]byte(y), &pc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pc.Jira().IsComplete() {
		t.Error("expected incomplete config")
	}
}
