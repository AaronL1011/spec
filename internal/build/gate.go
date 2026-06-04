package build

import (
	"fmt"
	"os"
	"strings"
)

// AllLeavesHaveDraftPR reports whether every leaf node in a §7.3 PR stack has a
// recorded draft-PR URL (a `<!-- pr: <url> -->` annotation). It is the
// verifier behind the pr_stack_exists gate: review may not begin until each
// stack tip has an open draft PR. prSectionContent is the §7.3 section body.
//
// Returns false when the plan is empty, malformed (not a DAG), has no leaves,
// or any leaf is missing its PR — so an unbuilt or partially-finished spec
// never passes the gate.
func AllLeavesHaveDraftPR(prSectionContent string) bool {
	steps, err := parsePRSteps(prSectionContent)
	if err != nil || len(steps) == 0 {
		return false
	}
	g, err := BuildGraph(steps)
	if err != nil {
		return false
	}
	leaves := g.Leaves()
	if len(leaves) == 0 {
		return false
	}
	for _, leaf := range leaves {
		if strings.TrimSpace(leaf.PRURL) == "" {
			return false
		}
	}
	return true
}

// recordPRInSpec annotates a node's line in the §7.3 PR Stack Plan with its
// draft-PR URL, so the pr_stack_exists gate can verify coverage from the spec
// itself. It rewrites any existing annotation on that line (idempotent).
func recordPRInSpec(specPath string, nodeNumber int, url string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("reading spec %s: %w", specPath, err)
	}

	lines := strings.Split(string(data), "\n")
	inSection := false
	for i, line := range lines {
		if isHeadingLine(line) {
			inSection = isPRStackHeading(line)
			continue
		}
		if !inSection {
			continue
		}
		m := prStepPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil || n != nodeNumber {
			continue
		}
		clean := strings.TrimRight(prAnnotationPattern.ReplaceAllString(line, ""), " ")
		lines[i] = fmt.Sprintf("%s <!-- pr: %s -->", clean, url)
		if err := os.WriteFile(specPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return fmt.Errorf("writing spec %s: %w", specPath, err)
		}
		return nil
	}
	return fmt.Errorf("node %d not found in §7.3 PR Stack Plan of %s — cannot record PR", nodeNumber, specPath)
}

// isHeadingLine reports whether a line is a markdown ATX heading.
func isHeadingLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// isPRStackHeading reports whether a heading line is the §7.3 PR Stack Plan
// heading, matching both the numbered and prose authoring styles.
func isPRStackHeading(line string) bool {
	return strings.Contains(strings.ToLower(line), "pr stack plan")
}
