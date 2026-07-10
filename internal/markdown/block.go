package markdown

import (
	"regexp"
	"strings"
)

// BlockRange is one source block, expressed as [StartLine, EndLine). Blocks
// are the units the reader can quote: paragraphs, individual list items,
// individual table rows, fenced blocks, and blockquote paragraphs.
type BlockRange struct {
	StartLine int
	EndLine   int
}

var (
	listItemLine = regexp.MustCompile(`^\s*(?:[-+*]|\d+[.)])\s+`)
	tableSepLine = regexp.MustCompile(`^\s*\|?\s*:?-{3,}`)
)

// BlockRanges splits markdown into stable reader anchor units. This is a
// source-oriented scanner rather than a renderer concern; it deliberately
// keeps list items and table rows separate even without blank lines.
func BlockRanges(source string) []BlockRange {
	lines := strings.Split(source, "\n")
	var out []BlockRange
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || tableSepLine.MatchString(trimmed) {
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			// The block spans the fence's content only: marker lines (and the
			// info string, e.g. "```go") never appear in rendered output, so
			// including them would desync source and rendered token streams.
			marker := trimmed[:3]
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), marker) {
				end++
			}
			if end > i+1 {
				out = append(out, BlockRange{StartLine: i + 1, EndLine: end})
			}
			if end < len(lines) {
				end++ // step past the closing marker
			}
			i = end
			continue
		}
		if listItemLine.MatchString(lines[i]) || isTableRow(lines[i]) {
			out = append(out, BlockRange{StartLine: i, EndLine: i + 1})
			i++
			continue
		}
		start := i
		i++
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "" || listItemLine.MatchString(lines[i]) ||
				isTableRow(lines[i]) || strings.HasPrefix(strings.TrimSpace(lines[i]), "```") ||
				strings.HasPrefix(strings.TrimSpace(lines[i]), "~~~") {
				break
			}
			i++
		}
		out = append(out, BlockRange{StartLine: start, EndLine: i})
	}
	return out
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.Count(trimmed, "|") >= 2
}
