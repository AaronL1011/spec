package build

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
)

// Engine orchestrates the build process.
type Engine struct {
	db    *store.DB
	agent adapter.AgentAdapter
	opts  Options
}

// NewEngine creates a new build engine.
func NewEngine(db *store.DB, agent adapter.AgentAdapter, opts Options) *Engine {
	SetActivityDB(db)
	return &Engine{db: db, agent: agent, opts: opts}
}

// StartOrResume begins or continues a build session for a spec. It walks the
// PR stack one step at a time, automatically moving into each step's target
// repository (via configured workspaces) so a multi-repo spec can be executed
// end-to-end. It advances to the next step only when the current one is
// completed (via MCP or the interactive prompt); if a step is left unfinished
// it stops so the user can resume later with `spec do`.
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

	for {
		if session.IsComplete() {
			fmt.Printf("✓ All %d steps complete for %s.\n", len(session.Steps), specID)
			return nil
		}

		step := session.CurrentPRStep()
		if step == nil {
			return fmt.Errorf("no current step — session may be corrupted")
		}

		workDir, err := e.resolveStepDir(specID, step, startDir)
		if err != nil {
			return err
		}

		advanced, err := e.runOneStep(ctx, specID, specPath, session, step, workDir)
		if err != nil {
			return err
		}
		if !advanced {
			// Step left in progress — stop; the user resumes with `spec do`.
			return nil
		}

		// Reload the session (the MCP server may have advanced it in another
		// process) and continue with the next step.
		session, err = LoadSession(e.db, specID)
		if err != nil {
			return err
		}
		if session == nil {
			return nil
		}
	}
}

// runOneStep provisions and runs a single step, returning whether it advanced.
func (e *Engine) runOneStep(ctx context.Context, specID, specPath string, session *SessionState, step *PRStep, workDir string) (bool, error) {
	if err := e.setupBranch(ctx, session, step, workDir); err != nil {
		return false, err
	}

	buildCtx, err := e.assemble(ctx, specPath, session, workDir)
	if err != nil {
		return false, err
	}

	e.printStatus(specID, specPath, session, step, workDir)

	req, err := e.provision(specID, buildCtx, workDir)
	if err != nil {
		return false, err
	}

	return e.runStep(ctx, specID, session, step, workDir, req)
}

// resolveStepDir returns the directory a step should run in. If the step has no
// repo, or the current directory already matches it, the start directory is
// used. Otherwise the configured workspace path for that repo is used; a
// missing mapping is an actionable error rather than a dead-end.
func (e *Engine) resolveStepDir(specID string, step *PRStep, startDir string) (string, error) {
	if step.Repo == "" || step.Repo == filepath.Base(startDir) {
		return startDir, nil
	}

	ws := expandTilde(e.opts.Workspaces[step.Repo])
	if ws == "" {
		return "", fmt.Errorf(
			"step %d targets repo %q but you're in %q and no workspace is configured\n"+
				"add it to ~/.spec/config.yaml under:\n  workspaces:\n    %s: /path/to/%s\n"+
				"or cd into the repo and run: spec do %s",
			step.Number, step.Repo, filepath.Base(startDir), step.Repo, step.Repo, specID)
	}
	if !filepath.IsAbs(ws) {
		ws = filepath.Join(startDir, ws)
	}
	info, err := os.Stat(ws)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf(
			"step %d targets repo %q but its workspace path %q is not a directory — "+
				"fix 'workspaces.%s' in ~/.spec/config.yaml",
			step.Number, step.Repo, ws, step.Repo)
	}
	fmt.Printf("→ %s: working in %s\n", step.Repo, ws)
	return ws, nil
}

// createSession parses the PR stack and creates a fresh session.
func (e *Engine) createSession(specID, specPath, workDir string) (*SessionState, error) {
	steps, err := ParsePRStackFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("parsing PR stack: %w", err)
	}

	if len(steps) == 0 {
		// No PR stack — single-step build with no repo, so it runs wherever the
		// user launched it rather than inventing a repo name from the cwd.
		steps = []PRStep{{
			Number:      1,
			Description: "Build implementation",
			Status:      "pending",
		}}
	}

	session, err := CreateSession(e.db, specID, steps, workDir)
	if err != nil {
		return nil, err
	}
	_ = LogActivity(specID, "Build session started") // Best-effort logging
	return session, nil
}

// setupBranch generates a branch name, creates/checks out the branch, and
// records the base ref for diff capture.
func (e *Engine) setupBranch(ctx context.Context, session *SessionState, step *PRStep, workDir string) error {
	if step.Branch == "" {
		step.Branch = gitpkg.SpecBranchName(session.SpecID, step.Number, step.Description)
	}

	if !gitpkg.BranchExists(ctx, workDir, step.Branch) {
		// Record the commit the branch is cut from so we can diff later.
		if step.BaseRef == "" {
			if base, err := gitpkg.RevParse(ctx, workDir, "HEAD"); err == nil {
				step.BaseRef = strings.TrimSpace(base)
			}
		}
		if err := gitpkg.CreateBranch(ctx, workDir, step.Branch); err != nil {
			return fmt.Errorf("creating branch %s: %w", step.Branch, err)
		}
	} else if currentBranch, _ := gitpkg.CurrentBranch(ctx, workDir); currentBranch != step.Branch {
		if err := gitpkg.CheckoutBranch(ctx, workDir, step.Branch); err != nil {
			return fmt.Errorf("checking out branch %s: %w", step.Branch, err)
		}
	}

	_ = SaveSession(e.db, session) // Best-effort persistence
	return nil
}

// assemble builds the context payload, including conventions, skills, and
// best-effort failing-test output.
func (e *Engine) assemble(ctx context.Context, specPath string, session *SessionState, workDir string) (*BuildContext, error) {
	conventions := ""
	convPath := filepath.Join(workDir, ".spec", "conventions.md")
	if data, err := os.ReadFile(convPath); err == nil {
		conventions = string(data)
	}

	buildCtx, err := AssembleContext(specPath, session, conventions)
	if err != nil {
		return nil, fmt.Errorf("assembling context: %w", err)
	}

	profile := readProfile(workDir)
	for _, p := range resolveSkills(workDir, e.opts.SkillRefs, profile) {
		if body := readSkillBody(p); body != "" {
			buildCtx.Skills = append(buildCtx.Skills, body)
		}
	}

	if out := e.runTests(ctx, workDir); out != "" {
		buildCtx.FailingTests = out
	}

	return buildCtx, nil
}

// provision writes the context file and the ephemeral MCP config, then builds
// the adapter request. Skills flow as paths to skill-capable agents; otherwise
// their bodies ride along in the context file and system prompt.
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
	systemPrompt := buildCtx.SystemPrompt

	profile := readProfile(workDir)
	skillPaths := resolveSkills(workDir, e.opts.SkillRefs, profile)
	if !caps.Skills {
		// Non-skill agents get the playbook inline in the system prompt.
		for _, body := range buildCtx.Skills {
			systemPrompt += "\n\n" + strings.TrimSpace(body)
		}
		skillPaths = nil
	}

	return adapter.InvokeRequest{
		SpecID:        specID,
		WorkDir:       workDir,
		ContextFile:   contextPath,
		MCPConfigPath: mcpConfigPath,
		SystemPrompt:  systemPrompt,
		SkillPaths:    skillPaths,
		Prompt:        buildKickoffPrompt(buildCtx),
		Headless:      e.opts.Headless,
	}, nil
}

// runStep invokes the agent and handles step completion. On the MCP path
// advancement happens via the spec_step_complete tool; the engine only reports
// status. The interactive [y/n] fallback is gated on non-MCP agents whose step
// is still in-progress.
func (e *Engine) runStep(ctx context.Context, specID string, session *SessionState, step *PRStep, workDir string, req adapter.InvokeRequest) (bool, error) {
	completedStepNum := step.Number
	baseRef := step.BaseRef

	_ = LogActivity(specID, fmt.Sprintf("Step %d started: %s", step.Number, step.Description))
	result, err := e.agent.Invoke(ctx, req)
	if err != nil {
		return false, fmt.Errorf("agent exited with error: %w", err)
	}
	if result == nil {
		result = &adapter.InvokeResult{}
	}

	// Re-read session: the MCP server advances it in a separate process.
	updated, err := LoadSession(e.db, specID)
	if err != nil {
		return false, err
	}
	advancedViaMCP := result.StepSignalled || (updated != nil && updated.CurrentStep > session.CurrentStep)

	if advancedViaMCP {
		captureStepDiff(ctx, workDir, specID, completedStepNum, baseRef)
		_ = LogActivity(specID, fmt.Sprintf("Step %d completed: %s", completedStepNum, step.Description))
		fmt.Printf("✓ Step %d complete.\n", completedStepNum)
		return true, nil
	}

	if e.agent.Capabilities().MCP {
		// MCP agent that did not advance — nothing to prompt; report status.
		fmt.Printf("\nStep %d still in progress. Run 'spec do %s' to resume.\n", completedStepNum, specID)
		return false, nil
	}

	return e.promptComplete(ctx, specID, session, completedStepNum, baseRef, workDir)
}

// promptComplete is the interactive fallback for non-MCP agents. It returns
// whether the step was advanced.
func (e *Engine) promptComplete(ctx context.Context, specID string, session *SessionState, stepNum int, baseRef, workDir string) (bool, error) {
	fmt.Printf("\nStep %d complete? [y/n] ", stepNum)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		return false, nil
	}

	if err := AdvanceStep(e.db, session); err != nil {
		return false, err
	}
	captureStepDiff(ctx, workDir, specID, stepNum, baseRef)
	_ = LogActivity(specID, fmt.Sprintf("Step %d completed: %s", stepNum, session.Steps[stepNum-1].Description))
	fmt.Printf("✓ Step %d complete.\n", stepNum)
	return true, nil
}

// printStatus prints the resume banner, the working directory, the MCP context
// summary (when the agent is MCP-capable), and acceptance-criteria progress.
func (e *Engine) printStatus(specID, specPath string, session *SessionState, step *PRStep, workDir string) {
	fmt.Printf("Resuming %s — %s\n", specID, specTitle(specPath))
	fmt.Printf("Step %d/%d: [%s] %s\n", step.Number, len(session.Steps), step.Repo, step.Description)
	fmt.Printf("Dir: %s\n", workDir)
	fmt.Printf("Branch: %s\n", step.Branch)

	if e.agent.Capabilities().MCP {
		fmt.Println("MCP server: spec mcp-server --spec " + specID)
		fmt.Println("Context available:")
		fmt.Printf("  • spec://current/full (%s)\n", specID)
		fmt.Println("  • spec://current/prior-diffs")
		fmt.Println("  • spec://current/conventions")
	}

	showACProgress(specPath)
	fmt.Println()
}

// runTests runs the configured test command and returns its combined output
// only when it fails. Best-effort: a missing/empty command yields no output.
// The command is split into argv and executed directly (no shell), so it is a
// parameterized invocation rather than shell-string composition.
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
