package markdown

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixtureSpec writes the shared fixture to a temp file and returns its path.
func writeFixtureSpec(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "SPEC-001.md")
	if err := os.WriteFile(path, []byte(fixtureSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const fixtureSpec = `---
id: SPEC-001
title: Test
---

# SPEC-001 - Test

## 1. Problem Statement

The original problem.

## 2. Goals

The goals.
`

// The hash must ignore differences that carry no meaning, or writers and readers
// will disagree over whitespace an editor introduced.
func TestNormalizeSectionBody_IgnoresInsignificantWhitespace(t *testing.T) {
	same := []string{
		"content",
		"content\n",
		"\n\ncontent\n\n\n",
		"content   ",
		"content\t\n",
		"\r\ncontent\r\n",
	}
	want := SectionHash(same[0])
	for _, s := range same[1:] {
		if got := SectionHash(s); got != want {
			t.Errorf("SectionHash(%q) = %s, want it to match %q", s, got, same[0])
		}
	}
}

// Interior whitespace can be significant (code blocks, nested lists), so it must
// not be normalised away.
func TestNormalizeSectionBody_PreservesInteriorStructure(t *testing.T) {
	a := "line one\n\nline two"
	b := "line one\nline two"
	if SectionHash(a) == SectionHash(b) {
		t.Error("a blank line between paragraphs is meaningful and must change the hash")
	}

	indented := "- item\n  - nested"
	flat := "- item\n- nested"
	if SectionHash(indented) == SectionHash(flat) {
		t.Error("indentation is meaningful in lists and must change the hash")
	}
}

func TestSectionHash_IsPrefixedWithAlgorithm(t *testing.T) {
	if !strings.HasPrefix(SectionHash("x"), "sha256:") {
		t.Error("hash should name its algorithm so a future change is recognisable")
	}
}

func TestSectionContentHash_ReadsNamedSection(t *testing.T) {
	path := writeFixtureSpec(t)

	body, hash, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatalf("SectionContentHash: %v", err)
	}
	if !strings.Contains(body, "The original problem.") {
		t.Errorf("body = %q", body)
	}
	if hash == "" {
		t.Error("hash should be populated")
	}

	if _, _, err := SectionContentHash(path, "no_such_section"); err == nil {
		t.Error("expected an error for an unknown section")
	}
}

// The read/write contract: a hash from read must be accepted by an unmodified
// write.
func TestReplaceSectionChecked_AcceptsMatchingHash(t *testing.T) {
	path := writeFixtureSpec(t)

	_, hash, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatal(err)
	}

	newHash, err := ReplaceSectionChecked(path, "problem_statement", "A better problem.", hash)
	if err != nil {
		t.Fatalf("write with a current hash should succeed: %v", err)
	}
	if newHash == "" || newHash == hash {
		t.Errorf("write should return the new content hash, got %q (old %q)", newHash, hash)
	}

	body, _, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "A better problem.") {
		t.Errorf("content not written: %q", body)
	}
}

// This is the property that makes multi-write agent sessions work without a
// re-read between every step, and the one that a hash-of-submitted-content
// implementation would break.
func TestReplaceSectionChecked_ReturnedHashIsImmediatelyReusable(t *testing.T) {
	path := writeFixtureSpec(t)

	_, hash, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatal(err)
	}

	// Three chained writes, each using only the hash the previous one returned.
	for i, content := range []string{"first edit", "second edit", "third edit"} {
		hash, err = ReplaceSectionChecked(path, "problem_statement", content, hash)
		if err != nil {
			t.Fatalf("chained write %d failed with the hash returned by the previous write: %v", i+1, err)
		}
	}

	body, readHash, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "third edit") {
		t.Errorf("final content = %q", body)
	}
	// A fresh read must agree with what the last write reported.
	if readHash != hash {
		t.Errorf("hash from write (%s) disagrees with hash from read (%s) — writers and readers must compute the same value", hash, readHash)
	}
}

// A stale hash must be refused, and the file left byte-identical: the whole point
// is that concurrent edits cannot silently clobber each other.
func TestReplaceSectionChecked_StaleHashIsRefusedAndFileUntouched(t *testing.T) {
	path := writeFixtureSpec(t)

	_, staleHash, err := SectionContentHash(path, "problem_statement")
	if err != nil {
		t.Fatal(err)
	}

	// Someone else edits the section.
	if _, err := ReplaceSectionChecked(path, "problem_statement", "an edit by a human", ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ReplaceSectionChecked(path, "problem_statement", "an agent clobber", staleHash)
	if err == nil {
		t.Fatal("a stale hash must be refused")
	}

	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error should be a *ConflictError so callers can react structurally, got %T", err)
	}
	if conflict.Slug != "problem_statement" {
		t.Errorf("conflict should name the section, got %q", conflict.Slug)
	}
	if !strings.Contains(conflict.CurrentContent, "an edit by a human") {
		t.Error("conflict should carry the current content so a caller can show what it would have overwritten")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a refused write must leave the file byte-identical")
	}
}

// The CLI has a human in the loop rather than a hash, so an empty base hash
// skips the check.
func TestReplaceSectionChecked_EmptyBaseHashSkipsCheck(t *testing.T) {
	path := writeFixtureSpec(t)
	if _, err := ReplaceSectionChecked(path, "problem_statement", "unchecked write", ""); err != nil {
		t.Fatalf("an empty base hash should skip the check: %v", err)
	}
}

func TestConflictError_MessageNamesNextAction(t *testing.T) {
	err := &ConflictError{Slug: "problem_statement", ExpectedHash: "sha256:aaaaaaaabbbb", ActualHash: "sha256:ccccccccdddd"}
	msg := err.Error()
	if !strings.Contains(msg, "problem_statement") {
		t.Errorf("message should name the section: %q", msg)
	}
	if !strings.Contains(msg, "re-read") {
		t.Errorf("message should say what to do next: %q", msg)
	}
}
