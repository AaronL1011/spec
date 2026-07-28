package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Section content hashing backs optimistic concurrency for agent writes: an
// agent reads a section with its hash, and a write carrying a stale hash is
// rejected instead of clobbering an edit made in the meantime.
//
// The hash is taken over the section body *after* canonical normalisation, not
// over whatever bytes a caller submitted. That distinction is load-bearing: a
// write reformats what it is given, so hashing the submitted text would make the
// hash a writer receives disagree with the hash a subsequent reader computes, and
// every second write in a session would fail with a spurious conflict.

// NormalizeSectionBody canonicalises a section body for hashing and comparison.
//
// It normalises line endings and strips leading and trailing blank lines, which
// are exactly the differences that carry no meaning in a markdown section and
// that editors and writers introduce inconsistently. Interior whitespace is
// preserved, because inside a section it can be significant (code blocks,
// nested lists).
func NormalizeSectionBody(body string) string {
	s := strings.ReplaceAll(body, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	// Trailing whitespace on a line is invisible and unstable, so it is dropped
	// before hashing to keep the hash from depending on an editor's habits.
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// SectionHash returns the content hash of a section body.
//
// The value is prefixed with its algorithm so a future change can be recognised
// rather than silently compared against a differently-computed digest.
func SectionHash(body string) string {
	sum := sha256.Sum256([]byte(NormalizeSectionBody(body)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SectionContentHash reads a spec file and returns one section's body and hash.
//
// This is the read half of the concurrency contract: the hash it returns is what
// a subsequent unmodified write must present.
func SectionContentHash(path, slug string) (body, hash string, err error) {
	sections, err := ExtractSectionsFromFile(path)
	if err != nil {
		return "", "", err
	}
	section := FindSection(sections, slug)
	if section == nil {
		return "", "", fmt.Errorf("section %q not found in %s", slug, path)
	}
	return section.Content, SectionHash(section.Content), nil
}

// ReplaceSectionChecked replaces a section only when baseHash matches the
// section's current hash, and returns the hash of the newly written content.
//
// Returning the new hash is what makes agent-to-agent editing work without a
// re-read between every step: a writer can chain writes using the hash it just
// received. An empty baseHash skips the check, for callers that are not
// participating in optimistic concurrency (the CLI, where the human is the lock).
func ReplaceSectionChecked(path, slug, newContent, baseHash string) (newHash string, err error) {
	current, currentHash, err := SectionContentHash(path, slug)
	if err != nil {
		return "", err
	}

	if baseHash != "" && baseHash != currentHash {
		return "", &ConflictError{
			Slug:         slug,
			ExpectedHash: baseHash,
			ActualHash:   currentHash,
			// The current body travels with the error so a caller can show what
			// it would have overwritten instead of just reporting a mismatch.
			CurrentContent: current,
		}
	}

	if err := ReplaceSection(path, slug, newContent); err != nil {
		return "", err
	}

	_, newHash, err = SectionContentHash(path, slug)
	if err != nil {
		return "", err
	}
	return newHash, nil
}

// ConflictError reports that a section changed since the hash a writer holds.
//
// It is a distinct type rather than a formatted string so callers can react
// structurally — the MCP port turns it into a machine-readable error naming the
// section, and the TUI into a non-destructive notice.
type ConflictError struct {
	Slug           string
	ExpectedHash   string
	ActualHash     string
	CurrentContent string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("section %q changed since it was read (expected %s, found %s) — re-read the section and reapply your edit",
		e.Slug, shortHash(e.ExpectedHash), shortHash(e.ActualHash))
}

// shortHash abbreviates a hash for human-readable messages.
func shortHash(h string) string {
	h = strings.TrimPrefix(h, "sha256:")
	if len(h) > 8 {
		return h[:8]
	}
	if h == "" {
		return "none"
	}
	return h
}
