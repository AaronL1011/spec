package ai

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ReviewResult represents the outcome of the accept/edit/skip flow.
type ReviewResult struct {
	Action  string // "accept", "edit", "skip"
	Content string // the final content (after edit if applicable)
}

// PresentDraft presents an AI draft to the user for accept/edit/skip.
// This is the standard interaction model for all AI-generated content.
func PresentDraft(sectionName, draft, editor string) (*ReviewResult, error) {
	if draft == "" {
		return &ReviewResult{Action: "skip"}, nil
	}

	// Display the draft
	fmt.Println()
	fmt.Printf("─── DRAFT %s ─────────────────────────────────────\n", sectionName)
	// Indent the draft
	for _, line := range strings.Split(draft, "\n") {
		fmt.Printf(" %s\n", line)
	}
	fmt.Println("──────────────────────────────────────────────────────────")
	fmt.Println()

	// Prompt
	fmt.Print(" Accept draft? [y/e/s] (yes / edit in $EDITOR / skip) ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	switch answer {
	case "y", "yes":
		return &ReviewResult{Action: "accept", Content: draft}, nil

	case "e", "edit":
		edited, err := editInEditor(draft, editor)
		if err != nil {
			return nil, fmt.Errorf("editor failed: %w", err)
		}
		return &ReviewResult{Action: "edit", Content: edited}, nil

	case "s", "skip", "":
		return &ReviewResult{Action: "skip"}, nil

	default:
		return &ReviewResult{Action: "skip"}, nil
	}
}

func editInEditor(content, editor string) (string, error) {
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "spec-draft-*.md")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		return "", err
	}
	_ = tmpFile.Close()

	// Open editor
	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Read edited content
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", err
	}
	return string(data), nil
}
