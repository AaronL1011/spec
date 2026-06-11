package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity classifies a lint diagnostic.
type Severity string

const (
	// SeverityError is a config defect that should block (non-zero exit).
	SeverityError Severity = "error"
	// SeverityWarning is a non-blocking advisory.
	SeverityWarning Severity = "warning"
)

// Diagnostic is a single line-precise lint finding. Line and Column are
// 1-based; Column is 0 when only a line is known.
type Diagnostic struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Column     int      `json:"column,omitempty"`
	Severity   Severity `json:"severity"`
	Field      string   `json:"field,omitempty"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// LintResult is the full outcome of linting one config file.
type LintResult struct {
	File        string       `json:"file"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// HasErrors reports whether any diagnostic is an error (blocks exit 0).
func (r LintResult) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// knownGateKeys is the set of valid keys inside a gate mapping. Used to detect
// typos like "sectoin_complete" with a did-you-mean suggestion.
var knownGateKeys = []string{
	"section_not_empty", "section_complete", "pr_stack_exists", "steps_exists",
	"prs_approved", "review_approved", "duration", "link_exists",
	"expr", "message", "all", "any", "not",
}

// knownPresetNames enumerates the built-in pipeline presets. It mirrors the
// preset registry in internal/pipeline (which cannot be imported here without a
// cycle, since pipeline depends on config); pipeline.PresetNames is the
// authoritative list and a test guards these two against drift.
var knownPresetNames = []string{"minimal", "startup", "product", "platform", "kanban"}

// KnownPresets returns the built-in pipeline preset names recognised by the
// linter.
func KnownPresets() []string {
	return knownPresetNames
}

func isKnownPreset(name string) bool {
	return contains(knownPresetNames, name)
}

// validDoScopes enumerates the accepted dashboard do_scope values.
var validDoScopes = []string{"role", "assignee", "author", "none"}

// validSyncDirections enumerates the accepted sync-effect directions.
var validSyncDirections = []string{"inbound", "outbound"}

// LintTeamConfigFile reads and lints a team config file at path. A read or
// parse failure is itself returned as an error-severity diagnostic so the
// caller always gets a structured result.
func LintTeamConfigFile(path string) (LintResult, error) {
	res := LintResult{File: path}

	data, err := os.ReadFile(path)
	if err != nil {
		return res, fmt.Errorf("reading config %q: %w — run 'spec config init' to create one", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			File: path, Line: yamlErrorLine(err), Severity: SeverityError,
			Message: "config is not valid YAML: " + err.Error(),
		})
		return res, nil
	}

	res.Diagnostics = lintTeamConfigNode(path, &doc)
	sortDiagnostics(res.Diagnostics)
	return res, nil
}

// lintTeamConfigNode walks the parsed YAML document and collects diagnostics.
func lintTeamConfigNode(path string, doc *yaml.Node) []Diagnostic {
	root := documentRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return []Diagnostic{{
			File: path, Line: 1, Severity: SeverityError,
			Message: "config root must be a YAML mapping",
		}}
	}

	var diags []Diagnostic

	// Required top-level field: version.
	if vNode := mapValue(root, "version"); vNode == nil {
		diags = append(diags, Diagnostic{
			File: path, Line: lineOf(root), Severity: SeverityError, Field: "version",
			Message:    "missing required field \"version\"",
			Suggestion: "add 'version: \"1\"' at the top of the file",
		})
	}

	pipelineNode := mapValue(root, "pipeline")
	if pipelineNode != nil {
		diags = append(diags, lintPipelineNode(path, pipelineNode)...)
	}

	return diags
}

// lintPipelineNode validates the pipeline block: preset name, stage list, and
// each stage's gates, effects, and enums.
func lintPipelineNode(path string, pipelineNode *yaml.Node) []Diagnostic {
	var diags []Diagnostic

	if presetNode := mapValue(pipelineNode, "preset"); presetNode != nil {
		if name := presetNode.Value; name != "" && !isKnownPreset(name) {
			diags = append(diags, Diagnostic{
				File: path, Line: lineOf(presetNode), Column: presetNode.Column,
				Severity: SeverityError, Field: "pipeline.preset",
				Message:    fmt.Sprintf("unknown preset %q", name),
				Suggestion: suggest(name, KnownPresets()),
			})
		}
	}

	stagesNode := mapValue(pipelineNode, "stages")
	if stagesNode == nil || stagesNode.Kind != yaml.SequenceNode {
		return diags
	}

	for i, stageNode := range stagesNode.Content {
		diags = append(diags, lintStageNode(path, i, stageNode)...)
	}
	return diags
}

// lintStageNode validates a single stage mapping.
func lintStageNode(path string, idx int, stageNode *yaml.Node) []Diagnostic {
	if stageNode.Kind != yaml.MappingNode {
		return nil
	}
	var diags []Diagnostic
	field := fmt.Sprintf("stages[%d]", idx)

	// A stage must be named.
	if nameNode := mapValue(stageNode, "name"); nameNode == nil || nameNode.Value == "" {
		diags = append(diags, Diagnostic{
			File: path, Line: lineOf(stageNode), Severity: SeverityError,
			Field:   field + ".name",
			Message: "stage is missing a required \"name\"",
		})
	}

	// do_scope enum (nested under dashboard).
	if dashNode := mapValue(stageNode, "dashboard"); dashNode != nil {
		if scopeNode := mapValue(dashNode, "do_scope"); scopeNode != nil && scopeNode.Value != "" {
			if !contains(validDoScopes, scopeNode.Value) {
				diags = append(diags, Diagnostic{
					File: path, Line: lineOf(scopeNode), Column: scopeNode.Column,
					Severity: SeverityError, Field: field + ".dashboard.do_scope",
					Message:    fmt.Sprintf("unknown do_scope %q", scopeNode.Value),
					Suggestion: suggest(scopeNode.Value, validDoScopes),
				})
			}
		}
	}

	// Gates.
	if gatesNode := mapValue(stageNode, "gates"); gatesNode != nil && gatesNode.Kind == yaml.SequenceNode {
		for j, gateNode := range gatesNode.Content {
			diags = append(diags, lintGateNode(path, fmt.Sprintf("%s.gates[%d]", field, j), gateNode)...)
		}
	}

	// Effects on enter/exit.
	for _, key := range []string{"on_enter", "on_exit"} {
		if effNode := mapValue(stageNode, key); effNode != nil && effNode.Kind == yaml.SequenceNode {
			for j, e := range effNode.Content {
				diags = append(diags, lintEffectNode(path, fmt.Sprintf("%s.%s[%d]", field, key, j), e)...)
			}
		}
	}

	return diags
}

// lintGateNode validates a gate mapping, flagging unknown keys (typos) with a
// did-you-mean suggestion and recursing into logical sub-gates.
func lintGateNode(path, field string, gateNode *yaml.Node) []Diagnostic {
	if gateNode.Kind != yaml.MappingNode {
		return nil
	}
	var diags []Diagnostic
	matched := false

	for i := 0; i+1 < len(gateNode.Content); i += 2 {
		keyNode := gateNode.Content[i]
		valNode := gateNode.Content[i+1]
		key := keyNode.Value

		if !contains(knownGateKeys, key) {
			diags = append(diags, Diagnostic{
				File: path, Line: lineOf(keyNode), Column: keyNode.Column,
				Severity: SeverityError, Field: field + ".gate",
				Message:    fmt.Sprintf("unknown gate type %q", key),
				Suggestion: suggest(key, knownGateKeys),
			})
			continue
		}
		matched = true

		// Recurse into logical sub-gates.
		switch key {
		case "all", "any":
			if valNode.Kind == yaml.SequenceNode {
				for j, sub := range valNode.Content {
					diags = append(diags, lintGateNode(path, fmt.Sprintf("%s.%s[%d]", field, key, j), sub)...)
				}
			}
		case "not":
			diags = append(diags, lintGateNode(path, field+".not", valNode)...)
		}
	}

	if !matched && len(diags) == 0 {
		diags = append(diags, Diagnostic{
			File: path, Line: lineOf(gateNode), Severity: SeverityError, Field: field,
			Message: "gate has no recognised condition",
		})
	}
	return diags
}

// lintEffectNode validates an effect mapping: sync direction and webhook URL.
func lintEffectNode(path, field string, effNode *yaml.Node) []Diagnostic {
	if effNode.Kind != yaml.MappingNode {
		return nil
	}
	var diags []Diagnostic

	if syncNode := mapValue(effNode, "sync"); syncNode != nil && syncNode.Value != "" {
		if !contains(validSyncDirections, syncNode.Value) {
			diags = append(diags, Diagnostic{
				File: path, Line: lineOf(syncNode), Column: syncNode.Column,
				Severity: SeverityError, Field: field + ".sync",
				Message:    fmt.Sprintf("unknown sync direction %q", syncNode.Value),
				Suggestion: suggest(syncNode.Value, validSyncDirections),
			})
		}
	}

	if webhookNode := mapValue(effNode, "webhook"); webhookNode != nil && webhookNode.Kind == yaml.MappingNode {
		if urlNode := mapValue(webhookNode, "url"); urlNode == nil || urlNode.Value == "" {
			diags = append(diags, Diagnostic{
				File: path, Line: lineOf(webhookNode), Severity: SeverityError,
				Field:   field + ".webhook.url",
				Message: "webhook effect is missing a required \"url\"",
			})
		}
	}

	return diags
}

// --- YAML node helpers ---

// documentRoot returns the mapping node at the root of a parsed document.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

// mapValue returns the value node for key in a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// lineOf returns the 1-based line of a node, or 1 if unknown.
func lineOf(n *yaml.Node) int {
	if n == nil || n.Line == 0 {
		return 1
	}
	return n.Line
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// suggest returns a "did you mean X?" hint for the closest candidate within a
// small edit distance, or "" when nothing is close enough.
func suggest(got string, candidates []string) string {
	best := ""
	bestDist := len(got)/2 + 1 // tolerate roughly half the length as edits
	if bestDist < 1 {
		bestDist = 1
	}
	for _, c := range candidates {
		d := levenshtein(got, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("did you mean %q?", best)
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// sortDiagnostics orders diagnostics by line then column for stable output.
func sortDiagnostics(diags []Diagnostic) {
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Line != diags[j].Line {
			return diags[i].Line < diags[j].Line
		}
		return diags[i].Column < diags[j].Column
	})
}

// yamlErrorLine extracts a line number from a yaml parse error message, or 0.
func yamlErrorLine(err error) int {
	// yaml.v3 errors are formatted as "yaml: line N: ...".
	msg := err.Error()
	const marker = "line "
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return 0
	}
	rest := msg[idx+len(marker):]
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
