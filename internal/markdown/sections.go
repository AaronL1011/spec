package markdown

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Section represents a parsed markdown section with ownership.
type Section struct {
	Slug      string // e.g., "problem_statement"
	Heading   string // e.g., "## 1. Problem Statement"
	Level     int    // heading level (2 = ##, 3 = ###)
	Owner     string // from <!-- owner: role --> marker, or "auto"
	Content   string // raw markdown content (excluding heading line)
	StartLine int    // line number in the source file (1-indexed)
	EndLine   int    // line number (exclusive)
}

var (
	headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	ownerPattern   = regexp.MustCompile(`<!--\s*owner:\s*(\w+)\s*-->`)
	autoPattern    = regexp.MustCompile(`<!--\s*auto:\s*(.+?)\s*-->`)
	fencePattern   = regexp.MustCompile("^(\\s{0,3})(`{3,}|~{3,})")
)

// fenceMask returns a boolean for each line that is true when the line lies
// inside a fenced code block (and false for the fence delimiters themselves
// and ordinary prose). A line such as "# load config" inside a ``` block is a
// code comment, not a markdown heading, so callers must not treat it as a
// section boundary. Closing fences must use the same marker character and be
// at least as long as the opening fence, per CommonMark.
func fenceMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	inFence := false
	var fenceChar byte
	fenceLen := 0
	for i, line := range lines {
		m := fencePattern.FindStringSubmatch(line)
		switch {
		case m == nil:
			mask[i] = inFence
		case !inFence:
			inFence = true
			fenceChar = m[2][0]
			fenceLen = len(m[2])
		case m[2][0] == fenceChar && len(m[2]) >= fenceLen:
			// Closing fence: the delimiter line is not inside the block.
			inFence = false
		default:
			// A different marker while open is just code content.
			mask[i] = true
		}
	}
	return mask
}

// ExtractSections parses markdown content into sections.
func ExtractSections(content string) []Section {
	lines := strings.Split(content, "\n")
	inCode := fenceMask(lines)
	var sections []Section
	var currentOwner string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if inCode[i] {
			continue
		}
		matches := headingPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		level := len(matches[1])
		heading := matches[2]

		// Check for owner marker on the same line
		owner := ""
		if m := ownerPattern.FindStringSubmatch(line); m != nil {
			owner = m[1]
		} else if m := autoPattern.FindStringSubmatch(line); m != nil {
			owner = "auto"
		}

		// Sub-sections inherit parent's owner unless they have their own
		if owner != "" {
			if level <= 2 {
				currentOwner = owner
			}
		} else {
			owner = currentOwner
		}

		slug := slugify(heading)

		section := Section{
			Slug:      slug,
			Heading:   line,
			Level:     level,
			Owner:     owner,
			StartLine: i + 1,
		}

		// Find the content: everything until the next same-or-higher-level heading
		contentStart := i + 1
		contentEnd := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if inCode[j] {
				continue
			}
			nextMatches := headingPattern.FindStringSubmatch(lines[j])
			if nextMatches != nil {
				nextLevel := len(nextMatches[1])
				if nextLevel <= level {
					contentEnd = j
					break
				}
			}
		}

		section.EndLine = contentEnd
		if contentStart < contentEnd {
			section.Content = strings.Join(lines[contentStart:contentEnd], "\n")
		}

		sections = append(sections, section)
	}

	return sections
}

// ExtractSectionsFromFile reads a file and extracts sections.
func ExtractSectionsFromFile(path string) ([]Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	content := Body(string(data))
	return ExtractSections(content), nil
}

// FindSection returns the section with the given slug, or nil.
func FindSection(sections []Section, slug string) *Section {
	for i := range sections {
		if sections[i].Slug == slug {
			return &sections[i]
		}
	}
	return nil
}

// ReplaceSection replaces the content of a section in a file.
func ReplaceSection(path, slug, newContent string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	result, err := ReplaceSectionContent(string(data), slug, newContent)
	if err != nil {
		return err
	}

	return os.WriteFile(path, []byte(result), 0o644)
}

// ReplaceSectionContent replaces a section's content in the markdown string.
func ReplaceSectionContent(content, slug, newContent string) (string, error) {
	lines := strings.Split(content, "\n")
	inCode := fenceMask(lines)

	// Find the section heading
	sectionStart := -1
	sectionLevel := 0
	for i, line := range lines {
		if inCode[i] {
			continue
		}
		matches := headingPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		if slugify(matches[2]) == slug {
			sectionStart = i
			sectionLevel = len(matches[1])
			break
		}
	}

	if sectionStart < 0 {
		return "", fmt.Errorf("section %q not found", slug)
	}

	// Find the end of the section
	sectionEnd := len(lines)
	for j := sectionStart + 1; j < len(lines); j++ {
		if inCode[j] {
			continue
		}
		matches := headingPattern.FindStringSubmatch(lines[j])
		if matches != nil && len(matches[1]) <= sectionLevel {
			sectionEnd = j
			break
		}
	}

	// Rebuild: heading + new content + rest
	var result []string
	result = append(result, lines[:sectionStart+1]...)
	if newContent != "" {
		result = append(result, newContent)
	}
	result = append(result, lines[sectionEnd:]...)

	return strings.Join(result, "\n"), nil
}

// IsSectionNonEmpty checks if a section has meaningful content (not just whitespace).
func IsSectionNonEmpty(sections []Section, slug string) bool {
	s := FindSection(sections, slug)
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.Content) != ""
}

// slugify converts a heading to a slug.
// "## 1. Problem Statement <!-- owner: pm -->" → "problem_statement"
func slugify(heading string) string {
	// Remove owner/auto comments
	heading = ownerPattern.ReplaceAllString(heading, "")
	heading = autoPattern.ReplaceAllString(heading, "")

	// Remove heading markers
	heading = strings.TrimLeft(heading, "# ")

	// Remove section numbers (e.g., "1.", "7.3")
	heading = regexp.MustCompile(`^\d+(\.\d+)*\.?\s*`).ReplaceAllString(heading, "")

	// Lowercase, replace spaces/special chars with underscores
	heading = strings.ToLower(strings.TrimSpace(heading))
	heading = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(heading, "_")
	heading = strings.Trim(heading, "_")

	return heading
}

// ValidSectionSlugs returns all valid section slugs from the spec template.
func ValidSectionSlugs() []string {
	return []string{
		"tl_dr",
		"problem_statement",
		"goals_non_goals",
		"user_stories",
		"proposed_solution",
		"concept_overview",
		"architecture_approach",
		"design_inputs",
		"acceptance_criteria",
		"technical_implementation",
		"architecture_notes",
		"dependencies_risks",
		"pr_stack_plan",
		"escape_hatch_log",
		"qa_validation_notes",
		"deployment_notes",
		"retrospective",
		"decision_log",
	}
}

// IsValidSectionSlug checks if the slug is a known section.
func IsValidSectionSlug(slug string) bool {
	for _, s := range ValidSectionSlugs() {
		if s == slug {
			return true
		}
	}
	return false
}
