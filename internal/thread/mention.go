package thread

import (
	"regexp"
	"strings"
)

// mentionPattern matches an @handle at a word boundary. A handle is
// [A-Za-z0-9_.:-]+ — the colon is deliberate, not just word characters:
// agent identities are "agent:<adapter>" (e.g. "agent:claude"), and they must
// be mentionable exactly like a human handle. A narrower grammar would
// silently truncate "@agent:claude" to "agent" and drop ":claude".
//
// The negative lookbehind an RE2 regexp can't express is approximated by
// requiring the character before '@' to be absent or a non-identifier rune;
// ParseMentions post-filters matches immediately preceded by an identifier
// character (e.g. "user@example.com") so email addresses and mid-word '@'
// are ignored.
var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9_.:-]+)`)

// ParseMentions extracts @handle tokens from a body in first-seen order,
// deduplicated, with the leading @ stripped. A handle is [A-Za-z0-9_.:-]+
// following an @ at a word boundary. Email addresses and mid-word @ are
// ignored.
func ParseMentions(body string) []string {
	var out []string
	seen := make(map[string]bool)

	matches := mentionPattern.FindAllStringSubmatchIndex(body, -1)
	for _, m := range matches {
		start, handleStart, handleEnd := m[0], m[2], m[3]
		if start > 0 && isIdentifierByte(body[start-1]) {
			// The '@' is preceded by an identifier character — this is a
			// mid-word '@' or an email address ("user@example.com"), not a
			// mention.
			continue
		}
		handle := body[handleStart:handleEnd]
		key := handle
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, handle)
	}
	return out
}

// unionMentions combines mentions parsed from a body with explicit handles
// supplied by the caller (e.g. a --to flag), in first-seen order
// (parsed-from-body first), deduplicated case-insensitively and tolerant of a
// leading '@' — "bob" and "@bob" are the same handle.
func unionMentions(parsed, explicit []string) []string {
	if len(parsed) == 0 && len(explicit) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(parsed)+len(explicit))
	var out []string
	add := func(handle string) {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			return
		}
		key := strings.ToLower(strings.TrimPrefix(handle, "@"))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, handle)
	}
	for _, h := range parsed {
		add(h)
	}
	for _, h := range explicit {
		add(h)
	}
	return out
}

// isIdentifierByte reports whether b is a character that can appear inside a
// handle or a local-part/domain of an email address — used to detect that an
// '@' is not at a mention's word boundary.
func isIdentifierByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	case b == '_' || b == '.' || b == '-':
		return true
	}
	return false
}
