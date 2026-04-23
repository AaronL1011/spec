package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/adapter"
	gitpkg "github.com/aaronl1011/spec-cli/internal/git"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/store"
)

// Engine orchestrates the build process.
type Engine struct {
	db    *store.DB
	agent adapter.AgentAdapter
}

// NewEngine creates a new build engine.
func NewEngine(db *store.DB, agent adapter.AgentAdapter) *Engine {
	SetActivityDB(db)
	return &Engine{db: db, agent: agent}
}

// StartOrResume begins or continues a build session for a spec.
func (e *Engine) StartOrResume(ctx context.Context, specID, specPath, workDir string) error {
	// Check for existing session
	session, err := LoadSession(e.db, specID)
	if err != nil {
		return err
	}

	if session == nil {
		// Create a new session
		steps, err := ParsePRStackFromFile(specPath)
		if err != nil {
			return fmt.Errorf("parsing PR stack: %w", err)
		}

		if len(steps) == 0 {
			// No PR stack — single-step build
			steps = []PRStep{{
				Number:      1,
				Repo:        filepath.Base(workDir),
				Description: "Build implementation",
				Status:      "pending",
			}}
		}

		session, err = CreateSession(e.db, specID, steps, workDir)
		if err != nil {
			return err
		}

		_ = LogActivity(specID, "Build session started") // Best-effort logging
	}

	if session.IsComplete() {
		fmt.Printf("✓ All %d steps complete for %s.\n", len(session.Steps), specID)
		return nil
	}

	step := session.CurrentPRStep()
	if step == nil {
		return fmt.Errorf("no current step — session may be corrupted")
	}

	// Check if we're in the right repo
	currentRepo := filepath.Base(workDir)
	if step.Repo != "" && step.Repo != currentRepo {
		fmt.Printf("This step targets %s. Switch to that repo and run 'spec do' again.\n", step.Repo)
		return nil
	}

	// Generate branch name
	if step.Branch == "" {
		step.Branch = gitpkg.SpecBranchName(specID, step.Number, step.Description)
		_ = SaveSession(e.db, session) // Best-effort persistence
	}

	// Create or checkout branch
	if !gitpkg.BranchExists(ctx, workDir, step.Branch) {
		if err := gitpkg.CreateBranch(ctx, workDir, step.Branch); err != nil {
			return fmt.Errorf("creating branch %s: %w", step.Branch, err)
		}
	} else {
		if currentBranch, _ := gitpkg.CurrentBranch(ctx, workDir); currentBranch != step.Branch {
			if err := gitpkg.CheckoutBranch(ctx, workDir, step.Branch); err != nil {
				return fmt.Errorf("checking out branch %s: %w", step.Branch, err)
			}
		}
	}

	// Read conventions
	conventions := ""
	convPath := filepath.Join(workDir, ".spec", "conventions.md")
	if data, err := os.ReadFile(convPath); err == nil {
		conventions = string(data)
	}

	// Assemble context
	buildCtx, err := AssembleContext(specPath, session, conventions)
	if err != nil {
		return fmt.Errorf("assembling context: %w", err)
	}

	// Print status
	fmt.Printf("Resuming %s — %s\n", specID, specTitle(specPath))
	fmt.Printf("Step %d/%d: [%s] %s\n", step.Number, len(session.Steps), step.Repo, step.Description)
	fmt.Printf("Branch: %s\n", step.Branch)

	// Show AC progress
	showACProgress(specPath)
	fmt.Println()

	// Write context file (for non-MCP agents or as backup)
	contextPath := filepath.Join(SessionDir(specID), "context.md")
	if err := WriteContextFile(buildCtx, contextPath); err != nil {
		fmt.Printf("Warning: could not write context file: %v\n", err)
	}

	// Start MCP server if agent supports it
	if e.agent.SupportsMCP() {
		fmt.Println("MCP server started on stdio.")
		fmt.Println("Context available:")
		fmt.Printf("  • spec://current/full (%s)\n", specID)
		fmt.Println("  • spec://current/prior-diffs")
		fmt.Println("  • spec://current/conventions")
		fmt.Println()
	}

	// Invoke the agent
	_ = LogActivity(specID, fmt.Sprintf("Step %d started: %s", step.Number, step.Description))
	if err := e.agent.Invoke(ctx, contextPath, workDir); err != nil {
		return fmt.Errorf("agent exited with error: %w", err)
	}

	// After agent exits, prompt for step completion if not already done via MCP
	fmt.Printf("\nStep %d complete? [y/n] ", step.Number)
	var answer string
	_, _ = fmt.Scanln(&answer)
	if strings.ToLower(answer) == "y" {
		if err := AdvanceStep(e.db, session); err != nil {
			return err
		}
		_ = LogActivity(specID, fmt.Sprintf("Step %d completed: %s", step.Number, step.Description))

		if session.IsComplete() {
			fmt.Printf("✓ All %d steps complete for %s!\n", len(session.Steps), specID)
		} else {
			next := session.CurrentPRStep()
			fmt.Printf("Next: Step %d — [%s] %s\n", next.Number, next.Repo, next.Description)
		}
	}

	return nil
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

	total := 0
	checked := 0
	for _, line := range strings.Split(ac.Content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			total++
		} else if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			total++
			checked++
		}
	}

	if total > 0 {
		fmt.Printf("Acceptance criteria: %d/%d passing\n", checked, total)
	}
}
