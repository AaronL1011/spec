package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
)

// authoring-port/v1
//
// These tools let an agent session author specs through spec's own engines, so an
// agent's edits pass the same validation, formatting, and section structure as a
// human's — rather than the agent editing markdown directly and bypassing all of
// it, which is what happens today.
//
// Tools are tiered by authority (decision 010). The authoring tier below is
// available in any agent session: section writes are what make the port useful,
// and every one is recoverable from the specs-repo diff. Stage transitions are a
// separate tier because they are not symmetric with writes — they fire
// team-visible pipeline effects — and they are absent from tools/list unless
// explicitly enabled.
//
// Port discipline matches build-port/v1: versioned schema, idempotent where
// re-runnable, additive-only within a major version, unknown optional fields
// tolerated, and the same engines as the CLI with no parallel logic and no gate
// bypass.

// authoringTools returns the always-available authoring tier.
func authoringTools() []Tool {
	return []Tool{
		{
			Name: "spec_section_read",
			Description: "Read one section of a spec with its content_hash. " +
				"Pass that hash to spec_section_write to avoid overwriting concurrent edits.",
			InputSchema: objectSchema(map[string]interface{}{
				"id":   stringProp("Spec ID (e.g., 'SPEC-042')"),
				"slug": stringProp("Section slug (e.g., 'problem_statement')"),
			}, "id", "slug"),
		},
		{
			Name: "spec_section_write",
			Description: "Replace a section's body through the markdown engine (validated, formatted, logged). " +
				"Supply base_hash from spec_section_read; the write fails if the section changed since. " +
				"Returns the new content_hash so consecutive writes need no re-read.",
			InputSchema: objectSchema(map[string]interface{}{
				"id":        stringProp("Spec ID (e.g., 'SPEC-042')"),
				"slug":      stringProp("Section slug (e.g., 'problem_statement')"),
				"content":   stringProp("New section body in markdown, without the heading"),
				"base_hash": stringProp("content_hash from spec_section_read; omit only for a blind write"),
			}, "id", "slug", "content"),
		},
		{
			Name: "spec_acceptance_add",
			Description: "Append an acceptance criterion to §6. Idempotent: an identical criterion " +
				"already present is a no-op success.",
			InputSchema: objectSchema(map[string]interface{}{
				"id":        stringProp("Spec ID (e.g., 'SPEC-042')"),
				"criterion": stringProp("A single testable criterion, without the checkbox prefix"),
			}, "id", "criterion"),
		},
		{
			Name: "spec_meta_update",
			Description: "Update constrained frontmatter fields (title, assignees, repos). " +
				"Status is deliberately not updatable — use the transition tools, which run the gates.",
			InputSchema: objectSchema(map[string]interface{}{
				"id":        stringProp("Spec ID (e.g., 'SPEC-042')"),
				"title":     stringProp("New title"),
				"assignees": stringProp("Comma-separated assignee handles"),
				"repos":     stringProp("Comma-separated repo names this spec targets"),
			}, "id"),
		},
	}
}

// objectSchema builds a JSON-schema object with the given properties and
// required fields.
func objectSchema(props map[string]interface{}, required ...string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

// toolSectionRead returns a section's body and its content hash.
func (h *GenericHandler) toolSectionRead(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	specID := strings.ToUpper(params.ID)

	body, hash, err := markdown.SectionContentHash(h.specPath(specID), params.Slug)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	// The hash is returned in the message body because ToolResult carries no
	// structured payload; the description tells the agent to pass it back.
	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("content_hash: %s\n\n%s", hash, body),
	}, nil
}

// toolSectionWrite replaces a section body with optimistic concurrency.
func (h *GenericHandler) toolSectionWrite(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		Content  string `json:"content"`
		BaseHash string `json:"base_hash"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	specID := strings.ToUpper(params.ID)

	if !markdown.IsValidSectionSlug(params.Slug) {
		return &ToolResult{
			Success: false,
			Message: fmt.Sprintf("unknown section %q — valid slugs: %s", params.Slug, strings.Join(markdown.ValidSectionSlugs(), ", ")),
		}, nil
	}

	newHash, err := markdown.ReplaceSectionChecked(h.specPath(specID), params.Slug, params.Content, params.BaseHash)
	if err != nil {
		// A conflict is a structured, expected outcome rather than a failure of
		// the tool: it names the section and tells the agent what to do next.
		var conflict *markdown.ConflictError
		if errors.As(err, &conflict) {
			return &ToolResult{
				Success: false,
				Message: fmt.Sprintf("CONFLICT: %s\n\nCurrent content:\n%s", conflict.Error(), conflict.CurrentContent),
			}, nil
		}
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	// Notify only after a successful write, so a rejected write never triggers a
	// push.
	h.publisher.Notify(specID)
	h.logAgentWrite(specID, "section_write", fmt.Sprintf("agent wrote §%s", params.Slug))

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Wrote %s §%s. content_hash: %s", specID, params.Slug, newHash),
	}, nil
}

// toolAcceptanceAdd appends a criterion to the acceptance-criteria section.
//
// Idempotency matters here because an agent that loses track mid-conversation
// will re-run this: a duplicate criterion is noise a human then has to clean up,
// so an identical line already present is a no-op success rather than a second
// entry.
func (h *GenericHandler) toolAcceptanceAdd(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID        string `json:"id"`
		Criterion string `json:"criterion"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	specID := strings.ToUpper(params.ID)
	criterion := strings.TrimSpace(params.Criterion)
	if criterion == "" {
		return &ToolResult{Success: false, Message: "criterion is empty"}, nil
	}

	path := h.specPath(specID)
	body, _, err := markdown.SectionContentHash(path, "acceptance_criteria")
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	// Compare on the criterion text, ignoring checkbox state, so a criterion the
	// user has already ticked is still recognised as present.
	if containsCriterion(body, criterion) {
		return &ToolResult{
			Success: true,
			Message: fmt.Sprintf("Criterion already present in %s §6 — no change", specID),
		}, nil
	}

	line := "- [ ] " + criterion
	updated := strings.TrimRight(body, "\n")
	if strings.TrimSpace(updated) == "" {
		updated = line
	} else {
		updated += "\n" + line
	}

	if _, err := markdown.ReplaceSectionChecked(path, "acceptance_criteria", updated, ""); err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}
	h.publisher.Notify(specID)
	h.logAgentWrite(specID, "acceptance_add", "agent added an acceptance criterion")

	return &ToolResult{Success: true, Message: fmt.Sprintf("Added criterion to %s §6", specID)}, nil
}

// containsCriterion reports whether a criterion is already listed, ignoring
// checkbox state and surrounding whitespace.
func containsCriterion(body, criterion string) bool {
	want := normaliseCriterion(criterion)
	for _, line := range strings.Split(body, "\n") {
		if normaliseCriterion(line) == want {
			return true
		}
	}
	return false
}

// normaliseCriterion strips list and checkbox markers so comparison is on the
// text alone.
func normaliseCriterion(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimSpace(s)
	for _, box := range []string{"[ ]", "[x]", "[X]"} {
		s = strings.TrimPrefix(s, box)
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// toolMetaUpdate applies constrained frontmatter changes.
//
// Status is deliberately absent: moving a spec is a gated transition with
// team-visible effects, so allowing it here would be a way around the gates that
// the transition tier exists to enforce. The allowed set is a fixed list rather
// than a passthrough map, so a future frontmatter field is not silently writable
// by an agent the day it is added.
func (h *GenericHandler) toolMetaUpdate(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Assignees string `json:"assignees"`
		Repos     string `json:"repos"`
		// Accepted only to reject them with an explanation, rather than silently
		// ignoring fields an agent may reasonably try.
		Status string `json:"status"`
		Stage  string `json:"stage"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Status) != "" || strings.TrimSpace(params.Stage) != "" {
		return &ToolResult{
			Success: false,
			Message: "stage/status cannot be set through spec_meta_update — use spec_advance/spec_revert, which run the pipeline's gates",
		}, nil
	}

	specID := strings.ToUpper(params.ID)
	path := h.specPath(specID)

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	var changed []string
	if t := strings.TrimSpace(params.Title); t != "" {
		meta.Title = t
		changed = append(changed, "title")
	}
	if a := strings.TrimSpace(params.Assignees); a != "" {
		meta.Assignees = splitList(a)
		changed = append(changed, "assignees")
	}
	if r := strings.TrimSpace(params.Repos); r != "" {
		meta.Repos = splitList(r)
		changed = append(changed, "repos")
	}
	if len(changed) == 0 {
		return &ToolResult{Success: true, Message: "no fields to update"}, nil
	}

	if err := markdown.WriteMeta(path, meta); err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}
	h.publisher.Notify(specID)
	h.logAgentWrite(specID, "meta_update", fmt.Sprintf("agent updated %s", strings.Join(changed, ", ")))

	return &ToolResult{
		Success: true,
		Message: fmt.Sprintf("Updated %s: %s", specID, strings.Join(changed, ", ")),
	}, nil
}

// splitList parses a comma-separated field into trimmed values.
func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
