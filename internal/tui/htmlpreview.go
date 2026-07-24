package tui

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"go.abhg.dev/goldmark/mermaid"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

const htmlPreviewTemplate = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
:root {
  color-scheme: light;
  --fg: #1f2328;
  --bg: #ffffff;
  --muted: #59636e;
  --border: #d1d9e0;
  --code-bg: #f6f8fa;
  --accent: #0969da;
}
:root[data-theme="dark"] {
  color-scheme: dark;
  --fg: #f0f6fc;
  --bg: #0d1117;
  --muted: #9198a1;
  --border: #3d444d;
  --code-bg: #151b23;
  --accent: #4493f8;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    color-scheme: dark;
    --fg: #f0f6fc;
    --bg: #0d1117;
    --muted: #9198a1;
    --border: #3d444d;
    --code-bg: #151b23;
    --accent: #4493f8;
  }
}
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif;
  font-size: 16px;
  line-height: 1.6;
  color: var(--fg);
  background: var(--bg);
  max-width: 860px;
  margin: 0 auto;
  padding: 2rem 1.5rem 4rem;
}
h1, h2, h3, h4, h5, h6 { line-height: 1.25; margin-top: 1.5em; margin-bottom: 0.5em; }
h1 { font-size: 2em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
h2 { font-size: 1.5em; border-bottom: 1px solid var(--border); padding-bottom: 0.3em; }
a { color: var(--accent); }
code, pre {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  font-size: 85%%;
  background: var(--code-bg);
  border-radius: 6px;
}
code { padding: 0.2em 0.4em; }
pre { padding: 1em; overflow-x: auto; }
pre code { padding: 0; background: none; }
blockquote { margin: 0; padding: 0 1em; color: var(--muted); border-left: 0.25em solid var(--border); }
table {
  border-collapse: collapse;
  display: block;
  width: max-content;
  max-width: calc(100vw - 3rem);
  overflow-x: auto;
  margin: 1em 0 1em 50%%;
  transform: translateX(-50%%);
}
th, td { border: 1px solid var(--border); padding: 6px 13px; }
th { font-weight: 600; }
tr:nth-child(2n) td { background: var(--code-bg); }
hr { border: 0; border-top: 1px solid var(--border); margin: 1.5em 0; }
img { max-width: 100%%; }
ul.contains-task-list { list-style: none; padding-left: 1em; }
pre.chroma { background: var(--code-bg) !important; }
pre.mermaid { background: none; text-align: center; }
%s
#theme-toggle {
  position: fixed;
  top: 1rem;
  right: 1rem;
  width: 2.25rem;
  height: 2.25rem;
  font-size: 1.1rem;
  line-height: 1;
  color: var(--fg);
  background: var(--code-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
}
</style>
</head>
<body>
<button id="theme-toggle" aria-label="Toggle light/dark mode"></button>
%s
<script>
(function () {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  var stored = localStorage.getItem("spec-preview-theme");
  if (stored) root.setAttribute("data-theme", stored);
  function effective() {
    return root.getAttribute("data-theme") ||
      (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  }
  function paint() { btn.textContent = effective() === "dark" ? "☀" : "☾"; }
  btn.addEventListener("click", function () {
    var next = effective() === "dark" ? "light" : "dark";
    root.setAttribute("data-theme", next);
    localStorage.setItem("spec-preview-theme", next);
    paint();
    if (window.__renderMermaid) window.__renderMermaid();
  });
  paint();
})();
</script>
%s</body>
</html>
`

// mermaidScript loads MermaidJS from CDN and renders diagram blocks
// client-side, re-rendering from the original source whenever the theme
// toggle flips so diagram colours follow the page theme. Only injected when
// the document contains mermaid blocks. Offline, diagrams degrade to their
// fenced source text.
const mermaidScript = `<script type="module">
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
var blocks = Array.from(document.querySelectorAll("pre.mermaid"));
blocks.forEach(function (b) { b.dataset.src = b.textContent; });
function diagramTheme() {
  var t = document.documentElement.getAttribute("data-theme") ||
    (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  return t === "dark" ? "dark" : "default";
}
window.__renderMermaid = function () {
  blocks.forEach(function (b) {
    b.removeAttribute("data-processed");
    b.textContent = b.dataset.src;
  });
  mermaid.initialize({ startOnLoad: false, theme: diagramTheme() });
  mermaid.run({ nodes: blocks });
};
window.__renderMermaid();
</script>
`

// renderSpecHTML converts spec markdown (front matter stripped) into a
// self-contained HTML document with embedded styling.
func renderSpecHTML(content, title string) (string, error) {
	var buf bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(
		extension.GFM,
		// Client-side rendering only: RenderModeAuto would silently switch
		// to server-side rendering when the mermaid CLI happens to be on
		// PATH. NoScript because the template injects its own theme-aware
		// loader script.
		&mermaid.Extender{RenderMode: mermaid.RenderModeClient, NoScript: true},
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	))
	if err := md.Convert([]byte(markdown.Body(content)), &buf); err != nil {
		return "", err
	}
	body := buf.String()
	script := ""
	if strings.Contains(body, `<pre class="mermaid">`) {
		script = mermaidScript
	}
	return fmt.Sprintf(htmlPreviewTemplate,
		html.EscapeString(title), highlightCSS(), body, script), nil
}

var (
	highlightCSSOnce sync.Once
	highlightCSSStr  string
)

// highlightCSS returns chroma token styles for both themes: GitHub light as
// the default, GitHub dark scoped to the same selectors the theme toggle and
// prefers-color-scheme fallback use, so highlighting follows the page theme.
func highlightCSS() string {
	highlightCSSOnce.Do(func() {
		f := chromahtml.New(chromahtml.WithClasses(true))
		var light, dark bytes.Buffer
		_ = f.WriteCSS(&light, styles.Get("github"))
		_ = f.WriteCSS(&dark, styles.Get("github-dark"))
		var b strings.Builder
		b.WriteString(light.String())
		b.WriteString(scopeCSS(dark.String(), `:root[data-theme="dark"]`))
		b.WriteString("@media (prefers-color-scheme: dark) {\n")
		b.WriteString(scopeCSS(dark.String(), `:root:not([data-theme="light"])`))
		b.WriteString("}\n")
		highlightCSSStr = b.String()
	})
	return highlightCSSStr
}

// scopeCSS prefixes every rule's selector with scope. Chroma's WriteCSS emits
// one rule per line, each as `/* TokenName */ .selector { ... }`.
func scopeCSS(css, scope string) string {
	var b strings.Builder
	for line := range strings.Lines(css) {
		brace := strings.Index(line, "{")
		if brace < 0 {
			b.WriteString(line)
			continue
		}
		sel := line[:brace]
		if end := strings.LastIndex(sel, "*/"); end >= 0 {
			b.WriteString(sel[:end+2])
			sel = sel[end+2:]
		}
		b.WriteString(" " + scope + " " + strings.TrimSpace(sel) + " " + line[brace:])
	}
	return b.String()
}

// previewServer serves rendered spec previews over localhost. A file-based
// approach (write HTML to $TMPDIR, open file:// URL) breaks on snap-confined
// browsers, which get a private /tmp and cannot read top-level hidden dirs in
// $HOME either — localhost HTTP is the one channel they can always reach.
// Specs render from disk per request, so a browser refresh picks up edits.
type previewServer struct {
	mu    sync.Mutex
	specs map[string]*config.ResolvedConfig
	addr  string
}

var preview previewServer

// register makes specID servable and returns its preview URL, starting the
// listener on first use. The server lives for the rest of the process.
func (s *previewServer) register(rc *config.ResolvedConfig, specID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.specs == nil {
		s.specs = make(map[string]*config.ResolvedConfig)
	}
	s.specs[specID] = rc
	if s.addr == "" {
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		s.addr = ln.Addr().String()
		go func() { _ = http.Serve(ln, s) }()
	}
	return "http://" + s.addr + "/" + specID, nil
}

func (s *previewServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	specID := strings.Trim(r.URL.Path, "/")
	s.mu.Lock()
	rc, ok := s.specs[specID]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(resolveLocalSpecPath(rc, specID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc, err := renderSpecHTML(string(data), specID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, doc)
}

// previewSpec serves the spec as rendered HTML on localhost and opens it in
// the default browser. Refreshing the page re-renders from disk.
func previewSpec(rc *config.ResolvedConfig, specID string) tea.Cmd {
	return func() tea.Msg {
		url, err := preview.register(rc, specID)
		if err != nil {
			return actionResultMsg{Action: "preview", SpecID: specID, Err: err}
		}
		err = browserCmd(url).Start()
		return actionResultMsg{Action: "preview", SpecID: specID, Detail: url, Err: err}
	}
}
