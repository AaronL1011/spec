package build

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// Engine orchestrates the build handoff. spec-cli owns the DAG, the durable
// node ledger, and all git/worktree/GitHub mechanics; the agent (pi) conducts
// the traversal via MCP. One StartOrResume call = one whole-DAG invocation.
type Engine struct {
	db       *store.DB
	agent    adapter.AgentAdapter
	opts     Options
	strategy BuildStrategy
}

// NewEngine creates a new build engine.
func NewEngine(db *store.DB, agent adapter.AgentAdapter, opts Options) *Engine {
	SetActivityDB(db)
	return &Engine{db: db, agent: agent, opts: opts, strategy: newBuildStrategy(opts)}
}

// StartOrResume begins or continues a build session for a spec. It ensures the
// session and DAG exist, validates the workspaces the DAG needs, assembles the
// context, and invokes the agent once with the DAG exposed via MCP. The agent
// provisions and checkpoints each node back through the spec MCP tools; on
// return the engine reconciles the node ledger and, if work remains, prints
// resume guidance. A resume re-dispatches only the surviving ready nodes —
// completed nodes are never re-run.
func (e *Engine) StartOrResume(ctx context.Context, specID, specPath, startDir string) error {
	session, err := LoadSession(e.db, specID)
	if err != nil {
		return err
	}
	if session == nil {
		session, err = e.createSession(specID, specPath, startDir)
		if err != nil {
			return err
		}
	}
	session.InitNodes()

	graph, err := BuildGraph(session.Steps)
	if err != nil {
		return fmt.Errorf("building DAG for %s: %w", specID, err)
	}

	if err := e.validateWorkspaces(ctx, graph, startDir); err != nil {
		return err
	}

	if session.NodesComplete() {
		e.reportComplete(specID, session, graph)
		return nil
	}

	buildCtx, err := e.assemble(ctx, specPath, session, graph, startDir)
	if err != nil {
		return err
	}

	e.printStatus(specID, specPath, session, graph, startDir)

	req, err := e.provision(specID, buildCtx, startDir)
	if err != nil {
		return err
	}

	_ = LogActivity(specID, "Build session invoked (DAG handoff)")
	if _, err := e.agent.Invoke(ctx, req); err != nil {
		return fmt.Errorf("agent exited with error: %w", err)
	}

	// The agent drove the DAG through the MCP server (a separate process), so
	// reconcile from the persisted ledger rather than any in-process state.
	return e.reconcile(specID)
}

// reconcile reloads the ledger after the agent exits and reports completion or
// prints resume guidance naming the ready and failed nodes.
func (e *Engine) reconcile(specID string) error {
	session, err := LoadSession(e.db, specID)
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}

	graph, err := BuildGraph(session.Steps)
	if err != nil {
		return fmt.Errorf("reconciling DAG for %s: %w", specID, err)
	}

	if session.NodesComplete() {
		_ = LogActivity(specID, "All nodes complete")
		e.reportComplete(specID, session, graph)
		return nil
	}

	done := session.DoneSet()
	ready := graph.ReadySet(done)
	failed := session.FailedNodes()

	fmt.Printf("\nBuild incomplete for %s — %d/%d nodes done.\n", specID, len(done), len(session.Steps))
	if len(failed) > 0 {
		fmt.Printf("Failed nodes: %s\n", strings.Join(failed, ", "))
		for _, id := range failed {
			if n := session.Nodes[id]; n != nil && n.Reason != "" {
				fmt.Printf("  • %s: %s\n", id, n.Reason)
			}
		}
	}
	if len(ready) > 0 {
		fmt.Printf("Ready to dispatch: %s\n", strings.Join(nodeIDs(ready), ", "))
	}
	fmt.Printf("Resume with: spec do %s\n", specID)
	return nil
}

// leafPRStatus reports the draft-PR coverage of the DAG's stack leaves (the
// nodes nothing else depends on — the tips that must carry a PR for review).
// applicable counts leaves that target a repo (and so can carry a PR); missing
// names the applicable leaves with no recorded PR URL. A repo-less fallback node
// is not PR-applicable and is ignored.
func leafPRStatus(session *SessionState, graph *Graph) (applicable, withPR int, missing []string) {
	for _, leaf := range graph.Leaves() {
		if leaf.Repo == "" {
			continue
		}
		applicable++
		if session.node(leaf.NodeID()).PRURL != "" {
			withPR++
		} else {
			missing = append(missing, leaf.NodeID())
		}
	}
	return applicable, withPR, missing
}

// reportComplete prints the terminal build status. Completion is defined by the
// active BuildStrategy, not node status alone: e.g. the default stacked-draft-pr
// strategy only reports done once every stack leaf carries a draft PR, while the
// none strategy is done as soon as all nodes complete.
func (e *Engine) reportComplete(specID string, session *SessionState, graph *Graph) {
	c := e.strategy.Complete(session, graph)
	if c.Done {
		fmt.Printf("✓ Build complete for %s — %s.\n", specID, c.Summary)
		return
	}
	fmt.Printf("Build incomplete for %s — %s\n", specID, c.Summary)
	if c.Hint != "" {
		fmt.Printf("%s, then: spec do %s\n", c.Hint, specID)
	}
}

// Check runs a read-only preflight for a build without launching the agent or
// touching any session: it validates the DAG, resolves each node's workspace and
// routed skills, surfaces skill name collisions, and reports the agent's
// capabilities and the completion definition. It returns an error when the build
// is not launchable so callers (and CI) can gate on it.
func (e *Engine) Check(ctx context.Context, specID, specPath, startDir string) error {
	steps, err := ParsePRStackFromFile(specPath)
	if err != nil {
		return fmt.Errorf("parsing PR stack: %w", err)
	}
	if len(steps) == 0 {
		steps = []PRStep{{Number: 1, ID: "n1", Description: "Build implementation", Status: NodePending}}
	}
	graph, err := BuildGraph(steps)
	if err != nil {
		return err
	}

	fmt.Printf("Build preflight for %s — %s\n", specID, specTitle(specPath))
	caps := e.agent.Capabilities()
	fmt.Printf("Agent capabilities: MCP=%t Skills=%t Headless=%t SystemPrompt=%t\n", caps.MCP, caps.Skills, caps.Headless, caps.SystemPrompt)
	fmt.Printf("Skill router: %s\n", routerName(e.opts.Router))
	fmt.Printf("Build strategy: %s (finishing tools: %s)\n", e.strategy.Name(), finishingToolsLabel(e.strategy))

	if err := e.validateWorkspaces(ctx, graph, startDir); err != nil {
		fmt.Printf("✗ %v\n", err)
		return err
	}

	collisions := e.printCheckWaves(graph, startDir)

	if caps.MCP && caps.Skills {
		cond := skillBasenames(e.conductorSkillPaths(startDir))
		display := strings.Join(cond, ", ")
		if display == "" {
			display = "(none configured — relying on harness skill discovery)"
		}
		fmt.Printf("Conductor skills: %s\n", display)
	}

	for _, name := range collisions {
		fmt.Printf("warning: skill %q is routed from more than one repo — keep per-repo skills in their own workspaces so workers load the right one\n", name)
	}

	fmt.Printf("Completion: defined by the %s strategy.\n", e.strategy.Name())
	fmt.Printf("✓ Launchable — run: spec build %s\n", specID)
	return nil
}

// routerName renders the configured router for display, defaulting empty to the
// shipped default.
func routerName(r string) string {
	if strings.TrimSpace(r) == "" {
		return "registry (default)"
	}
	return r
}

// finishingToolsLabel renders a strategy's finishing tools for display.
func finishingToolsLabel(strategy BuildStrategy) string {
	tools := strategy.FinishingTools()
	if len(tools) == 0 {
		return "none (local-only)"
	}
	return strings.Join(tools, ", ")
}

// printCheckWaves prints the DAG wave-by-wave with each node's workspace and
// routed skills, and returns the sorted set of skill names that resolve to more
// than one path (a cross-repo collision risk).
func (e *Engine) printCheckWaves(graph *Graph, startDir string) []string {
	waves := graph.Waves()
	fmt.Printf("DAG: %d node(s) in %d wave(s)\n", len(graph.Nodes), len(waves))
	router := newSkillRouter(startDir, e.opts)
	byName := make(map[string]map[string]bool)
	for i, wave := range waves {
		fmt.Printf("  Wave %d:\n", i+1)
		for _, n := range wave {
			skills := router.Route(n)
			for _, p := range skills {
				name := filepath.Base(p)
				if byName[name] == nil {
					byName[name] = make(map[string]bool)
				}
				byName[name][p] = true
			}
			repoPath, _ := resolveRepoPath(n.Repo, startDir, e.opts.Workspaces)
			fmt.Printf("    %s [%s] %s\n", n.NodeID(), repoLayer(n), n.Description)
			fmt.Printf("      workDir: %s\n", repoPath)
			if names := skillBasenames(skills); len(names) > 0 {
				fmt.Printf("      skills:  %s\n", strings.Join(names, ", "))
			} else {
				fmt.Println("      skills:  (none routed — relying on conventions)")
			}
		}
	}

	var collided []string
	for name, paths := range byName {
		if len(paths) > 1 {
			collided = append(collided, name)
		}
	}
	sort.Strings(collided)
	return collided
}

// skillBasenames maps skill paths to their directory/file names for display.
func skillBasenames(paths []string) []string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return names
}

// validateWorkspaces checks that every repo referenced by the DAG resolves to a
// real git repo before the build starts. A missing or non-repo workspace is an
// actionable error naming the repo and the config key it needs.
func (e *Engine) validateWorkspaces(ctx context.Context, graph *Graph, startDir string) error {
	seen := make(map[string]bool)
	for _, n := range graph.Nodes {
		if n.Repo == "" || seen[n.Repo] {
			continue
		}
		seen[n.Repo] = true

		repoPath, err := resolveRepoPath(n.Repo, startDir, e.opts.Workspaces)
		if err != nil {
			return err
		}
		if !gitpkg.IsRepo(ctx, repoPath) {
			return fmt.Errorf(
				"workspace for repo %q (%s) is not a git repository — fix workspaces.%s in ~/.spec/config.yaml",
				n.Repo, repoPath, n.Repo)
		}
	}
	return nil
}

// createSession parses the PR stack and creates a fresh session. With no PR
// stack it falls back to a single repo-less node so a plain spec still builds.
func (e *Engine) createSession(specID, specPath, workDir string) (*SessionState, error) {
	steps, err := ParsePRStackFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("parsing PR stack: %w", err)
	}

	if len(steps) == 0 {
		steps = []PRStep{{Number: 1, ID: "n1", Description: "Build implementation", Status: NodePending}}
	}

	session, err := CreateSession(e.db, specID, steps, workDir)
	if err != nil {
		return nil, err
	}
	_ = LogActivity(specID, "Build session started")
	return session, nil
}

// assemble builds the context payload: spec, conventions, prior-node diffs, the
// union of skills the orchestrator may need, and best-effort failing tests.
func (e *Engine) assemble(ctx context.Context, specPath string, session *SessionState, graph *Graph, startDir string) (*BuildContext, error) {
	conventions := ""
	convPath := filepath.Join(startDir, ".spec", "conventions.md")
	if data, err := os.ReadFile(convPath); err == nil {
		conventions = string(data)
	}

	buildCtx, err := AssembleContext(specPath, session, conventions)
	if err != nil {
		return nil, fmt.Errorf("assembling context: %w", err)
	}

	buildCtx.SkillPaths = e.unionSkills(graph, startDir)
	for _, p := range buildCtx.SkillPaths {
		if body := readSkillBody(p); body != "" {
			buildCtx.Skills = append(buildCtx.Skills, body)
		}
	}

	if out := e.runTests(ctx, startDir); out != "" {
		buildCtx.FailingTests = out
	}

	return buildCtx, nil
}

// unionSkills returns the deduplicated union of every node's routed skills, in
// node order — the set the orchestrator may dispatch to its workers.
func (e *Engine) unionSkills(graph *Graph, startDir string) []string {
	router := newSkillRouter(startDir, e.opts)
	seen := make(map[string]bool)
	var paths []string
	for _, n := range graph.Nodes {
		for _, p := range router.Route(n) {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// provision writes the context file and the ephemeral MCP config, then builds
// the DAG-framed adapter request. Skill-capable agents get skill paths; others
// get the skill bodies folded into the system prompt and the context file.
func (e *Engine) provision(specID string, buildCtx *BuildContext, workDir string) (adapter.InvokeRequest, error) {
	contextPath := filepath.Join(SessionDir(specID), "context.md")
	if err := WriteContextFile(buildCtx, contextPath); err != nil {
		fmt.Printf("Warning: could not write context file: %v\n", err)
	}

	mcpConfigPath := filepath.Join(SessionDir(specID), "mcp-config.json")
	if err := writeMCPConfig(specID, mcpConfigPath); err != nil {
		return adapter.InvokeRequest{}, fmt.Errorf("writing mcp config: %w", err)
	}

	caps := e.agent.Capabilities()

	var systemPrompt, prompt string
	var skillPaths []string
	if caps.MCP {
		// Port/conductor model: spec-cli supplies the DAG, node ledger, and all
		// git/PR mechanics; the agent conducts via the MCP port. Per-node worker
		// skills are routed through spec_provision_node, never injected here, so the
		// conductor's context stays clean and cross-repo skills cannot collide.
		systemPrompt = conductorSystemPrompt()
		prompt = conductorKickoff()
		if caps.Skills {
			skillPaths = e.conductorSkillPaths(workDir)
		}
	} else {
		// Solo model: a single non-MCP agent implements the spec itself, so fold
		// the consolidated context and every routed skill body into its prompt.
		systemPrompt = soloSystemPrompt()
		for _, body := range buildCtx.Skills {
			systemPrompt += "\n\n" + strings.TrimSpace(body)
		}
		prompt = soloKickoff(buildCtx)
	}

	return adapter.InvokeRequest{
		SpecID:        specID,
		WorkDir:       workDir,
		ContextFile:   contextPath,
		MCPConfigPath: mcpConfigPath,
		SystemPrompt:  systemPrompt,
		SkillPaths:    skillPaths,
		Prompt:        prompt,
		Headless:      e.opts.Headless,
	}, nil
}

// conductorSkillPaths resolves the skills handed to an MCP-capable conductor.
// These are start-dir-scoped adapter skills (explicit conductor refs, the legacy
// skill refs, or discovered .spec/agent/skills entries) — deliberately NOT the
// per-node registry-routed worker skills, which reach workers only via
// spec_provision_node. Resolving from the start dir alone keeps worker
// capability skills out of the conductor and removes the cross-repo skill
// name-collision class entirely.
func (e *Engine) conductorSkillPaths(startDir string) []string {
	refs := make([]string, 0, len(e.opts.ConductorSkills)+len(e.opts.SkillRefs))
	refs = append(refs, e.opts.ConductorSkills...)
	refs = append(refs, e.opts.SkillRefs...)
	return resolveSkills(startDir, refs, readProfile(startDir))
}

// printStatus prints the resume banner, the DAG shape (waves), the MCP context
// summary, and acceptance-criteria progress.
func (e *Engine) printStatus(specID, specPath string, session *SessionState, graph *Graph, workDir string) {
	fmt.Printf("Building %s — %s\n", specID, specTitle(specPath))
	fmt.Printf("Dir: %s\n", workDir)

	waves := graph.Waves()
	done := session.DoneSet()
	fmt.Printf("DAG: %d nodes in %d wave(s)\n", len(graph.Nodes), len(waves))
	for i, wave := range waves {
		var labels []string
		for _, n := range wave {
			marker := " "
			switch session.NodeStatus(n.NodeID()) {
			case NodeComplete:
				marker = "✓"
			case NodeInProgress:
				marker = "▶"
			case NodeFailed:
				marker = "✗"
			}
			labels = append(labels, fmt.Sprintf("%s %s[%s] %s", marker, n.NodeID(), repoLayer(n), n.Description))
		}
		fmt.Printf("  Wave %d: %s\n", i+1, strings.Join(labels, "; "))
	}
	if len(done) > 0 {
		fmt.Printf("Resuming: %d node(s) already complete.\n", len(done))
	}

	if e.agent.Capabilities().MCP {
		fmt.Println("MCP server: spec mcp-server --spec " + specID)
		fmt.Println("Context available:")
		fmt.Println("  • spec://current/dag (nodes, waves, skill routing)")
		fmt.Printf("  • spec://current/full (%s)\n", specID)
		fmt.Println("  • spec://current/conventions")
	}

	showACProgress(specPath)
	fmt.Println()
}

// repoLayer renders a node's repo:layer tag for status output.
func repoLayer(n PRStep) string {
	if n.Layer == "" {
		return n.Repo
	}
	if n.Repo == "" {
		return ":" + n.Layer
	}
	return n.Repo + ":" + n.Layer
}

// nodeIDs maps steps to their node IDs.
func nodeIDs(steps []PRStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.NodeID()
	}
	return out
}

// runTests runs the configured test command and returns its combined output
// only when it fails. Best-effort: a missing/empty command yields no output.
// The command is split into argv and executed directly (no shell).
func (e *Engine) runTests(ctx context.Context, workDir string) string {
	fields := strings.Fields(e.opts.TestCommand)
	if len(fields) == 0 {
		return ""
	}
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.Dir = workDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(buf.String())
	}
	return ""
}

func specTitle(path string) string {
	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return ""
	}
	return meta.Title
}

func showACProgress(specPath string) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return
	}

	body := markdown.Body(string(data))
	sections := markdown.ExtractSections(body)
	ac := markdown.FindSection(sections, "acceptance_criteria")
	if ac == nil || strings.TrimSpace(ac.Content) == "" {
		return
	}

	total, checked := countACs(ac.Content)
	if total > 0 {
		fmt.Printf("Acceptance criteria: %d/%d passing\n", checked, total)
	}
}

// countACs counts total and checked acceptance-criteria checkboxes.
func countACs(content string) (total, checked int) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- [ ]"):
			total++
		case strings.HasPrefix(trimmed, "- [x]"), strings.HasPrefix(trimmed, "- [X]"):
			total++
			checked++
		}
	}
	return total, checked
}
