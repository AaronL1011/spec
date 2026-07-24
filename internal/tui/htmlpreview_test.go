package tui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestRenderSpecHTML(t *testing.T) {
	content := `---
id: SPEC-042
status: draft
---

# Title

| Col A | Col B |
|-------|-------|
| 1     | 2     |

- [x] done item
`
	doc, err := renderSpecHTML(content, "SPEC-042")
	if err != nil {
		t.Fatalf("renderSpecHTML: %v", err)
	}

	if !strings.HasPrefix(doc, "<!doctype html>") {
		t.Errorf("missing doctype prefix")
	}
	if !strings.Contains(doc, "<title>SPEC-042</title>") {
		t.Errorf("missing title, got: %.200s", doc)
	}
	if strings.Contains(doc, "status: draft") {
		t.Errorf("front matter leaked into output")
	}
	if !strings.Contains(doc, "<h1") || !strings.Contains(doc, "Title") {
		t.Errorf("heading not rendered")
	}
	if !strings.Contains(doc, "<table>") {
		t.Errorf("GFM table not rendered")
	}
	if !strings.Contains(doc, `type="checkbox"`) {
		t.Errorf("task list not rendered")
	}
}

func TestRenderSpecHTMLHighlightsCode(t *testing.T) {
	content := "```go\nfunc main() {}\n```\n"
	doc, err := renderSpecHTML(content, "SPEC-001")
	if err != nil {
		t.Fatalf("renderSpecHTML: %v", err)
	}
	if !strings.Contains(doc, `class="chroma"`) {
		t.Errorf("fenced code block not run through chroma")
	}
	if !strings.Contains(doc, `<span class=`) {
		t.Errorf("no token spans in highlighted output")
	}
	if !strings.Contains(doc, `:root[data-theme="dark"] .chroma`) {
		t.Errorf("dark-scoped highlight CSS missing")
	}
}

func TestRenderSpecHTMLRendersMermaid(t *testing.T) {
	content := "```mermaid\ngraph TD;\n  A-->B;\n```\n"
	doc, err := renderSpecHTML(content, "SPEC-001")
	if err != nil {
		t.Fatalf("renderSpecHTML: %v", err)
	}
	if !strings.Contains(doc, `<pre class="mermaid">`) {
		t.Errorf("mermaid fence not rendered as mermaid block")
	}
	if !strings.Contains(doc, "A--&gt;B") {
		t.Errorf("diagram source missing from mermaid block")
	}
	if !strings.Contains(doc, "cdn.jsdelivr.net/npm/mermaid@11") {
		t.Errorf("mermaid loader script missing")
	}
	if strings.Contains(doc, `<pre class="chroma"><code>graph`) {
		t.Errorf("mermaid fence was highlighted as code instead of diagram")
	}
}

func TestRenderSpecHTMLOmitsMermaidScriptWithoutDiagrams(t *testing.T) {
	doc, err := renderSpecHTML("# Plain", "SPEC-002")
	if err != nil {
		t.Fatalf("renderSpecHTML: %v", err)
	}
	if strings.Contains(doc, "cdn.jsdelivr.net/npm/mermaid") {
		t.Errorf("mermaid script injected for spec without diagrams")
	}
}

func TestRenderSpecHTMLEscapesTitle(t *testing.T) {
	doc, err := renderSpecHTML("body", `<script>alert(1)</script>`)
	if err != nil {
		t.Fatalf("renderSpecHTML: %v", err)
	}
	if strings.Contains(doc, "<script>alert(1)</script>") {
		t.Errorf("title not escaped")
	}
}

func TestPreviewServerServesRegisteredSpec(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SPEC-001.md"), []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := &config.ResolvedConfig{SpecsRepoDir: dir}
	srv := &previewServer{specs: map[string]*config.ResolvedConfig{"SPEC-001": rc}}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	res := previewGet(t, ts, "/SPEC-001")
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Hello") {
		t.Errorf("rendered spec content missing from response")
	}

	res2 := previewGet(t, ts, "/SPEC-999")
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != 404 {
		t.Errorf("unregistered spec: status = %d, want 404", res2.StatusCode)
	}
}

func previewGet(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
