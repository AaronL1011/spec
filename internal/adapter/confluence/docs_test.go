package confluence

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

func TestMarkdownToStorage_Headings(t *testing.T) {
	md := "## 1. Problem Statement <!-- owner: pm -->\n\nSomething is broken.\n"
	storage := markdownToStorage(md, "SPEC-042", nil)

	if !strings.Contains(storage, `<!-- spec-section: problem_statement -->`) {
		t.Error("expected spec-section marker for problem_statement")
	}
	if !strings.Contains(storage, "<h2>") {
		t.Error("expected <h2> tag")
	}
	if !strings.Contains(storage, "Problem Statement") {
		t.Error("expected heading text")
	}
	if !strings.Contains(storage, "<p>Something is broken.</p>") {
		t.Error("expected paragraph")
	}
}

func TestMarkdownToStorage_CodeBlock(t *testing.T) {
	md := "```go\nfunc main() {}\n```\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	if !strings.Contains(storage, `ac:name="code"`) {
		t.Error("expected code macro")
	}
	if !strings.Contains(storage, `ac:name="language">go`) {
		t.Error("expected language parameter")
	}
	if !strings.Contains(storage, "func main()") {
		t.Error("expected code content")
	}
}

func TestMarkdownToStorage_Lists(t *testing.T) {
	md := "- Item one\n- Item two\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	if !strings.Contains(storage, "<ul>") {
		t.Error("expected <ul> tag")
	}
	if !strings.Contains(storage, "<li>Item one</li>") {
		t.Error("expected first list item")
	}
	if !strings.Contains(storage, "</ul>") {
		t.Error("expected closing </ul>")
	}
}

func TestMarkdownToStorage_OrderedList(t *testing.T) {
	md := "1. First\n2. Second\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	if !strings.Contains(storage, "<ol>") {
		t.Error("expected <ol> tag")
	}
	if !strings.Contains(storage, "<li>First</li>") {
		t.Error("expected first ordered item")
	}
}

func TestMarkdownToStorage_Table(t *testing.T) {
	md := "| Name | Value |\n|---|---|\n| foo | bar |\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	if !strings.Contains(storage, "<table>") {
		t.Error("expected <table>")
	}
	if !strings.Contains(storage, "<th>Name</th>") {
		t.Error("expected header cell")
	}
	if !strings.Contains(storage, "<td>foo</td>") {
		t.Error("expected data cell")
	}
}

func TestMarkdownToStorage_InlineFormatting(t *testing.T) {
	md := "This is **bold** and *italic* and `code`.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	if !strings.Contains(storage, "<strong>bold</strong>") {
		t.Error("expected <strong>")
	}
	if !strings.Contains(storage, "<em>italic</em>") {
		t.Error("expected <em>")
	}
	if !strings.Contains(storage, "<code>code</code>") {
		t.Error("expected <code>")
	}
}

func TestParseStorageToSections_WithMarkers(t *testing.T) {
	storage := `<!-- spec-section: problem_statement -->
<h2>1. Problem Statement</h2>
<p>Users are affected.</p>
<!-- spec-section: goals -->
<h2>2. Goals</h2>
<p>Fix the problem.</p>`

	sections := parseStorageToSections(storage)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if !strings.Contains(sections["problem_statement"], "Users are affected") {
		t.Errorf("problem_statement content: %q", sections["problem_statement"])
	}
	if !strings.Contains(sections["goals"], "Fix the problem") {
		t.Errorf("goals content: %q", sections["goals"])
	}
}

func TestParseStorageToSections_WithNumericMarker(t *testing.T) {
	storage := `<!-- spec-section: api_v2_plan -->
<h2>API v2 Plan</h2>
<p>Ship it.</p>`

	sections := parseStorageToSections(storage)
	if !strings.Contains(sections["api_v2_plan"], "Ship it") {
		t.Fatalf("api_v2_plan content = %q, want content from numeric slug", sections["api_v2_plan"])
	}
}

func TestStorageToMarkdown_Paragraphs(t *testing.T) {
	storage := "<p>Hello <strong>world</strong>.</p><p>Second paragraph.</p>"
	md := storageToMarkdown(storage)

	if !strings.Contains(md, "**world**") {
		t.Errorf("expected bold markdown, got %q", md)
	}
	if !strings.Contains(md, "Hello") {
		t.Errorf("expected text content, got %q", md)
	}
}

func TestStorageToMarkdown_Lists(t *testing.T) {
	storage := "<ul><li>First</li><li>Second</li></ul>"
	md := storageToMarkdown(storage)

	if !strings.Contains(md, "- First") {
		t.Errorf("expected list item, got %q", md)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1. Problem Statement", "problem_statement"},
		{"7.3 PR Stack Plan", "pr_stack_plan"},
		{"Decision Log", "decision_log"},
		{"Design Inputs", "design_inputs"},
		{"11. Retrospective", "retrospective"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRoundtrip_SimpleContent(t *testing.T) {
	// Convert markdown → storage → sections → markdown and verify content survives
	md := "## 1. Problem Statement\n\nUsers cannot log in.\n\n- EU users affected\n- Token expiry bug\n"
	storage := markdownToStorage(md, "SPEC-042", nil)
	sections := parseStorageToSections(storage)

	ps, ok := sections["problem_statement"]
	if !ok {
		t.Fatal("problem_statement section not found after roundtrip")
	}
	if !strings.Contains(ps, "Users cannot log in") {
		t.Errorf("content lost in roundtrip: %q", ps)
	}
	if !strings.Contains(ps, "EU users affected") {
		t.Errorf("list item lost in roundtrip: %q", ps)
	}
}

// testOptions builds Client Options against a stub server with all required
// fields populated.
func testOptions(serverURL string) Options {
	return Options{
		BaseURL:  serverURL,
		SpaceKey: "ENG",
		ParentID: "99999",
		Email:    "user@example.com",
		Token:    "token",
	}
}

func TestFetchSections_PageNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(searchResponse{})
	}))
	defer server.Close()

	client := NewClient(testOptions(server.URL))
	sections, err := client.FetchSections(context.Background(), "SPEC-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sections != nil {
		t.Errorf("expected nil sections for missing page, got %v", sections)
	}
}

func TestFetchSections_DuplicateLabelledPages_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searchResponse{}
		resp.Results = append(resp.Results, struct {
			ID string `json:"id"`
		}{ID: "1"}, struct {
			ID string `json:"id"`
		}{ID: "2"})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(testOptions(server.URL))
	_, err := client.FetchSections(context.Background(), "SPEC-042")
	if err == nil {
		t.Fatal("expected duplicate page error")
	}
}

// TestPushFull_CreatePage drives the full create path: label-based find (miss),
// space-id resolution, page creation, and label attach. It asserts the create
// body carries the numeric space id and parent id, and that a human-friendly
// title is derived from frontmatter.
func TestPushFull_CreatePage(t *testing.T) {
	var created createPageRequest
	var labelled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rest/api/content/search"):
			_ = json.NewEncoder(w).Encode(searchResponse{}) // no existing page
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v2/spaces"):
			_ = json.NewEncoder(w).Encode(spacesResponse{Results: []struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			}{{ID: "4242", Key: "ENG"}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v2/pages"):
			_ = json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(pageResponse{ID: "12345", Title: created.Title})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/label"):
			labelled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(testOptions(server.URL))
	content := "---\nid: SPEC-042\ntitle: Login Fix\nstatus: draft\n---\n\n## Problem\n\nSomething broke.\n"
	if err := client.PushFull(context.Background(), "SPEC-042", content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.SpaceID != "4242" {
		t.Errorf("expected numeric spaceId 4242, got %q", created.SpaceID)
	}
	if created.ParentID != "99999" {
		t.Errorf("expected parentId 99999, got %q", created.ParentID)
	}
	if created.Title != "SPEC-042 — Login Fix" {
		t.Errorf("expected friendly title, got %q", created.Title)
	}
	if created.Body.Representation != "storage" {
		t.Errorf("expected storage representation, got %s", created.Body.Representation)
	}
	if !labelled {
		t.Error("expected page to be labelled for durable identity")
	}
}

// TestPushFull_UpdateExistingPage exercises the update path: label search finds
// the page, the current version is read, and a PUT bumps the version with the
// friendly title preserved.
func TestPushFull_UpdateExistingPage(t *testing.T) {
	var updated updatePageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rest/api/content/search"):
			resp := searchResponse{}
			resp.Results = append(resp.Results, struct {
				ID string `json:"id"`
			}{ID: "777"})
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v2/pages/777"):
			_ = json.NewEncoder(w).Encode(pageResponse{ID: "777", Version: struct {
				Number int `json:"number"`
			}{Number: 4}})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/api/v2/pages/777"):
			_ = json.NewDecoder(r.Body).Decode(&updated)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(pageResponse{ID: "777"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(testOptions(server.URL))
	content := "---\nid: SPEC-042\ntitle: Login Fix\n---\n\n## Problem\n\nUpdated.\n"
	if err := client.PushFull(context.Background(), "SPEC-042", content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Version.Number != 5 {
		t.Errorf("expected version bump to 5, got %d", updated.Version.Number)
	}
	if updated.Title != "SPEC-042 — Login Fix" {
		t.Errorf("expected friendly title preserved, got %q", updated.Title)
	}
}

// TestMarkdownToStorage_EscapesXML proves prose with XML-special characters
// produces well-formed storage (the previous converter emitted invalid XML
// that Confluence rejected outright).
func TestMarkdownToStorage_EscapesXML(t *testing.T) {
	md := "A generic List<T> and a Q&A about a > b.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if strings.Contains(storage, "List<T>") || strings.Contains(storage, "Q&A") {
		t.Errorf("expected XML escaping, got raw specials: %q", storage)
	}
	if !strings.Contains(storage, "List&lt;T&gt;") || !strings.Contains(storage, "Q&amp;A") {
		t.Errorf("expected escaped entities, got %q", storage)
	}
	assertWellFormedXML(t, storage)
}

// TestMarkdownToStorage_Links converts markdown links to anchors.
func TestMarkdownToStorage_Links(t *testing.T) {
	md := "See [the design](https://example.com/d?a=1&b=2).\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if !strings.Contains(storage, `<a href="https://example.com/d?a=1&amp;b=2">the design</a>`) {
		t.Errorf("expected anchor with escaped href, got %q", storage)
	}
	assertWellFormedXML(t, storage)
}

// TestMarkdownToStorage_MetadataPanel renders frontmatter as an info panel and
// keeps the raw YAML out of the body.
func TestMarkdownToStorage_MetadataPanel(t *testing.T) {
	md := "---\nid: SPEC-007\ntitle: Thing\nstatus: engineering\n---\n\n## Problem\n\nBody.\n"
	meta, err := markdown.ParseMeta(md)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	storage := markdownToStorage(md, "SPEC-007", meta)
	if !strings.Contains(storage, `ac:name="info"`) {
		t.Errorf("expected info panel, got %q", storage)
	}
	if !strings.Contains(storage, "engineering") {
		t.Errorf("expected status in panel, got %q", storage)
	}
	if strings.Contains(storage, "status: engineering") {
		t.Errorf("raw frontmatter leaked into body: %q", storage)
	}
}

// TestMarkdownToStorage_CodeBlockRaw keeps code-block content raw inside CDATA
// rather than emitting literal entities, and verifies the CDATA section is
// terminated with "]]>" and the whole fragment is well-formed XML. A missing
// terminator left the CDATA open and swallowed the rest of the page into the
// first code block — the headline bug this guards against.
func TestMarkdownToStorage_CodeBlockRaw(t *testing.T) {
	md := "```go\nif a < b && c > d {}\n```\n\n## After\n\nBody.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if !strings.Contains(storage, "if a < b && c > d {}") {
		t.Errorf("expected raw code in CDATA, got %q", storage)
	}
	if strings.Count(storage, "<![CDATA[") != strings.Count(storage, "]]>") {
		t.Errorf("unbalanced CDATA open/close: %q", storage)
	}
	if !strings.Contains(storage, "<h2>After</h2>") {
		t.Errorf("content after the code block was swallowed: %q", storage)
	}
	assertWellFormedXML(t, storage)
}

// TestMarkdownToStorage_StarInCodeSpans is the regression for interleaved tags:
// a literal '*' inside two separate code spans let the italic regex wrap <em>
// across the </code>...<code> boundary, producing malformed XML.
func TestMarkdownToStorage_StarInCodeSpans(t *testing.T) {
	md := "Rename `SCHEDULED_JOB_*` event types to `BACKGROUND_JOB_*` for alignment.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if strings.Contains(storage, "<em>") {
		t.Errorf("a '*' inside code spans must not become emphasis: %q", storage)
	}
	if !strings.Contains(storage, "<code>SCHEDULED_JOB_*</code>") || !strings.Contains(storage, "<code>BACKGROUND_JOB_*</code>") {
		t.Errorf("code spans should preserve their literal '*': %q", storage)
	}
	assertWellFormedXML(t, storage)
}

// TestMarkdownToStorage_EmphasisAroundCode keeps emphasis well-formed when it
// legitimately wraps a code span.
func TestMarkdownToStorage_EmphasisAroundCode(t *testing.T) {
	md := "This is *very `important` indeed* today.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if !strings.Contains(storage, "<code>important</code>") {
		t.Errorf("expected code span preserved: %q", storage)
	}
	assertWellFormedXML(t, storage)
}

// TestMarkdownToStorage_IndentedClosingFence is the regression for the bug where
// an indented closing fence was not recognised, so the code block never closed
// and every following section was swallowed into it.
func TestMarkdownToStorage_IndentedClosingFence(t *testing.T) {
	md := "## H\n\n```go\nfunc main() {}\n  ```\n\n## After\n\nText after.\n"
	storage := markdownToStorage(md, "SPEC-001", nil)

	// The code macro must close exactly once and the following heading/body
	// must render as normal page content, not as code.
	if strings.Count(storage, "</ac:plain-text-body></ac:structured-macro>") != 1 {
		t.Fatalf("expected the code block to close once, got: %q", storage)
	}
	if !strings.Contains(storage, "<h2>After</h2>") {
		t.Errorf("content after the code block was swallowed: %q", storage)
	}
	if !strings.Contains(storage, "<p>Text after.</p>") {
		t.Errorf("paragraph after the code block was swallowed: %q", storage)
	}
}

// TestMarkdownToStorage_NestedFence checks a 4-backtick fence keeps inner
// triple-backtick lines as literal content and closes only on the longer fence.
func TestMarkdownToStorage_NestedFence(t *testing.T) {
	md := "````md\n```go\nx := 1\n```\n````\n\n## After\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if strings.Count(storage, `ac:name="code"`) != 1 {
		t.Fatalf("expected a single code macro, got: %q", storage)
	}
	if !strings.Contains(storage, "```go\nx := 1\n```") {
		t.Errorf("inner fences should be literal content: %q", storage)
	}
	if !strings.Contains(storage, "<h2>After</h2>") {
		t.Errorf("content after the outer fence was swallowed: %q", storage)
	}
}

// TestMarkdownToStorage_TildeFence supports ~~~ fences.
func TestMarkdownToStorage_TildeFence(t *testing.T) {
	md := "~~~go\nx := 1\n~~~\n\n## After\n"
	storage := markdownToStorage(md, "SPEC-001", nil)
	if !strings.Contains(storage, `ac:name="code"`) {
		t.Errorf("expected ~~~ to open a code macro: %q", storage)
	}
	if !strings.Contains(storage, "<h2>After</h2>") {
		t.Errorf("content after a tilde fence was swallowed: %q", storage)
	}
}

func TestParseFence(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOK  bool
		wantCh  byte
		wantLen int
		wantInf string
	}{
		{"open with lang", "```go", true, '`', 3, "go"},
		{"bare close", "```", true, '`', 3, ""},
		{"indented close", "   ```", true, '`', 3, ""},
		{"long fence", "````md", true, '`', 4, "md"},
		{"tilde", "~~~", true, '~', 3, ""},
		{"inline code, not a fence", "a `x` b", false, 0, 0, ""},
		{"too short", "``", false, 0, 0, ""},
		{"plain text", "hello", false, 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, n, info, ok := parseFence(tt.line)
			if ok != tt.wantOK || ch != tt.wantCh || n != tt.wantLen || info != tt.wantInf {
				t.Errorf("parseFence(%q) = (%q,%d,%q,%v), want (%q,%d,%q,%v)",
					tt.line, ch, n, info, ok, tt.wantCh, tt.wantLen, tt.wantInf, tt.wantOK)
			}
		})
	}
}

func TestSpecLabel(t *testing.T) {
	if got := specLabel("SPEC-042"); got != "spec-id-spec-042" {
		t.Errorf("specLabel = %q, want spec-id-spec-042", got)
	}
}

func TestPageTitle(t *testing.T) {
	if got := pageTitle("SPEC-1", nil); got != "SPEC-1" {
		t.Errorf("nil meta title = %q, want SPEC-1", got)
	}
	if got := pageTitle("SPEC-1", &markdown.SpecMeta{Title: "[Feature Title]"}); got != "SPEC-1" {
		t.Errorf("placeholder title = %q, want SPEC-1", got)
	}
	if got := pageTitle("SPEC-1", &markdown.SpecMeta{Title: "Real"}); got != "SPEC-1 — Real" {
		t.Errorf("friendly title = %q", got)
	}
}

// assertWellFormedXML wraps the fragment in a root element and parses it to
// prove the converter never emits malformed storage XML.
func assertWellFormedXML(t *testing.T, fragment string) {
	t.Helper()
	doc := "<root xmlns:ac=\"x\" xmlns:ri=\"y\">" + fragment + "</root>"
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("malformed storage XML: %v\nfragment: %s", err, fragment)
		}
	}
}

func TestIsSeparatorRow(t *testing.T) {
	tests := []struct {
		cells []string
		want  bool
	}{
		{[]string{"---", "---"}, true},
		{[]string{":---:", "---:"}, true},
		{[]string{"Name", "Value"}, false},
	}
	for _, tt := range tests {
		got := isSeparatorRow(tt.cells)
		if got != tt.want {
			t.Errorf("isSeparatorRow(%v) = %v, want %v", tt.cells, got, tt.want)
		}
	}
}

// TestMarkdownToStorage_NoMarkerForTitleHeading guards the wipe fix: the H1
// title must render as a heading but never carry a spec-section marker, since
// its slug maps to the local "section" that spans the entire document.
func TestMarkdownToStorage_NoMarkerForTitleHeading(t *testing.T) {
	md := `# SPEC-042 - Rate Limiting

## Problem Statement

Some content.
`
	storage := markdownToStorage(md, "SPEC-042", nil)

	if strings.Contains(storage, "spec-section: spec_042_rate_limiting") {
		t.Error("H1 title heading must not emit a spec-section marker")
	}
	if !strings.Contains(storage, "<h1>SPEC-042 - Rate Limiting</h1>") {
		t.Error("H1 title heading should still render as <h1>")
	}
	if !strings.Contains(storage, "<!-- spec-section: problem_statement -->") {
		t.Error("level-2 sections must keep their markers")
	}
}

// TestParseStorageByHeadings_SkipsPageTitle guards the marker-less fallback:
// the page title (h1) must not be keyed as a section, or inbound would map an
// empty/partial fragment onto the local whole-document title section.
func TestParseStorageByHeadings_SkipsPageTitle(t *testing.T) {
	storage := `<h1>SPEC-042 - Rate Limiting</h1>
<h2>Problem Statement</h2>
<p>Some content.</p>`

	sections := parseStorageToSections(storage)

	if _, ok := sections["spec_042_rate_limiting"]; ok {
		t.Error("page title (h1) must not be keyed as a section")
	}
	if got := sections["problem_statement"]; got != "Some content." {
		t.Errorf("problem_statement = %q, want %q", got, "Some content.")
	}
}

// TestRoundTrip_TitleSlugNeverKeyed pushes a full spec through the outbound
// converter and back through the inbound parser: the title slug must be absent
// so the sync engine can never be handed an empty fragment for it.
func TestRoundTrip_TitleSlugNeverKeyed(t *testing.T) {
	md := `---
id: SPEC-042
title: Rate Limiting
status: plan_review
---

# SPEC-042 - Rate Limiting

## Problem Statement

Clients can exhaust capacity.
`
	storage := markdownToStorage(md, "SPEC-042", nil)
	sections := parseStorageToSections(storage)

	if _, ok := sections["spec_042_rate_limiting"]; ok {
		t.Error("round trip keyed the title slug — inbound wipe hazard")
	}
	if got := sections["problem_statement"]; got != "Clients can exhaust capacity." {
		t.Errorf("problem_statement = %q", got)
	}
}
