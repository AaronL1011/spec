package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// `spec draft <id> --interactive` runs a conversational authoring session against
// the MCP authoring port, so drafting has two depths rather than two features:
// one-shot for a section that fits a prompt, a session for one that does not.
//
// The kickoff prompt carries the invocation context instead of discarding it. The
// first thing a user typed at a blank session prompt in every early design was
// "work on §4" — something spec already knew from the flag or the TUI cursor.

// runInteractiveDraft launches an authoring session for a spec.
func runInteractiveDraft(rc *config.ResolvedConfig, specID, section, kickoff string) error {
	reg := buildRegistry(rc)
	agent := reg.Agent()
	caps := agent.Capabilities()

	// MCP is what makes an authoring session useful: without it the agent has no
	// tools and would be writing markdown blind, so falling back to one-shot is
	// better than launching something that cannot do the job.
	if !caps.MCP {
		provider := rc.EffectiveAgentConfig().Provider
		if provider == "" {
			provider = "none"
		}
		fmt.Printf("%s does not support MCP sessions — falling back to one-shot drafting.\n", provider)
		if section == "" {
			return fmt.Errorf("one-shot drafting needs a target: rerun with --section <slug>")
		}
		svc := newLLMService(rc)
		if !svc.IsAvailable() {
			return fmt.Errorf("%s supports neither sessions nor completions — configure a different agent in ~/.spec/config.yaml", provider)
		}
		return draftSectionCmd(rc, svc, specID, section, false)
	}

	specPath, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}
	if _, err := markdown.ReadMeta(specPath); err != nil {
		return fmt.Errorf("reading %s: %w", specID, err)
	}

	// The MCP config is ephemeral and scoped to this spec, mirroring how the
	// build engine provisions a session.
	mcpPath, cleanup, err := writeAuthoringMCPConfig()
	if err != nil {
		return err
	}
	defer cleanup()

	if kickoff == "" {
		kickoff = authoringKickoff(specID, section, "", nil)
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	req := adapter.InvokeRequest{
		SpecID:        specID,
		WorkDir:       workDir,
		MCPConfigPath: mcpPath,
		SystemPrompt:  authoringSystemPrompt(rc),
		Prompt:        kickoff,
	}

	fmt.Printf("Launching %s for %s", rc.EffectiveAgentConfig().Provider, specID)
	if section != "" {
		fmt.Printf(" (§%s)", section)
	}
	fmt.Println(" — writes go through spec's engines and gates.")

	if _, err := agent.Invoke(context.Background(), req); err != nil {
		if errors.Is(err, adapter.ErrNotSupported) {
			return fmt.Errorf("the configured agent does not support interactive sessions")
		}
		return fmt.Errorf("agent session: %w", err)
	}
	return nil
}

// writeAuthoringMCPConfig emits an ephemeral MCP config pointing at the
// authoring port for one spec, and returns a cleanup that removes it.
func writeAuthoringMCPConfig() (path string, cleanup func(), err error) {
	bin := "spec"
	if exe, e := os.Executable(); e == nil && exe != "" {
		bin = exe
	}

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"spec": map[string]any{
				"command": bin,
				// Generic mode, not --spec: an authoring session reads and writes
				// across specs (a decision here, a reference there), unlike a
				// build session pinned to one DAG.
				"args": []string{"mcp-server"},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", func() {}, fmt.Errorf("marshalling mcp config: %w", err)
	}

	dir, err := os.MkdirTemp("", "spec-authoring-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating mcp config dir: %w", err)
	}
	path = filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, fmt.Errorf("writing mcp config: %w", err)
	}
	return path, func() { _ = os.RemoveAll(dir) }, nil
}

// authoringSystemPrompt tells the session how to author through the port rather
// than by editing files.
//
// The concurrency instruction is not optional advice: an agent that writes
// without a base_hash can clobber a human edit made while it was thinking, and
// the port's protection only works if the agent uses it.
func authoringSystemPrompt(rc *config.ResolvedConfig) string {
	var b strings.Builder
	b.WriteString("You are helping author a software specification in spec.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Author sections with spec_section_write, never by editing markdown files directly. ")
	b.WriteString("Writes go through spec's markdown engine, so they are validated, formatted, and logged.\n")
	b.WriteString("- Before writing a section, read it with spec_section_read and pass the content_hash you ")
	b.WriteString("get back as base_hash. If the write reports a conflict, re-read and reapply — never force it.\n")
	b.WriteString("- Record trade-offs with spec_decide as you go, so the reasoning survives the session.\n")
	b.WriteString("- Write the section body only: no headings, no meta-commentary.\n")

	if rc != nil && !rc.AgentTransitionsAllowed() {
		// Say so explicitly: the tools are absent from tools/list, and an agent
		// that does not know why will waste turns looking for them.
		b.WriteString("- You cannot advance or revert stages; that is the human's call. ")
		b.WriteString("Say when you think a spec is ready rather than trying to move it.\n")
	}
	return b.String()
}

// authoringKickoff builds the opening prompt for a session.
//
// It mirrors the TUI's escalation kickoff so a session started from either
// surface begins with the same context.
func authoringKickoff(specID, section, rejected string, notes []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on %s", specID)
	if section != "" {
		fmt.Fprintf(&b, ", section §%s", section)
	}
	b.WriteString(".\n\n")
	b.WriteString("Read the spec through the MCP tools first, then draft. ")
	b.WriteString("Use spec_section_read to get a content_hash and pass it to spec_section_write.\n")

	if strings.TrimSpace(rejected) != "" {
		b.WriteString("\nA one-shot draft was rejected. Do not simply repeat it:\n\n")
		b.WriteString(rejected)
		b.WriteString("\n")
	}
	if len(notes) > 0 {
		b.WriteString("\nReviewer feedback on that draft:\n")
		for i, n := range notes {
			fmt.Fprintf(&b, "%d. %s\n", i+1, n)
		}
	}
	return b.String()
}
