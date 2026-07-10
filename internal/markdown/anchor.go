package markdown

import (
	"strings"
	"unicode"
)

// AnchorMatch locates a quote within section content. Ambiguous means the
// quote exists more than once and its prefix did not identify one occurrence.
type AnchorMatch struct {
	Found     bool
	Ambiguous bool
	Line      int
	Col       int
}

// AnchorToken is one normalised word tagged with its original position.
type AnchorToken struct {
	Text string
	Line int
	Col  int
}

// ResolveAnchor finds quote within sectionBody. An ambiguous match is a
// graceful miss: callers degrade to the section rather than silently choosing
// the first occurrence.
func ResolveAnchor(sectionBody, quote, prefix string) AnchorMatch {
	body := TokenizeAnchor(sectionBody)
	idx, ok, ambiguous := ResolveAnchorTokens(body, quote, prefix)
	if !ok {
		return AnchorMatch{Ambiguous: ambiguous}
	}
	return AnchorMatch{Found: true, Line: body[idx].Line, Col: body[idx].Col}
}

// ResolveAnchorTokens finds one uniquely identified quote occurrence.
//
// Matching runs over a concatenated character stream of the normalised
// tokens rather than token-by-token: renderers move word boundaries (inline
// code gains padding spaces, long words reflow), so `n`/`p` may tokenise as
// one word in markdown source and two in rendered output. Character-stream
// matching is immune to boundary drift while staying whitespace-, markup-,
// and case-insensitive.
func ResolveAnchorTokens(body []AnchorToken, quote, prefix string) (idx int, ok, ambiguous bool) {
	stream := newAnchorStream(body)
	want := concatTokens(TokenizeAnchor(quote))
	if want == "" || len(stream.text) < len(want) {
		return 0, false, false
	}
	matches := stream.occurrences(want)
	switch len(matches) {
	case 0:
		return 0, false, false
	case 1:
		return stream.tokenAt(matches[0]), true, false
	}
	matched := stream.prefixMatches(matches, prefix)
	if len(matched) == 1 {
		return stream.tokenAt(matched[0]), true, false
	}
	return 0, false, true
}

// looseMinChars is the shortest quote head ResolveAnchorTokensLoose will try
// before giving up — short enough to survive aggressive truncation, long
// enough to keep accidental matches unlikely.
const looseMinChars = 16

// ResolveAnchorTokensLoose matches like ResolveAnchorTokens but tolerates
// renderer-side truncation: Glamour clips long code lines and ellipsises wide
// table cells, so a block's full text may be absent from rendered output even
// though its head is on screen. It binary-searches the longest head of the
// quote that still occurs (occurrence is monotone in head length) and accepts
// it when the head is either the whole quote or long enough to be
// distinctive. Ambiguity still degrades — the head must identify exactly one
// position.
func ResolveAnchorTokensLoose(body []AnchorToken, quote, prefix string) (idx int, ok, ambiguous bool) {
	stream := newAnchorStream(body)
	want := concatTokens(TokenizeAnchor(quote))
	if want == "" || len(stream.occurrences(want[:1])) == 0 {
		return 0, false, false
	}
	lo, hi := 1, len(want)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if len(stream.occurrences(want[:mid])) > 0 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo < min(looseMinChars, len(want)) {
		return 0, false, false // head too short to be distinctive
	}
	matches := stream.occurrences(want[:lo])
	if len(matches) == 1 {
		return stream.tokenAt(matches[0]), true, false
	}
	if matched := stream.prefixMatches(matches, prefix); len(matched) == 1 {
		return stream.tokenAt(matched[0]), true, false
	}
	return 0, false, true
}

// anchorStream is the concatenated normalised text of a token slice, with a
// per-character map back to the originating token.
type anchorStream struct {
	text   string
	tokIdx []int
}

func newAnchorStream(body []AnchorToken) anchorStream {
	var b strings.Builder
	var tokIdx []int
	for i, tok := range body {
		b.WriteString(tok.Text)
		for range len(tok.Text) {
			tokIdx = append(tokIdx, i)
		}
	}
	return anchorStream{text: b.String(), tokIdx: tokIdx}
}

func (s anchorStream) occurrences(want string) []int {
	var out []int
	from := 0
	for {
		at := strings.Index(s.text[from:], want)
		if at < 0 {
			return out
		}
		out = append(out, from+at)
		from += at + 1
	}
}

func (s anchorStream) tokenAt(charPos int) int {
	if charPos < 0 || charPos >= len(s.tokIdx) {
		return 0
	}
	return s.tokIdx[charPos]
}

func (s anchorStream) prefixMatches(matches []int, prefix string) []int {
	pre := concatTokens(TokenizeAnchor(prefix))
	if pre == "" {
		return nil
	}
	var out []int
	for _, match := range matches {
		if match >= len(pre) && s.text[match-len(pre):match] == pre {
			out = append(out, match)
		}
	}
	return out
}

// TokenizeAnchor tokenises text into normalised words with source positions.
// Normalisation folds case and drops every non-alphanumeric rune — interior
// ones included — so markdown inline markup (`n`/`p`, **bold**, smart quotes)
// tokenises identically on the source and rendered sides of a Glamour render.
func TokenizeAnchor(text string) []AnchorToken {
	var out []AnchorToken
	for lineIdx, line := range strings.Split(text, "\n") {
		col := 0
		for col < len(line) {
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

func normalizeAnchorToken(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func concatTokens(toks []AnchorToken) string {
	var b strings.Builder
	for _, token := range toks {
		b.WriteString(token.Text)
	}
	return b.String()
}
