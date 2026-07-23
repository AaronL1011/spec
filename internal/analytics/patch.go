package analytics

import (
	"regexp"
	"strconv"
	"strings"
)

// statusChange is a frontmatter `status:` transition observed in one commit's
// diff for one spec file.
type statusChange struct {
	specID string
	from   string
	to     string
	added  bool // file created in this commit (scaffold)
}

// frontmatterWindow bounds how deep into a file a hunk may start and still be
// treated as frontmatter. Guards against `status:` lines inside code blocks
// in the spec body being mistaken for stage transitions.
const frontmatterWindow = 40

var (
	// reDiffHeader captures the b/ side path of a per-file diff header.
	reDiffHeader = regexp.MustCompile(`^diff --git a/.* b/(.+)$`)
	// reSpecFile matches spec markdown files (including archived copies) and
	// captures the spec ID. Sidecars (.threads.yaml) and triage files never match.
	reSpecFile = regexp.MustCompile(`(?:^|/)(SPEC-\d+)\.md$`)
	// reHunkOldStart captures the old-file start line of a hunk header.
	reHunkOldStart = regexp.MustCompile(`^@@ -(\d+)`)
)

// filePatch accumulates status observations while walking one file's diff.
type filePatch struct {
	specID  string
	newFile bool
	from    string
	fromSet bool
	to      string
	toSet   bool
	inFront bool // current hunk starts within the frontmatter window
}

// parsePatchStatusChanges walks a commit's unified diff and returns the
// frontmatter status transitions per spec file. unreadable counts spec files
// whose status change could not be fully resolved (e.g. a new status with no
// old side on a pre-existing file).
func parsePatchStatusChanges(patch string) (changes []statusChange, unreadable int) {
	if patch == "" {
		return nil, 0
	}
	var cur *filePatch
	flush := func() {
		if cur == nil {
			return
		}
		if ch, bad := cur.change(); bad {
			unreadable++
		} else if ch != nil {
			changes = append(changes, *ch)
		}
		cur = nil
	}

	for _, line := range strings.Split(patch, "\n") {
		if m := reDiffHeader.FindStringSubmatch(line); m != nil {
			flush()
			if sm := reSpecFile.FindStringSubmatch(m[1]); sm != nil {
				cur = &filePatch{specID: sm[1]}
			}
			continue
		}
		if cur != nil {
			cur.observe(line)
		}
	}
	flush()
	return changes, unreadable
}

// observe updates the file's accumulated state from one diff line.
func (f *filePatch) observe(line string) {
	switch {
	case strings.HasPrefix(line, "new file mode"):
		f.newFile = true
	case strings.HasPrefix(line, "@@ "):
		f.inFront = hunkInFrontmatter(line)
	case !f.inFront:
		// Ignore body hunks entirely.
	case strings.HasPrefix(line, "-status: "):
		f.from = strings.TrimSpace(strings.TrimPrefix(line, "-status: "))
		f.fromSet = true
	case strings.HasPrefix(line, "+status: "):
		f.to = strings.TrimSpace(strings.TrimPrefix(line, "+status: "))
		f.toSet = true
	}
}

// change resolves the accumulated observations into a statusChange, or
// reports the file as unreadable when the observations are inconsistent.
func (f *filePatch) change() (*statusChange, bool) {
	switch {
	case !f.toSet:
		// No status added: pure rename, body edit, or deletion. Not an event.
		return nil, false
	case f.newFile:
		return &statusChange{specID: f.specID, to: f.to, added: true}, false
	case !f.fromSet:
		// Status appeared without an old side on an existing file — the
		// frontmatter was restructured in a way we cannot attribute.
		return nil, true
	case f.from == f.to:
		return nil, false
	default:
		return &statusChange{specID: f.specID, from: f.from, to: f.to}, false
	}
}

// hunkInFrontmatter reports whether a hunk header starts within the
// frontmatter window of the old file.
func hunkInFrontmatter(header string) bool {
	m := reHunkOldStart.FindStringSubmatch(header)
	if m == nil {
		return false
	}
	start, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	return start <= frontmatterWindow
}
