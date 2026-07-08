package tui

import (
	"regexp"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

// anchorPrefixTokens is how many tokens of preceding context are captured as
// a QuotePrefix when the picker creates a quoted thread.
const anchorPrefixTokens = 5

// ansiPattern matches CSI escape sequences (colours, cursor movement) and
// OSC sequences (hyperlinks) so rendered output can be reduced to plain text
// before token matching.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// stripANSI removes terminal escape sequences from s.
func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// anchorMap maps threads to rendered-line positions within one section's
// rendered output, and rendered lines back to source blocks. It is pure —
// built from strings, no model state — and is rebuilt whenever the rendered
// content or the thread set changes (never inside view()).
//
// The load-bearing assumption is that the renderer reflows text but preserves
// token content and order, so a whitespace-normalised token stream matches on
// both sides of the render. Do not replace this with source mutation: marker
// bytes injected into markdown corrupt block syntax and poison the render
// cache (see docs/discussion-03-reader-cockpit.md §2.3).
type anchorMap struct {
	lines   map[string]int // thread ID → 0-based rendered line
	counts  map[int]int    // rendered line → anchored thread count
	srcBody string
	renToks []markdown.AnchorToken // tokens of the ANSI-stripped rendered content
}

// buildAnchorMap resolves each thread's quote against the raw section body
// (existence + disambiguation), then locates it in the rendered output by
// token matching. Threads without a quote, or whose quote no longer resolves,
// simply have no entry — they degrade to the section-level treatment.
func buildAnchorMap(sectionBody, rendered string, threads []thread.Thread) anchorMap {
	am := anchorMap{
		lines:   make(map[string]int),
		counts:  make(map[int]int),
		srcBody: sectionBody,
		renToks: markdown.TokenizeAnchor(stripANSI(rendered)),
	}
	for _, t := range threads {
		if t.Quote == "" {
			continue
		}
		// Source side first: the quote must still exist in the section.
		if src := markdown.ResolveAnchor(sectionBody, t.Quote, t.QuotePrefix); !src.Found {
			continue
		}
		idx, ok := markdown.ResolveAnchorTokens(am.renToks, t.Quote, t.QuotePrefix)
		if !ok {
			continue
		}
		line := am.renToks[idx].Line
		am.lines[t.ID] = line
		am.counts[line]++
	}
	return am
}

// renderedLineFor returns the rendered line a thread anchors to, ok=false on
// a degrade-to-section miss.
func (am anchorMap) renderedLineFor(threadID string) (int, bool) {
	line, ok := am.lines[threadID]
	return line, ok
}

// countAt returns how many threads anchor to a rendered line (0 when none).
func (am anchorMap) countAt(renderedLine int) int { return am.counts[renderedLine] }

// sourceBlockAt maps a rendered line back to its source block — the reverse
// direction, used by the anchor picker. The returned quote is the raw text of
// the source block containing the match; prefix is a short run of preceding
// tokens for disambiguation. ok=false when the rendered line carries no
// matchable text (chrome, rules, blank lines).
func (am anchorMap) sourceBlockAt(renderedLine int) (quote, prefix string, ok bool) {
	var want []string
	for _, tok := range am.renToks {
		if tok.Line == renderedLine {
			want = append(want, tok.Text)
		}
	}
	if len(want) == 0 {
		return "", "", false
	}

	srcToks := markdown.TokenizeAnchor(am.srcBody)
	idx, ok := markdown.ResolveAnchorTokens(srcToks, strings.Join(want, " "), "")
	if !ok {
		return "", "", false
	}
	srcLine := srcToks[idx].Line

	srcLines := strings.Split(am.srcBody, "\n")
	start, end := blockBounds(srcLines, srcLine)
	quote = strings.TrimSpace(strings.Join(srcLines[start:end], "\n"))
	if quote == "" {
		return "", "", false
	}

	// Prefix: the last few tokens before the block, normalised.
	var pre []string
	for _, tok := range srcToks {
		if tok.Line < start {
			pre = append(pre, tok.Text)
		}
	}
	if len(pre) > anchorPrefixTokens {
		pre = pre[len(pre)-anchorPrefixTokens:]
	}
	return quote, strings.Join(pre, " "), true
}

// blockBounds expands from a line to its enclosing block: the contiguous run
// of non-blank lines around it. Returns [start, end) line indexes.
func blockBounds(lines []string, at int) (int, int) {
	if at < 0 || at >= len(lines) {
		return 0, 0
	}
	start := at
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	end := at + 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	return start, end
}
