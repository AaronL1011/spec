package tui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

const anchorPrefixTokens = 5

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

type anchorLineState struct {
	Count       int
	Selected    bool
	Unread      bool
	AllResolved bool
}

type anchorMap struct {
	lines     map[string]int
	states    map[int]anchorLineState
	ambiguous map[string]bool
	pickLines []int
	srcBody   string
	renToks   []markdown.AnchorToken
}

func buildAnchorMap(sectionBody, rendered string, threads []thread.Thread) anchorMap {
	return buildAnchorMapState(sectionBody, rendered, threads, "", func(thread.Thread) bool { return false })
}

func buildAnchorMapState(sectionBody, rendered string, threads []thread.Thread, selectedID string, unread func(thread.Thread) bool) anchorMap {
	am := anchorMap{
		lines: make(map[string]int), states: make(map[int]anchorLineState),
		ambiguous: make(map[string]bool), srcBody: sectionBody,
		renToks: markdown.TokenizeAnchor(stripANSI(rendered)),
	}
	for _, t := range threads {
		if t.Quote == "" {
			continue
		}
		// Source-side existence stays strict: a drifted quote must degrade.
		source := markdown.ResolveAnchor(sectionBody, t.Quote, t.QuotePrefix)
		if !source.Found {
			am.ambiguous[t.ID] = source.Ambiguous
			continue
		}
		// Rendered-side location is loose: Glamour clips long code lines and
		// wide table cells, so a live quote's tail may never render.
		idx, ok, ambiguous := markdown.ResolveAnchorTokensLoose(am.renToks, t.Quote, t.QuotePrefix)
		if !ok {
			am.ambiguous[t.ID] = ambiguous
			continue
		}
		line := am.renToks[idx].Line
		am.lines[t.ID] = line
		state := am.states[line]
		state.Count++
		state.Selected = state.Selected || t.ID == selectedID
		state.Unread = state.Unread || unread(t)
		state.AllResolved = (state.Count == 1 || state.AllResolved) && !t.IsOpen()
		am.states[line] = state
	}
	am.pickLines = am.buildPickLines()
	return am
}

func (am anchorMap) renderedLineFor(threadID string) (int, bool) {
	line, ok := am.lines[threadID]
	return line, ok
}

func (am anchorMap) stateAt(line int) (anchorLineState, bool) {
	state, ok := am.states[line]
	return state, ok
}

func (am anchorMap) countAt(line int) int { return am.states[line].Count }

func (am anchorMap) isAmbiguous(threadID string) bool { return am.ambiguous[threadID] }

// buildPickLines maps every source block to a rendered line. Matching is
// loose (head-of-block) because Glamour truncates long code lines and wide
// table cells — a block must stay pickable even when its tail never renders.
// The result is sorted so stepPickLine moves monotonically down the screen
// regardless of source-block order versus rendered order.
func (am anchorMap) buildPickLines() []int {
	seen := make(map[int]bool)
	var out []int
	for _, block := range markdown.BlockRanges(am.srcBody) {
		quote := blockText(am.srcBody, block)
		idx, ok, _ := markdown.ResolveAnchorTokensLoose(am.renToks, quote, prefixForBlock(am.srcBody, block.StartLine))
		if !ok {
			continue
		}
		line := am.renToks[idx].Line
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	sort.Ints(out)
	return out
}

func (am anchorMap) nearestPickLine(line int) (int, bool) {
	if len(am.pickLines) == 0 {
		return 0, false
	}
	best, distance := am.pickLines[0], absInt(am.pickLines[0]-line)
	for _, candidate := range am.pickLines[1:] {
		if d := absInt(candidate - line); d < distance {
			best, distance = candidate, d
		}
	}
	return best, true
}

func (am anchorMap) stepPickLine(current, delta int) int {
	if len(am.pickLines) == 0 {
		return current
	}
	idx := 0
	for i, line := range am.pickLines {
		if line >= current {
			idx = i
			break
		}
		idx = i
	}
	idx = clampInt(idx+delta, 0, len(am.pickLines)-1)
	return am.pickLines[idx]
}

func (am anchorMap) sourceBlockAt(renderedLine int) (quote, prefix string, ok bool) {
	for _, block := range markdown.BlockRanges(am.srcBody) {
		candidate := blockText(am.srcBody, block)
		pre := prefixForBlock(am.srcBody, block.StartLine)
		idx, found, _ := markdown.ResolveAnchorTokensLoose(am.renToks, candidate, pre)
		if found && am.renToks[idx].Line == renderedLine {
			return candidate, pre, true
		}
	}
	return "", "", false
}

func blockText(body string, block markdown.BlockRange) string {
	lines := strings.Split(body, "\n")
	return strings.TrimSpace(strings.Join(lines[block.StartLine:block.EndLine], "\n"))
}

func prefixForBlock(body string, startLine int) string {
	var tokens []string
	for _, token := range markdown.TokenizeAnchor(body) {
		if token.Line < startLine {
			tokens = append(tokens, token.Text)
		}
	}
	if len(tokens) > anchorPrefixTokens {
		tokens = tokens[len(tokens)-anchorPrefixTokens:]
	}
	return strings.Join(tokens, " ")
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
