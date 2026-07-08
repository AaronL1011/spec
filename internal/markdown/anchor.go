package markdown

import (
	"strings"
	"unicode"
)

// AnchorMatch locates a quote within section content.
type AnchorMatch struct {
	Found bool
	Line  int // 0-based line within the section body where the quote starts
	Col   int // 0-based byte offset within that line
}

// AnchorToken is one normalised word of a text, tagged with its position in
// the original source so a token match can be mapped back to a line. The
// reader's rendered-side anchor mapping tokenises Glamour output through the
// same function, which is what makes source and rendered matching agree.
type AnchorToken struct {
	Text string
	Line int
	Col  int
}

// ResolveAnchor finds quote (disambiguated by prefix) within sectionBody.
// Matching is whitespace-normalised and markdown-tolerant (emphasis and
// punctuation at token edges are ignored) so reflowed or lightly reworded
// markdown still resolves. When the quote is absent, Found is false and the
// caller anchors to the section — a miss is a graceful degrade, never an
// error.
func ResolveAnchor(sectionBody, quote, prefix string) AnchorMatch {
	body := TokenizeAnchor(sectionBody)
	idx, ok := ResolveAnchorTokens(body, quote, prefix)
	if !ok {
		return AnchorMatch{}
	}
	return AnchorMatch{Found: true, Line: body[idx].Line, Col: body[idx].Col}
}

// ResolveAnchorTokens finds quote (disambiguated by prefix) within a
// pre-tokenised stream, returning the index of the first matched token.
// It reports ok=false when the quote has no alphanumeric content or does not
// occur in the stream.
func ResolveAnchorTokens(body []AnchorToken, quote, prefix string) (int, bool) {
	want := tokenTexts(TokenizeAnchor(quote))
	if len(want) == 0 || len(body) < len(want) {
		return 0, false
	}
	matches := findTokenRuns(body, want)
	if len(matches) == 0 {
		return 0, false
	}
	idx := matches[0]
	if len(matches) > 1 {
		idx = disambiguateByPrefix(body, matches, prefix)
	}
	return idx, true
}

// findTokenRuns returns every index in body where want occurs contiguously.
func findTokenRuns(body []AnchorToken, want []string) []int {
	var matches []int
	for i := 0; i+len(want) <= len(body); i++ {
		ok := true
		for j, w := range want {
			if body[i+j].Text != w {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
	}
	return matches
}

// disambiguateByPrefix picks the match whose preceding tokens end with the
// prefix token stream, falling back to the first match when none (or no
// prefix) qualifies.
func disambiguateByPrefix(body []AnchorToken, matches []int, prefix string) int {
	pre := tokenTexts(TokenizeAnchor(prefix))
	if len(pre) == 0 {
		return matches[0]
	}
	for _, m := range matches {
		if m < len(pre) {
			continue
		}
		ok := true
		for j, p := range pre {
			if body[m-len(pre)+j].Text != p {
				ok = false
				break
			}
		}
		if ok {
			return m
		}
	}
	return matches[0]
}

// TokenizeAnchor tokenises text into normalised words with source positions.
// Normalisation trims non-alphanumeric runes from token edges (so `**bold**`,
// “ `code` “ and "word," all match their plain forms) and drops tokens with
// no alphanumeric content (list bullets, rules, table borders).
func TokenizeAnchor(text string) []AnchorToken {
	var out []AnchorToken
	for lineIdx, line := range strings.Split(text, "\n") {
		col := 0
		for col < len(line) {
			// Skip whitespace.
			for col < len(line) && (line[col] == ' ' || line[col] == '\t') {
				col++
			}
			start := col
			for col < len(line) && line[col] != ' ' && line[col] != '\t' {
				col++
			}
			if start == col {
				continue
			}
			if norm := normalizeAnchorToken(line[start:col]); norm != "" {
				out = append(out, AnchorToken{Text: norm, Line: lineIdx, Col: start})
			}
		}
	}
	return out
}

// normalizeAnchorToken trims non-alphanumeric runes from both ends of a raw
// token and returns "" when nothing alphanumeric remains.
func normalizeAnchorToken(raw string) string {
	return strings.TrimFunc(raw, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// tokenTexts projects tokens to their normalised text.
func tokenTexts(toks []AnchorToken) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}
