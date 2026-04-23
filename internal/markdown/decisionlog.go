package markdown

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DecisionEntry represents a row in the decision log table.
type DecisionEntry struct {
	Number    int
	Question  string
	Options   string
	Decision  string
	Rationale string
	DecidedBy string
	Date      string
}

var tableRowPattern = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)

// ParseDecisionLog extracts decision entries from markdown content.
func ParseDecisionLog(content string) ([]DecisionEntry, error) {
	lines := strings.Split(content, "\n")
	var entries []DecisionEntry

	for _, line := range lines {
		if !tableRowPattern.MatchString(line) {
			continue
		}

		cells := splitTableRow(line)
		if len(cells) < 7 {
			continue
		}

		num, err := strconv.Atoi(strings.TrimSpace(cells[0]))
		if err != nil {
			continue
		}

		entries = append(entries, DecisionEntry{
			Number:    num,
			Question:  strings.TrimSpace(cells[1]),
			Options:   strings.TrimSpace(cells[2]),
			Decision:  strings.TrimSpace(cells[3]),
			Rationale: strings.TrimSpace(cells[4]),
			DecidedBy: strings.TrimSpace(cells[5]),
			Date:      strings.TrimSpace(cells[6]),
		})
	}

	return entries, nil
}

// ParseDecisionLogFromFile reads a spec file and extracts the decision log.
func ParseDecisionLogFromFile(path string) ([]DecisionEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	sections := ExtractSections(Body(string(data)))
	dlSection := FindSection(sections, "decision_log")
	if dlSection == nil {
		return nil, fmt.Errorf("decision log section not found in %s", path)
	}

	return ParseDecisionLog(dlSection.Content)
}

// AppendDecision adds a new question to the decision log in a file.
// Returns the assigned decision number.
func AppendDecision(path, question, user string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	content := string(data)
	sections := ExtractSections(Body(content))
	dlSection := FindSection(sections, "decision_log")
	if dlSection == nil {
		return 0, fmt.Errorf("decision log section not found in %s", path)
	}

	// Find the next number
	entries, _ := ParseDecisionLog(dlSection.Content)
	nextNum := 1
	for _, e := range entries {
		if e.Number >= nextNum {
			nextNum = e.Number + 1
		}
	}

	date := time.Now().Format("2006-01-02")
	newRow := fmt.Sprintf("| %03d | %s | | | | %s | %s |", nextNum, question, user, date)

	// Append the row to the decision log section
	newSectionContent := strings.TrimRight(dlSection.Content, "\n") + "\n" + newRow + "\n"

	result, err := ReplaceSectionContent(content, "decision_log", newSectionContent)
	if err != nil {
		return 0, err
	}

	if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", path, err)
	}

	return nextNum, nil
}

// ResolveDecision updates an existing decision log entry.
func ResolveDecision(path string, number int, decision, rationale, user string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	found := false
	date := time.Now().Format("2006-01-02")

	for i, line := range lines {
		if !tableRowPattern.MatchString(line) {
			continue
		}
		cells := splitTableRow(line)
		if len(cells) < 7 {
			continue
		}
		num, err := strconv.Atoi(strings.TrimSpace(cells[0]))
		if err != nil || num != number {
			continue
		}

		// Update the row
		cells[3] = fmt.Sprintf(" **%s** ", decision)
		cells[4] = fmt.Sprintf(" %s ", rationale)
		cells[5] = fmt.Sprintf(" %s ", user)
		cells[6] = fmt.Sprintf(" %s ", date)
		lines[i] = "|" + strings.Join(cells, "|") + "|"
		found = true
		break
	}

	if !found {
		return fmt.Errorf("decision #%03d not found — run 'spec decide <id> --list' to see existing decisions", number)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// splitTableRow splits a markdown table row into cells.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// FormatDecisionTable renders decision entries as a markdown table.
func FormatDecisionTable(entries []DecisionEntry) string {
	var sb strings.Builder
	sb.WriteString("| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |\n")
	sb.WriteString("|---|---|---|---|---|---|---|\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "| %03d | %s | %s | %s | %s | %s | %s |\n",
			e.Number, e.Question, e.Options, e.Decision, e.Rationale, e.DecidedBy, e.Date)
	}
	return sb.String()
}
