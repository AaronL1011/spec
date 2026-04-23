package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarkdownToStorage_Headings(t *testing.T) {
	md := "## 1. Problem Statement <!-- owner: pm -->\n\nSomething is broken.\n"
	storage := markdownToStorage(md, "SPEC-042")

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
	storage := markdownToStorage(md, "SPEC-001")

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
	storage := markdownToStorage(md, "SPEC-001")

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
	storage := markdownToStorage(md, "SPEC-001")

	if !strings.Contains(storage, "<ol>") {
		t.Error("expected <ol> tag")
	}
	if !strings.Contains(storage, "<li>First</li>") {
		t.Error("expected first ordered item")
	}
}

func TestMarkdownToStorage_Table(t *testing.T) {
	md := "| Name | Value |\n|---|---|\n| foo | bar |\n"
	storage := markdownToStorage(md, "SPEC-001")

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
	storage := markdownToStorage(md, "SPEC-001")

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
	storage := markdownToStorage(md, "SPEC-042")
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

func TestFetchSections_PageNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pagesResponse{Results: []pageResponse{}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "ENG", "user@example.com", "token")
	sections, err := client.FetchSections(context.Background(), "SPEC-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sections != nil {
		t.Errorf("expected nil sections for missing page, got %v", sections)
	}
}

func TestPushFull_CreatePage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.Method {
		case http.MethodGet:
			// findPage — return empty results
			_ = json.NewEncoder(w).Encode(pagesResponse{Results: []pageResponse{}})
		case http.MethodPost:
			var req createPageRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Title != "SPEC-042" {
				t.Errorf("expected title SPEC-042, got %s", req.Title)
			}
			if req.Body.Representation != "storage" {
				t.Errorf("expected storage representation, got %s", req.Body.Representation)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(pageResponse{ID: "12345", Title: "SPEC-042"})
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "ENG", "user@example.com", "token")
	err := client.PushFull(context.Background(), "SPEC-042", "## Problem\n\nSomething broke.\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 2 {
		t.Errorf("expected at least 2 API calls (find + create), got %d", calls)
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
