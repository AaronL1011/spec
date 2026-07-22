package markdown

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

//go:embed default_spec.md
var defaultSpecTpl string

//go:embed default_triage.md
var defaultTriageTpl string

// Default spec/triage template paths inside the specs repo.
const (
	DefaultSpecPath   = "templates/spec.md"
	DefaultTriagePath = "templates/triage.md"
)

// TemplateKind selects which template a resolve/render/validate call targets.
type TemplateKind int

const (
	// SpecTemplate is the SPEC-NNN.md skeleton.
	SpecTemplate TemplateKind = iota
	// TriageTemplate is the TRIAGE-NNN.md skeleton.
	TriageTemplate
)

func (k TemplateKind) name() string {
	if k == TriageTemplate {
		return "triage"
	}
	return "spec"
}

func (k TemplateKind) defaultContent() string {
	if k == TriageTemplate {
		return defaultTriageTpl
	}
	return defaultSpecTpl
}

func (k TemplateKind) defaultPath() string {
	if k == TriageTemplate {
		return DefaultTriagePath
	}
	return DefaultSpecPath
}

// SpecFields are the scaffold fields available to a spec template.
type SpecFields struct {
	ID, Title, Author, Cycle, Source, Date string

	// Assignees is the initial claim written to the scaffolded frontmatter
	// (typically the creator, so new specs start claimed rather than
	// unclaimed). It is not a template placeholder: it is injected into the
	// rendered frontmatter, so custom team templates receive it without
	// referencing it. A template that already emits an assignees key wins.
	Assignees []string
}

// TriageFields are the scaffold fields available to a triage template.
type TriageFields struct {
	ID, Title, Priority, Source, SourceRef, ReportedBy, Date string
}

// KV is an ordered key/value pair, used for frontmatter defaults.
type KV struct {
	Key   string
	Value string
}

// TemplateConfig is the markdown-local view of a team's templates config. It
// is built from config.ResolvedConfig so this package stays decoupled from
// internal/config.
type TemplateConfig struct {
	SpecPath            string
	TriagePath          string
	FrontmatterDefaults []KV
}

// DefaultSpecTemplate returns the embedded default spec template.
func DefaultSpecTemplate() string { return defaultSpecTpl }

// DefaultTriageTemplate returns the embedded default triage template.
func DefaultTriageTemplate() string { return defaultTriageTpl }

// fieldFuncs builds a FuncMap that resolves <% name %> placeholders to the
// given field values. Every known field name is registered so a custom
// template that references an unknown name fails at parse time (caught by
// ValidateTemplate and by the scaffold fallback).
func fieldFuncs(fields map[string]string) template.FuncMap {
	fm := make(template.FuncMap, len(fields))
	for k, v := range fields {
		fm[k] = func() string { return v }
	}
	return fm
}

// parseTemplateWith parses content with the <% %> delimiters and the given
// field functions. text/template validates function references at parse time,
// so an unknown placeholder (<% foo %>) surfaces here as a parse error.
func parseTemplateWith(name, content string, fields map[string]string) (*template.Template, error) {
	return template.New(name).Delims("<%", "%>").Funcs(fieldFuncs(fields)).Parse(content)
}

// renderTemplate parses and executes content with the given field values.
func renderTemplate(name, content string, fields map[string]string) (string, error) {
	t, err := parseTemplateWith(name, content, fields)
	if err != nil {
		return "", fmt.Errorf("parsing %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("rendering %s template: %w", name, err)
	}
	return buf.String(), nil
}

// sampleAllFields returns non-empty values for every known placeholder, used
// during parsing and validation so unresolved tokens surface as errors.
func sampleAllFields() map[string]string {
	return map[string]string{
		"id":          "SPEC-000",
		"title":       "Sample Title",
		"author":      "sample-author",
		"cycle":       "Cycle 0",
		"source":      "direct",
		"date":        "2026-01-01",
		"priority":    "medium",
		"source_ref":  "sample-ref",
		"reported_by": "sample-reporter",
	}
}

// specFieldsMap returns the field map for a spec render, seeded with sample
// values for triage-only fields so a spec template that references them still
// parses (it would render the sample, which validate flags as a misuse).
func specFieldsMap(f SpecFields) map[string]string {
	return map[string]string{
		"id":          f.ID,
		"title":       f.Title,
		"author":      f.Author,
		"cycle":       f.Cycle,
		"source":      f.Source,
		"date":        f.Date,
		"priority":    "medium",
		"source_ref":  "",
		"reported_by": "",
	}
}

// triageFieldsMap returns the field map for a triage render, seeded with
// sample values for spec-only fields so a triage template parses cleanly.
func triageFieldsMap(f TriageFields) map[string]string {
	return map[string]string{
		"id":          f.ID,
		"title":       f.Title,
		"priority":    f.Priority,
		"source":      f.Source,
		"source_ref":  f.SourceRef,
		"reported_by": f.ReportedBy,
		"date":        f.Date,
		"author":      "",
		"cycle":       "",
	}
}

// RenderSpec renders a spec template with the given fields and optional
// frontmatter defaults. Defaults whose key already appears in the rendered
// frontmatter are skipped, so computed fields always win.
func RenderSpec(tpl string, fields SpecFields, defaults []KV) (string, error) {
	out, err := renderTemplate("spec", tpl, specFieldsMap(fields))
	if err != nil {
		return "", err
	}
	// Assignees inject before frontmatter defaults so a computed claim wins
	// over a team-configured `assignees` default, matching the "runtime
	// fields always win" rule documented on injectFrontmatterDefaults.
	if len(fields.Assignees) > 0 {
		out = injectFrontmatterLines(out, []frontmatterLine{{key: "assignees", value: yamlFlowSeq(fields.Assignees)}})
	}
	if len(defaults) > 0 {
		out = injectFrontmatterDefaults(out, defaults)
	}
	return out, nil
}

// RenderTriage renders a triage template with the given fields.
func RenderTriage(tpl string, fields TriageFields) (string, error) {
	return renderTemplate("triage", tpl, triageFieldsMap(fields))
}

// ReadTeamTemplate reads the raw team template file for a kind from
// repoDir/configuredPath (defaulting to the conventional path). It returns
// the file content, the resolved path, and whether the file exists. No
// validation is performed — callers that need the effective (safe) template
// should use ResolveTemplate instead.
func ReadTeamTemplate(kind TemplateKind, repoDir, configuredPath string) (content, path string, ok bool) {
	if repoDir == "" {
		return "", "", false
	}
	if configuredPath == "" {
		configuredPath = kind.defaultPath()
	}
	path = filepath.Join(repoDir, configuredPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", path, false
	}
	return string(data), path, true
}

// ResolveTemplate returns the effective template content for a kind: the team
// file at repoDir/configuredPath if present and free of fatal validation
// issues (parse/render errors, unresolved placeholders, missing gate-critical
// sections), else the embedded default. The returned source is "team" or
// "default".
//
// Falling back — rather than erroring — keeps scaffolding forgiving of a
// fluid team template: a template broken mid-edit degrades to the built-in
// default with a warning instead of blocking spec creation.
func ResolveTemplate(kind TemplateKind, repoDir, configuredPath string) (content, source string) {
	raw, path, ok := ReadTeamTemplate(kind, repoDir, configuredPath)
	if !ok {
		return kind.defaultContent(), "default"
	}
	if msg := firstFatalIssue(kind, raw); msg != "" {
		fmt.Fprintf(os.Stderr, "warning: %s template at %s is invalid (%s); falling back to built-in default — run 'spec template validate %s' for details\n",
			kind.name(), path, msg, kind.name())
		return kind.defaultContent(), "default"
	}
	return raw, "team"
}

// firstFatalIssue returns the message of the first fatal validation issue in
// content, or "" when the template is safe to scaffold from.
func firstFatalIssue(kind TemplateKind, content string) string {
	for _, iss := range ValidateTemplate(kind, content) {
		if iss.Fatal {
			return iss.Message
		}
	}
	return ""
}

// ScaffoldSpec generates a new SPEC.md from the embedded default template. It
// is the zero-config entry point; use ScaffoldSpecFromConfig to honour a
// committed team template and frontmatter defaults.
func ScaffoldSpec(id, title, author, cycle, source string) string {
	date := time.Now().Format("2006-01-02")
	out, err := RenderSpec(defaultSpecTpl, SpecFields{ID: id, Title: title, Author: author, Cycle: cycle, Source: source, Date: date}, nil)
	if err != nil {
		// The embedded default cannot fail to render; this is unreachable.
		return defaultSpecTpl
	}
	return out
}

// ScaffoldSpecFromConfig resolves the effective spec template (team file first,
// embedded default fallback) and renders it with the given fields and
// frontmatter defaults. A render error in a team template falls back to the
// embedded default so `spec new` never blocks on a broken template.
func ScaffoldSpecFromConfig(repoDir string, tc TemplateConfig, fields SpecFields) string {
	content, source := ResolveTemplate(SpecTemplate, repoDir, tc.SpecPath)
	out, err := RenderSpec(content, fields, tc.FrontmatterDefaults)
	if err != nil {
		if source == "team" {
			fmt.Fprintf(os.Stderr, "warning: team spec template failed to render (%v); falling back to built-in default\n", err)
		}
		out, err = RenderSpec(defaultSpecTpl, fields, tc.FrontmatterDefaults)
		if err != nil {
			// Unreachable: the embedded default always renders (regression-tested).
			return defaultSpecTpl
		}
	}
	return out
}

// ScaffoldTriage generates a new TRIAGE.md from the embedded default template.
func ScaffoldTriage(id, title, priority, source, sourceRef, reportedBy string) string {
	if priority == "" {
		priority = "medium"
	}
	date := time.Now().Format("2006-01-02")
	out, err := RenderTriage(defaultTriageTpl, TriageFields{ID: id, Title: title, Priority: priority, Source: source, SourceRef: sourceRef, ReportedBy: reportedBy, Date: date})
	if err != nil {
		return defaultTriageTpl
	}
	return out
}

// ScaffoldTriageFromConfig resolves the effective triage template and renders
// it with the given fields.
func ScaffoldTriageFromConfig(repoDir string, tc TemplateConfig, fields TriageFields) string {
	content, source := ResolveTemplate(TriageTemplate, repoDir, tc.TriagePath)
	out, err := RenderTriage(content, fields)
	if err != nil {
		if source == "team" {
			fmt.Fprintf(os.Stderr, "warning: team triage template failed to render (%v); falling back to built-in default\n", err)
		}
		out, err = RenderTriage(defaultTriageTpl, fields)
		if err != nil {
			// Unreachable: the embedded default always renders (regression-tested).
			return defaultTriageTpl
		}
	}
	return out
}

// injectFrontmatterDefaults inserts non-colliding frontmatter defaults into the
// rendered frontmatter block, in declaration order, before the closing ---.
// Keys already present (computed fields) are skipped, so runtime fields always
// win. The block is written directly (no YAML round-trip) to keep diffs stable.
func injectFrontmatterDefaults(rendered string, defaults []KV) string {
	entries := make([]frontmatterLine, 0, len(defaults))
	for _, d := range defaults {
		entries = append(entries, frontmatterLine{key: d.Key, value: yamlScalar(d.Value)})
	}
	return injectFrontmatterLines(rendered, entries)
}

// frontmatterLine is one frontmatter entry to inject: a key and an
// already-rendered YAML value (scalar or flow sequence).
type frontmatterLine struct {
	key   string
	value string
}

// injectFrontmatterLines inserts pre-rendered frontmatter entries before the
// closing ---, in declaration order, skipping keys the block already has.
func injectFrontmatterLines(rendered string, entries []frontmatterLine) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return rendered
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return rendered
	}
	existing := make(map[string]bool, closeIdx-1)
	for i := 1; i < closeIdx; i++ {
		kv := strings.SplitN(lines[i], ":", 2)
		existing[strings.TrimSpace(kv[0])] = true
	}
	var insert []string
	for _, e := range entries {
		if e.key == "" || existing[e.key] {
			continue
		}
		insert = append(insert, e.key+": "+e.value)
		existing[e.key] = true
	}
	if len(insert) == 0 {
		return rendered
	}
	merged := make([]string, 0, len(lines)+len(insert))
	merged = append(merged, lines[:closeIdx]...)
	merged = append(merged, insert...)
	merged = append(merged, lines[closeIdx:]...)
	return strings.Join(merged, "\n")
}

// yamlFlowSeq renders values as a YAML flow sequence (e.g. ["@ana", "@ben"]),
// quoting each element by the same rules as yamlScalar.
func yamlFlowSeq(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = yamlScalar(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// yamlScalar returns v rendered as a YAML scalar, double-quoting it when the
// plain form would be ambiguous or invalid (empty value, leading indicator
// character, ": " or " #" sequences, trailing colon, surrounding whitespace,
// control characters). Go's strconv.Quote escaping is a valid YAML
// double-quoted scalar.
func yamlScalar(v string) string {
	if v == "" {
		return `""`
	}
	if needsYAMLQuoting(v) {
		return strconv.Quote(v)
	}
	return v
}

// yamlIndicators are characters that start another YAML construct when they
// lead a plain scalar.
const yamlIndicators = "-?:,[]{}#&*!|>'\"%@`"

func needsYAMLQuoting(v string) bool {
	switch {
	case v != strings.TrimSpace(v):
		return true
	case strings.ContainsAny(v, "\n\t"):
		return true
	case strings.ContainsRune(yamlIndicators, rune(v[0])):
		return true
	case strings.Contains(v, ": ") || strings.HasSuffix(v, ":"):
		return true
	case strings.Contains(v, " #"):
		return true
	}
	return false
}

// TemplateIssue is a single validation finding. Fatal issues fail validation;
// non-fatal issues are reported as warnings.
type TemplateIssue struct {
	Fatal   bool
	Message string
}

// ValidateTemplate parses, renders, and structurally checks a template. It
// returns fatal issues (parse errors, unresolved placeholders, missing
// gate-critical sections) and non-fatal issues (level-2 sections lacking an
// owner/auto marker). The embedded default validates clean on fatal issues.
func ValidateTemplate(kind TemplateKind, content string) []TemplateIssue {
	var issues []TemplateIssue

	fields := sampleAllFields()
	if _, err := parseTemplateWith(kind.name(), content, fields); err != nil {
		return []TemplateIssue{{Fatal: true, Message: fmt.Sprintf("parse error: %v", err)}}
	}

	out, err := renderTemplate(kind.name(), content, fields)
	if err != nil {
		return []TemplateIssue{{Fatal: true, Message: fmt.Sprintf("render error: %v", err)}}
	}
	if idx := strings.Index(out, "<%"); idx >= 0 {
		snippet := out[idx:]
		if end := strings.Index(snippet, "%>"); end >= 0 {
			snippet = snippet[:end+2]
		}
		issues = append(issues, TemplateIssue{Fatal: true, Message: fmt.Sprintf("unresolved placeholder %q", snippet)})
	}

	body := Body(out)
	sections := ExtractSections(body)

	// Every level-2 heading should carry an owner/auto marker. Missing markers
	// are a warning (non-fatal) so the default template — whose Decision Log
	// section intentionally has none — still validates clean.
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		if !ownerPattern.MatchString(line) && !autoPattern.MatchString(line) {
			issues = append(issues, TemplateIssue{Fatal: false, Message: fmt.Sprintf("section lacks owner/auto marker: %q", strings.TrimSpace(line))})
		}
	}

	// Gate-critical sections must be present (spec templates only).
	if kind == SpecTemplate {
		for _, slug := range []string{"problem_statement", "user_stories", "acceptance_criteria"} {
			if FindSection(sections, slug) == nil {
				issues = append(issues, TemplateIssue{Fatal: true, Message: fmt.Sprintf("missing gate-critical section %q", slug)})
			}
		}
	}

	return issues
}

// MaxSpecNum returns the highest SPEC-NNN number among the given filenames, or
// 0 if none. It is the bootstrap seed for the counter ref (SPEC-018 §7.1).
func MaxSpecNum(existingFiles []string) int {
	return maxNumWithPrefix(existingFiles, "SPEC-%d.md")
}

// MaxTriageNum returns the highest TRIAGE-NNN number among the given filenames,
// or 0 if none.
func MaxTriageNum(existingFiles []string) int {
	return maxNumWithPrefix(existingFiles, "TRIAGE-%d.md")
}

func maxNumWithPrefix(existingFiles []string, format string) int {
	maxNum := 0
	for _, f := range existingFiles {
		var num int
		if _, err := fmt.Sscanf(f, format, &num); err == nil {
			if num > maxNum {
				maxNum = num
			}
		}
	}
	return maxNum
}
