package hierarchy

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
)

// inheritedSlugs are the parent sections a deliverable slice inherits, in the
// order they are rendered.
//
// The set is deliberately bounded to the *why*. The parent's §5 acceptance
// criteria and §7 technical implementation describe nothing this slice should
// build, and an agent handed the initiative's §7 will happily implement the
// wrong slice. Excluding them is the difference between inherited rationale
// and a second, conflicting work order.
var inheritedSlugs = []struct {
	slug    string
	heading string
}{
	{"tl_dr", "TL;DR"},
	{"problem_statement", "1. Problem Statement"},
	{"proposed_solution", "4. Proposed Solution"},
}

// Delimiters for the inherited block. The instruction lives in the markers
// because prompt text is the only enforcement available: nothing downstream
// can stop an agent implementing what it is shown, so the block must say
// plainly what it is for.
const (
	inheritedOpenFmt = "<!-- inherited from %s — read-only context, do not implement -->"
	inheritedClose   = "<!-- end inherited context -->"
)

// InheritedContext renders the read-only initiative block a deliverable slice
// carries into its agent context: the parent's TL;DR, problem statement and
// proposed solution, delimited and labelled.
//
// It returns "" when the parent has none of those sections filled in, so an
// empty initiative contributes an empty block rather than a heading with
// nothing under it.
func InheritedContext(parentID, parentTitle, parentContent string) string {
	sections := markdown.ExtractSections(markdown.Body(parentContent))

	var body strings.Builder
	for _, want := range inheritedSlugs {
		sec := markdown.FindSection(sections, want.slug)
		if sec == nil || strings.TrimSpace(sec.Content) == "" {
			continue
		}
		fmt.Fprintf(&body, "### %s\n\n%s\n\n", want.heading, strings.TrimSpace(sec.Content))
	}
	if body.Len() == 0 {
		return ""
	}

	title := parentID
	if parentTitle != "" {
		title = parentID + ": " + parentTitle
	}
	var out strings.Builder
	fmt.Fprintf(&out, inheritedOpenFmt+"\n", parentID)
	fmt.Fprintf(&out, "## Initiative context — %s\n\n", title)
	out.WriteString(body.String())
	out.WriteString(inheritedClose + "\n")
	return out.String()
}

// InheritedContextFor resolves specID's initiative and renders its inherited
// block. It returns "" for a standalone spec, an unresolvable parent, or a
// parent whose file cannot be read — inherited context is an enhancement, and
// no build may fail because the initiative was unreadable.
func (g *Graph) InheritedContextFor(specID string) string {
	parent, ok := g.Parent(specID)
	if !ok || parent.Path == "" {
		return ""
	}
	data, err := os.ReadFile(parent.Path)
	if err != nil {
		return ""
	}
	return InheritedContext(parent.ID, parent.Title, string(data))
}
