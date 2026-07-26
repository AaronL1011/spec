package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// The kickoff is the whole reason escalation beats opening a blank session, so
// these pin that invocation context survives into the prompt.

func TestAuthoringKickoff_CarriesInvocationContext(t *testing.T) {
	got := authoringKickoff("SPEC-034", "proposed_solution", "", nil)

	for _, want := range []string{"SPEC-034", "proposed_solution", "spec_section_read", "spec_section_write"} {
		if !strings.Contains(got, want) {
			t.Errorf("kickoff missing %q:\n%s", want, got)
		}
	}
}

func TestAuthoringKickoff_CarriesRejectedDraftAndNotes(t *testing.T) {
	got := authoringKickoff("SPEC-034", "proposed_solution",
		"the rejected body",
		[]string{"weigh the queue option", "cite the benchmark"})

	for _, want := range []string{"the rejected body", "weigh the queue option", "cite the benchmark"} {
		if !strings.Contains(got, want) {
			t.Errorf("kickoff missing %q:\n%s", want, got)
		}
	}
	// Repeating a rejected draft is the failure mode escalation exists to avoid.
	if !strings.Contains(got, "Do not simply repeat it") {
		t.Errorf("kickoff should warn against repeating the rejected draft:\n%s", got)
	}
}

// Without a prior draft the prompt must not imply one was rejected.
func TestAuthoringKickoff_NoPhantomRejection(t *testing.T) {
	got := authoringKickoff("SPEC-001", "problem_statement", "   ", nil)
	if strings.Contains(got, "rejected") {
		t.Errorf("a fresh session should not mention a rejection:\n%s", got)
	}
}

// A section-less invocation is valid (the whole spec), so the prompt must still
// be coherent.
func TestAuthoringKickoff_WithoutSection(t *testing.T) {
	got := authoringKickoff("SPEC-001", "", "", nil)
	if strings.Contains(got, "§") {
		t.Errorf("no section was given, so none should be named:\n%s", got)
	}
	if !strings.Contains(got, "SPEC-001") {
		t.Errorf("the spec should still be named:\n%s", got)
	}
}

func TestAuthoringSystemPrompt_RequiresPortAndHash(t *testing.T) {
	rc := &config.ResolvedConfig{User: &config.UserConfig{}, Team: &config.TeamConfig{}}
	got := authoringSystemPrompt(rc)

	for _, want := range []string{
		"spec_section_write",
		// The hash instruction is what stops an agent clobbering a concurrent
		// human edit, so it is not optional advice.
		"base_hash",
		"never force it",
		"never by editing markdown files directly",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt missing %q:\n%s", want, got)
		}
	}
}

// When transitions are off the tools are absent from tools/list, so the prompt
// must say why rather than let the agent burn turns hunting for them.
func TestAuthoringSystemPrompt_ExplainsMissingTransitions(t *testing.T) {
	rc := &config.ResolvedConfig{User: &config.UserConfig{}, Team: &config.TeamConfig{}}
	got := authoringSystemPrompt(rc)
	if !strings.Contains(got, "cannot advance or revert") {
		t.Errorf("prompt should explain the absent transition tools:\n%s", got)
	}

	// With transitions granted, the restriction must not be stated.
	rc.User.Preferences.AgentAuthoring = config.AgentAuthoringConfig{Transitions: true}
	if got := authoringSystemPrompt(rc); strings.Contains(got, "cannot advance or revert") {
		t.Errorf("transitions are allowed, so the restriction should be absent:\n%s", got)
	}
}

// The session config points at the authoring port in generic mode: an authoring
// session reads across specs, unlike a build session pinned to one DAG.
func TestWriteAuthoringMCPConfig(t *testing.T) {
	path, cleanup, err := writeAuthoringMCPConfig()
	if err != nil {
		t.Fatalf("writeAuthoringMCPConfig: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}

	srv, ok := cfg.MCPServers["spec"]
	if !ok {
		t.Fatal("config should register a 'spec' server")
	}
	if len(srv.Args) == 0 || srv.Args[0] != "mcp-server" {
		t.Errorf("args = %v, want mcp-server", srv.Args)
	}
	for _, arg := range srv.Args {
		if arg == "--spec" {
			t.Error("an authoring session should not be pinned to one spec")
		}
	}

	// The config may carry nothing secret, but it lives in a temp dir shared with
	// other users on some systems.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perms = %o, want 0600", perm)
	}

	// Cleanup must actually remove it: these accumulate once per session.
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup should remove the ephemeral config")
	}
}
