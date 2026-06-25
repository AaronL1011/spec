// Package confluence implements DocsAdapter using the Confluence REST API.
//
// Outbound (the hardened, primary path): converts spec markdown to Confluence
// storage format (XHTML) and publishes the full page as a child of a
// configured parent page. Frontmatter is rendered as a metadata info panel,
// markdown links and inline formatting are preserved, and all prose is
// XML-escaped so arbitrary spec content (e.g. "List<T>", "a < b", "Q&A")
// produces well-formed storage XML. Pages are bound to their spec by a
// durable Confluence label so lookups survive human title edits. Section
// markers (<!-- spec-section: slug -->) are still emitted to support inbound.
//
// Inbound (best-effort): fetches the Confluence page and parses XHTML storage
// format back to markdown sections keyed by slug. The conversion is lossy for
// complex formatting and depends on HTML-comment markers that Confluence's
// editor may strip; it is retained for compatibility but not the focus of the
// hardened mirror.
package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
)

// defaultTimeout is the per-request timeout for Confluence API calls. It can
// be overridden via Options.Timeout (config key docs.request_timeout).
const defaultTimeout = 10 * time.Second

// Options configures a Confluence DocsAdapter.
type Options struct {
	// BaseURL includes the /wiki path, e.g. "https://myorg.atlassian.net/wiki".
	BaseURL string
	// SpaceKey is the human space key (e.g. "ENG"). Resolved to a numeric
	// space id lazily for page creation.
	SpaceKey string
	// ParentID is the numeric id of the parent page under which spec pages are
	// created. Required: it keeps the mirror navigable instead of dumping
	// pages at the space root.
	ParentID string
	Email    string
	Token    string
	// Timeout overrides defaultTimeout when non-zero.
	Timeout time.Duration
}

// Client implements adapter.DocsAdapter using the Confluence REST API.
type Client struct {
	baseURL  string // e.g. "https://myorg.atlassian.net/wiki"
	spaceKey string
	parentID string
	email    string
	token    string
	http     *http.Client

	mu sync.Mutex
	// spaceID is the numeric space id resolved lazily from spaceKey.
	spaceID string
	// pageCache maps specID → pageID for the current session to avoid
	// redundant lookups. Not persisted across invocations.
	pageCache map[string]string
}

// NewClient creates a Confluence DocsAdapter from Options.
func NewClient(opts Options) *Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL:   strings.TrimRight(opts.BaseURL, "/"),
		spaceKey:  opts.SpaceKey,
		parentID:  opts.ParentID,
		email:     opts.Email,
		token:     opts.Token,
		http:      &http.Client{Timeout: timeout},
		pageCache: make(map[string]string),
	}
}

// FetchSections retrieves the spec page from Confluence and returns section
// content keyed by slug. Sections are identified by <!-- spec-section: slug -->
// markers inserted during outbound push, or by heading-based slug derivation.
func (c *Client) FetchSections(ctx context.Context, specID string) (map[string]string, error) {
	pageID, err := c.findPage(ctx, specID)
	if err != nil {
		return nil, err
	}
	if pageID == "" {
		return nil, nil // page doesn't exist yet — not an error
	}

	storage, err := c.getPageBody(ctx, pageID)
	if err != nil {
		return nil, err
	}

	return parseStorageToSections(storage), nil
}

// PushFull publishes the complete spec to Confluence. Creates the page if it
// doesn't exist, or updates it if it does. The page title is derived from the
// spec frontmatter ("SPEC-042 — Title") for readability; identity is bound to a
// durable label, not the title, so renames don't orphan the mirror.
func (c *Client) PushFull(ctx context.Context, specID string, content string) error {
	meta, _ := markdown.ParseMeta(content) // best-effort: nil meta degrades to specID title
	title := pageTitle(specID, meta)
	storage := markdownToStorage(content, specID, meta)

	pageID, err := c.findPage(ctx, specID)
	if err != nil {
		return err
	}

	if pageID == "" {
		return c.createPage(ctx, specID, title, storage)
	}
	return c.updatePage(ctx, pageID, title, storage)
}

// PageURL returns the URL of the spec's Confluence page.
func (c *Client) PageURL(ctx context.Context, specID string) (string, error) {
	pageID, err := c.findPage(ctx, specID)
	if err != nil {
		return "", err
	}
	if pageID == "" {
		return "", fmt.Errorf("no Confluence page found for %s", specID)
	}
	return fmt.Sprintf("%s/pages/%s", c.baseURL, pageID), nil
}

// --- Page CRUD ---

// findPage locates the Confluence page mirroring specID by its durable label.
// Label search (via the stable v1 CQL endpoint) is space-scoped and survives
// human edits to the page title, unlike a title match. Returns "" when no page
// exists yet (not an error).
func (c *Client) findPage(ctx context.Context, specID string) (string, error) {
	c.mu.Lock()
	if id, ok := c.pageCache[specID]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	cql := fmt.Sprintf(`space="%s" AND label="%s" AND type=page`, c.spaceKey, specLabel(specID))
	endpoint := fmt.Sprintf("%s/rest/api/content/search?cql=%s&limit=2", c.baseURL, url.QueryEscape(cql))

	resp, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("searching for page %s: %w", specID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence search error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result searchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing search response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", nil
	}
	if len(result.Results) > 1 {
		return "", fmt.Errorf("multiple Confluence pages labelled for %s in space %s — remove the duplicate so the mirror has a single target", specID, c.spaceKey)
	}

	pageID := result.Results[0].ID
	c.cachePage(specID, pageID)
	return pageID, nil
}

// cachePage records a specID→pageID mapping for the session.
func (c *Client) cachePage(specID, pageID string) {
	c.mu.Lock()
	c.pageCache[specID] = pageID
	c.mu.Unlock()
}

// resolveSpaceID resolves the configured space key to its numeric space id,
// which the v2 create-page endpoint requires (it rejects the key). The result
// is cached for the session.
func (c *Client) resolveSpaceID(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.spaceID != "" {
		id := c.spaceID
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	endpoint := fmt.Sprintf("%s/api/v2/spaces?keys=%s&limit=2", c.baseURL, url.QueryEscape(c.spaceKey))
	resp, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("resolving space %q: %w", c.spaceKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading spaces response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence spaces error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result spacesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing spaces response: %w", err)
	}
	if len(result.Results) == 0 {
		return "", fmt.Errorf("confluence space %q not found — check docs.space_key", c.spaceKey)
	}

	id := result.Results[0].ID
	c.mu.Lock()
	c.spaceID = id
	c.mu.Unlock()
	return id, nil
}

func (c *Client) getPageBody(ctx context.Context, pageID string) (string, error) {
	url := fmt.Sprintf("%s/api/v2/pages/%s?body-format=storage", c.baseURL, pageID)

	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading page body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence API error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var page pageResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return "", fmt.Errorf("parsing page: %w", err)
	}

	return page.Body.Storage.Value, nil
}

func (c *Client) createPage(ctx context.Context, specID, title, storageBody string) error {
	spaceID, err := c.resolveSpaceID(ctx)
	if err != nil {
		return err
	}
	if c.parentID == "" {
		return fmt.Errorf("confluence parent page not configured — set docs.parent_page_id so the %s mirror has a home", specID)
	}

	payload := createPageRequest{
		SpaceID:  spaceID,
		ParentID: c.parentID,
		Status:   "current",
		Title:    title,
		Body: pageBody{
			Representation: "storage",
			Value:          storageBody,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling create page: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/pages", c.baseURL)
	resp, err := c.doRequest(ctx, http.MethodPost, url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading create response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("confluence create page error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var page pageResponse
	if err := json.Unmarshal(body, &page); err != nil || page.ID == "" {
		return fmt.Errorf("confluence create page for %s returned no id: %s", specID, truncate(string(body), 200))
	}
	c.cachePage(specID, page.ID)

	// Bind the page to the spec with a durable label so future lookups survive
	// title edits. A failure here would orphan the page from findPage, so it is
	// fatal: the operator can re-run once the cause is resolved.
	if err := c.attachLabel(ctx, page.ID, specLabel(specID)); err != nil {
		return fmt.Errorf("labelling %s page: %w", specID, err)
	}
	return nil
}

// attachLabel adds a global label to a page via the stable v1 endpoint (v2
// exposes labels read-only).
func (c *Client) attachLabel(ctx context.Context, pageID, label string) error {
	payload := []labelRequest{{Prefix: "global", Name: label}}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling label: %w", err)
	}
	url := fmt.Sprintf("%s/rest/api/content/%s/label", c.baseURL, pageID)
	resp, err := c.doRequest(ctx, http.MethodPost, url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("confluence label error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 300))
	}
	return nil
}

func (c *Client) updatePage(ctx context.Context, pageID, title, storageBody string) error {
	// Fetch current version for optimistic locking
	version, err := c.getPageVersion(ctx, pageID)
	if err != nil {
		return err
	}

	payload := updatePageRequest{
		ID:     pageID,
		Status: "current",
		Title:  title,
		Body: pageBody{
			Representation: "storage",
			Value:          storageBody,
		},
		Version: versionRef{Number: version + 1},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling update page: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/pages/%s", c.baseURL, pageID)
	resp, err := c.doRequest(ctx, http.MethodPut, url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("confluence update page error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	return nil
}

func (c *Client) getPageVersion(ctx context.Context, pageID string) (int, error) {
	url := fmt.Sprintf("%s/api/v2/pages/%s", c.baseURL, pageID)
	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var page pageResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return 0, err
	}
	return page.Version.Number, nil
}

func (c *Client) doRequest(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating Confluence request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.email, c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Confluence API: %w", err)
	}
	return resp, nil
}

// --- Markdown ↔ Storage Format conversion ---

// markdownToStorage converts spec markdown to Confluence storage format (XHTML).
// Handles: headings, paragraphs, bullet/numbered lists, code blocks, tables,
// links, and bold/italic/code inline formatting. The YAML frontmatter is
// stripped and rendered as a metadata info panel at the top of the page. All
// text is XML-escaped so arbitrary spec prose produces well-formed storage XML.
// Inserts <!-- spec-section: slug --> markers before each heading for inbound.
//
// codeMacroClose MUST lead with "]]>" to terminate the CDATA section opened by
// the code macro. Omitting it leaves the CDATA open, so Confluence swallows the
// entire rest of the page into the first code block — producing malformed
// storage XML that the v2 API silently mangles on render.
const codeMacroClose = "]]></ac:plain-text-body></ac:structured-macro>\n"

func markdownToStorage(md, specID string, meta *markdown.SpecMeta) string {
	body := stripFrontmatter(md)
	lines := strings.Split(body, "\n")
	var out strings.Builder
	out.WriteString(metadataPanel(specID, meta))
	inCodeBlock := false
	var fenceChar byte // '`' or '~' of the open fence
	fenceLen := 0      // run length of the open fence
	inList := false
	listType := "" // "ul" or "ol"

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Fenced code blocks. Fences are matched after trimming leading
		// indentation, and a block only closes on a fence of the SAME character
		// that is at least as long as the opener with no info string — so an
		// indented closing fence (the common cause of "everything after a code
		// block renders inside it") and nested longer fences both work.
		if char, runLen, info, ok := parseFence(line); ok {
			if inCodeBlock {
				if char == fenceChar && runLen >= fenceLen && info == "" {
					out.WriteString(codeMacroClose)
					inCodeBlock = false
					fenceChar, fenceLen = 0, 0
					continue
				}
				// A shorter/mismatched fence inside the block is literal content.
				out.WriteString(escapeCDATA(line))
				out.WriteString("\n")
				continue
			}
			if inList {
				out.WriteString(closeList(listType))
				inList = false
			}
			inCodeBlock = true
			fenceChar, fenceLen = char, runLen
			out.WriteString(`<ac:structured-macro ac:name="code">`)
			if lang := firstToken(info); lang != "" {
				fmt.Fprintf(&out, `<ac:parameter ac:name="language">%s</ac:parameter>`, escapeXML(lang))
			}
			out.WriteString("<ac:plain-text-body><![CDATA[")
			continue
		}
		if inCodeBlock {
			// CDATA content is raw; only the "]]>" terminator must be split so
			// it does not close the section prematurely.
			out.WriteString(escapeCDATA(line))
			out.WriteString("\n")
			continue
		}

		// Headings
		if level, text := parseHeading(line); level > 0 {
			if inList {
				out.WriteString(closeList(listType))
				inList = false
			}
			slug := slugify(text)
			fmt.Fprintf(&out, "<!-- spec-section: %s -->\n", slug)
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", level, formatInline(text), level)
			continue
		}

		// Unordered list items
		if strings.HasPrefix(strings.TrimSpace(line), "- ") || strings.HasPrefix(strings.TrimSpace(line), "* ") {
			if !inList || listType != "ul" {
				if inList {
					out.WriteString(closeList(listType))
				}
				out.WriteString("<ul>\n")
				inList = true
				listType = "ul"
			}
			text := strings.TrimSpace(line)
			text = strings.TrimPrefix(text, "- ")
			text = strings.TrimPrefix(text, "* ")
			fmt.Fprintf(&out, "<li>%s</li>\n", formatInline(text))
			continue
		}

		// Ordered list items
		if isOrderedListItem(line) {
			if !inList || listType != "ol" {
				if inList {
					out.WriteString(closeList(listType))
				}
				out.WriteString("<ol>\n")
				inList = true
				listType = "ol"
			}
			text := orderedListText(line)
			fmt.Fprintf(&out, "<li>%s</li>\n", formatInline(text))
			continue
		}

		// Table rows
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			if inList {
				out.WriteString(closeList(listType))
				inList = false
			}
			// Collect all table lines
			tableLines := []string{line}
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				tableLines = append(tableLines, lines[i])
			}
			out.WriteString(convertTable(tableLines))
			continue
		}

		// Close list if we hit non-list content
		if inList {
			out.WriteString(closeList(listType))
			inList = false
		}

		// Blank lines
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Frontmatter delimiter
		if trimmed == "---" {
			out.WriteString("<hr/>\n")
			continue
		}

		// Paragraph
		fmt.Fprintf(&out, "<p>%s</p>\n", formatInline(trimmed))
	}

	if inList {
		out.WriteString(closeList(listType))
	}
	if inCodeBlock {
		// Close an unterminated trailing block (author forgot the closing fence).
		out.WriteString(codeMacroClose)
	}

	return out.String()
}

// parseStorageToSections parses Confluence storage format back to markdown
// sections keyed by slug. Uses <!-- spec-section: slug --> markers for
// reliable mapping, falling back to heading-based slug derivation.
func parseStorageToSections(storage string) map[string]string {
	sections := make(map[string]string)

	// Split on spec-section markers
	markerPattern := regexp.MustCompile(`<!--\s*spec-section:\s*([a-z0-9_]+)\s*-->`)
	headingPattern := regexp.MustCompile(`<h(\d)>(.*?)</h\d>`)

	parts := markerPattern.Split(storage, -1)
	slugs := markerPattern.FindAllStringSubmatch(storage, -1)

	if len(slugs) == 0 {
		// No markers — fall back to heading extraction
		return parseStorageByHeadings(storage)
	}

	for i, slug := range slugs {
		if i+1 < len(parts) {
			content := parts[i+1]
			// Strip the heading tag itself — we only want the body
			headingLoc := headingPattern.FindStringIndex(content)
			if headingLoc != nil {
				content = content[headingLoc[1]:]
			}
			sections[slug[1]] = storageToMarkdown(strings.TrimSpace(content))
		}
	}

	return sections
}

// parseStorageByHeadings extracts sections from storage format without markers.
func parseStorageByHeadings(storage string) map[string]string {
	headingPattern := regexp.MustCompile(`<h(\d)>(.*?)</h\d>`)
	matches := headingPattern.FindAllStringSubmatchIndex(storage, -1)

	sections := make(map[string]string)
	for i, match := range matches {
		heading := storage[match[4]:match[5]]
		slug := slugify(stripTags(heading))

		start := match[1] // end of heading tag
		end := len(storage)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := storage[start:end]
		sections[slug] = storageToMarkdown(strings.TrimSpace(body))
	}
	return sections
}

// storageToMarkdown converts a fragment of Confluence storage format back to markdown.
// Handles: paragraphs, lists, code blocks, tables, inline formatting.
func storageToMarkdown(storage string) string {
	s := storage

	// Code blocks
	codePattern := regexp.MustCompile(`<ac:structured-macro ac:name="code">(?:<ac:parameter ac:name="language">([^<]*)</ac:parameter>)?<ac:plain-text-body><!\[CDATA\[(.*?)\]\]></ac:plain-text-body></ac:structured-macro>`)
	s = codePattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := codePattern.FindStringSubmatch(match)
		lang := ""
		code := ""
		if len(sub) >= 3 {
			lang = sub[1]
			code = sub[2]
		}
		return fmt.Sprintf("```%s\n%s```", lang, code)
	})

	// Tables
	tablePattern := regexp.MustCompile(`<table>.*?</table>`)
	s = tablePattern.ReplaceAllStringFunc(s, storageTableToMarkdown)

	// Lists
	s = regexp.MustCompile(`<ul>\s*`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s*</ul>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<ol>\s*`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s*</ol>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`<li>(.*?)</li>`).ReplaceAllString(s, "- $1")

	// Paragraphs
	s = regexp.MustCompile(`<p>(.*?)</p>`).ReplaceAllString(s, "$1\n")

	// Inline formatting
	s = regexp.MustCompile(`<strong>(.*?)</strong>`).ReplaceAllString(s, "**$1**")
	s = regexp.MustCompile(`<em>(.*?)</em>`).ReplaceAllString(s, "*$1*")
	s = regexp.MustCompile(`<code>(.*?)</code>`).ReplaceAllString(s, "`$1`")

	// Horizontal rules
	s = strings.ReplaceAll(s, "<hr/>", "---")

	// Strip remaining tags
	s = stripTags(s)

	// Clean up whitespace
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func storageTableToMarkdown(table string) string {
	rowPattern := regexp.MustCompile(`<tr>(.*?)</tr>`)
	cellPattern := regexp.MustCompile(`<t[hd](?:\s[^>]*)?>(.*?)</t[hd]>`)

	rows := rowPattern.FindAllStringSubmatch(table, -1)
	if len(rows) == 0 {
		return ""
	}

	var md strings.Builder
	for i, row := range rows {
		cells := cellPattern.FindAllStringSubmatch(row[1], -1)
		md.WriteString("|")
		for _, cell := range cells {
			md.WriteString(" ")
			md.WriteString(stripTags(cell[1]))
			md.WriteString(" |")
		}
		md.WriteString("\n")

		// Add separator after header row
		if i == 0 {
			md.WriteString("|")
			for range cells {
				md.WriteString("---|")
			}
			md.WriteString("\n")
		}
	}
	return md.String()
}

// --- Helpers ---

func parseHeading(line string) (int, string) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for _, c := range trimmed {
		if c == '#' {
			level++
		} else {
			break
		}
	}
	if level == 0 || level > 6 {
		return 0, ""
	}
	text := strings.TrimSpace(trimmed[level:])
	// Strip <!-- owner: ... --> comments from heading text
	if idx := strings.Index(text, "<!--"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return level, text
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(text string) string {
	// Strip section numbers like "1." or "7.3"
	text = regexp.MustCompile(`^\d+(\.\d+)*\.?\s*`).ReplaceAllString(text, "")
	text = strings.ToLower(text)
	text = slugPattern.ReplaceAllString(text, "_")
	text = strings.Trim(text, "_")
	return text
}

var (
	inlineLink        = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	inlineBold        = regexp.MustCompile(`\*\*(.+?)\*\*`)
	inlineItalic      = regexp.MustCompile(`\*(.+?)\*`)
	inlineCode        = regexp.MustCompile("`(.+?)`")
	inlinePlaceholder = regexp.MustCompile("\x00(\\d+)\x00")
)

// formatInline escapes XML special characters in markdown text, then converts
// inline markdown (links, code, bold, italic) into storage-format tags.
//
// Code spans and links are extracted to placeholders BEFORE emphasis is applied,
// then restored afterward. This is essential: without it, a stray '*' inside one
// code span (e.g. "`SCHEDULED_JOB_*`") and another in a later span let the italic
// regex wrap <em> across the intervening </code>...<code> tags, producing
// interleaved, malformed storage XML. Isolating emphasis to plain text keeps the
// output well-formed. Escaping first also turns literal '<','>','&' (e.g.
// "List<T>", "Q&A") into valid entities.
func formatInline(text string) string {
	text = escapeXML(text)

	stash := make([]string, 0, 4)
	protect := func(rendered string) string {
		stash = append(stash, rendered)
		return fmt.Sprintf("\x00%d\x00", len(stash)-1)
	}

	// Inline code first — its content is verbatim and must not be re-formatted.
	text = inlineCode.ReplaceAllStringFunc(text, func(m string) string {
		return protect("<code>" + inlineCode.FindStringSubmatch(m)[1] + "</code>")
	})
	// Links next: [text](url) → <a href="url">text</a>. The url was already
	// escaped for '&'/'<'/'>'; escapeAttrQuotes covers the attribute quote case.
	text = inlineLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := inlineLink.FindStringSubmatch(m)
		return protect(fmt.Sprintf(`<a href="%s">%s</a>`, escapeAttrQuotes(sub[2]), sub[1]))
	})
	// Emphasis now runs only over plain text containing no tags or backticks.
	text = inlineBold.ReplaceAllString(text, "<strong>$1</strong>")
	text = inlineItalic.ReplaceAllString(text, "<em>$1</em>")

	// Restore protected spans.
	return inlinePlaceholder.ReplaceAllStringFunc(text, func(m string) string {
		idx, err := strconv.Atoi(inlinePlaceholder.FindStringSubmatch(m)[1])
		if err != nil || idx < 0 || idx >= len(stash) {
			return m
		}
		return stash[idx]
	})
}

// parseFence reports whether a line is a Markdown code fence (after trimming
// leading indentation), returning the fence character ('`' or '~'), its run
// length (>=3), and the trimmed info string. Per CommonMark, a backtick fence's
// info string may not contain a backtick, which keeps inline code like `x` from
// being mistaken for a fence.
func parseFence(line string) (char byte, runLen int, info string, ok bool) {
	s := strings.TrimLeft(line, " \t")
	if len(s) < 3 {
		return 0, 0, "", false
	}
	c := s[0]
	if c != '`' && c != '~' {
		return 0, 0, "", false
	}
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info = strings.TrimSpace(s[n:])
	if c == '`' && strings.Contains(info, "`") {
		return 0, 0, "", false
	}
	return c, n, info, true
}

// firstToken returns the first whitespace-delimited token of a fence info
// string — the language hint Confluence's code macro expects.
func firstToken(info string) string {
	if fields := strings.Fields(info); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// frontmatterDelim matches the YAML frontmatter fence at the very top of a spec.
var frontmatterDelim = regexp.MustCompile(`(?s)\A\s*---\n.*?\n---\n?`)

// stripFrontmatter removes the leading YAML frontmatter block so it does not
// leak into the page body as stray paragraphs (it is rendered as a panel
// instead). Content without frontmatter is returned unchanged.
func stripFrontmatter(md string) string {
	return frontmatterDelim.ReplaceAllString(md, "")
}

// pageTitle builds a human-friendly Confluence page title from the spec
// frontmatter. Falls back to the bare specID when no usable title is present.
func pageTitle(specID string, meta *markdown.SpecMeta) string {
	if meta == nil {
		return specID
	}
	title := strings.TrimSpace(meta.Title)
	// Ignore the template placeholder so unfilled drafts don't get an ugly title.
	if title == "" || strings.HasPrefix(title, "[") {
		return specID
	}
	return fmt.Sprintf("%s — %s", specID, title)
}

// specLabel returns the durable Confluence label binding a page to its spec.
// Labels are lowercased and restricted to [a-z0-9-]; the spec prefix namespaces
// them so they don't collide with unrelated team labels.
func specLabel(specID string) string {
	slug := strings.ToLower(strings.TrimSpace(specID))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return "spec-id-" + slug
}

// metadataPanel renders selected frontmatter fields as a Confluence "info"
// panel containing a key/value table, giving external readers spec context
// (status, owner, cycle, repos) without exposing raw YAML.
func metadataPanel(specID string, meta *markdown.SpecMeta) string {
	rows := [][2]string{{"Spec", specID}}
	if meta != nil {
		rows = appendMetaRow(rows, "Status", meta.Status)
		rows = appendMetaRow(rows, "Author", meta.Author)
		rows = appendMetaRow(rows, "Cycle", meta.Cycle)
		rows = appendMetaRow(rows, "Version", meta.Version)
		rows = appendMetaRow(rows, "Epic", meta.EpicKey)
		rows = appendMetaRow(rows, "Repos", strings.Join(meta.Repos, ", "))
		rows = appendMetaRow(rows, "Updated", meta.Updated)
	}

	var b strings.Builder
	b.WriteString(`<ac:structured-macro ac:name="info"><ac:rich-text-body><table><tbody>`)
	for _, row := range rows {
		fmt.Fprintf(&b, "<tr><th>%s</th><td>%s</td></tr>", escapeXML(row[0]), escapeXML(row[1]))
	}
	b.WriteString("</tbody></table></ac:rich-text-body></ac:structured-macro>\n")
	return b.String()
}

func appendMetaRow(rows [][2]string, key, value string) [][2]string {
	if strings.TrimSpace(value) == "" {
		return rows
	}
	return append(rows, [2]string{key, value})
}

func isOrderedListItem(line string) bool {
	trimmed := strings.TrimSpace(line)
	for i, c := range trimmed {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && i > 0 && i < len(trimmed)-1 && trimmed[i+1] == ' ' {
			return true
		}
		return false
	}
	return false
}

func orderedListText(line string) string {
	trimmed := strings.TrimSpace(line)
	idx := strings.Index(trimmed, ". ")
	if idx < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[idx+2:])
}

func closeList(listType string) string {
	return fmt.Sprintf("</%s>\n", listType)
}

func convertTable(lines []string) string {
	var out strings.Builder
	out.WriteString("<table>\n")

	for i, line := range lines {
		cells := parseTableRow(line)
		if len(cells) == 0 {
			continue
		}
		// Skip separator rows (|---|---|)
		if isSeparatorRow(cells) {
			continue
		}

		out.WriteString("<tr>")
		tag := "td"
		if i == 0 {
			tag = "th"
		}
		for _, cell := range cells {
			fmt.Fprintf(&out, "<%s>%s</%s>", tag, formatInline(cell), tag)
		}
		out.WriteString("</tr>\n")
	}

	out.WriteString("</table>\n")
	return out.String()
}

func parseTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.Trim(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

func isSeparatorRow(cells []string) bool {
	for _, cell := range cells {
		cleaned := strings.TrimSpace(cell)
		cleaned = strings.Trim(cleaned, "-:")
		if cleaned != "" {
			return false
		}
	}
	return true
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// escapeAttrQuotes escapes the double-quote character for safe use inside an
// XML attribute value. '&', '<' and '>' are assumed already escaped upstream.
func escapeAttrQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, "&quot;")
}

// escapeCDATA makes a line safe inside a <![CDATA[ ... ]]> block by splitting
// any "]]>" terminator so it cannot close the section early. The content is
// otherwise emitted raw — CDATA must not XML-escape, or entities render literally.
func escapeCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// --- API types ---

type pageResponse struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

type createPageRequest struct {
	SpaceID  string   `json:"spaceId"`
	ParentID string   `json:"parentId,omitempty"`
	Status   string   `json:"status"`
	Title    string   `json:"title"`
	Body     pageBody `json:"body"`
}

// searchResponse models the v1 /rest/api/content/search payload used for
// durable label-based page lookup.
type searchResponse struct {
	Results []struct {
		ID string `json:"id"`
	} `json:"results"`
}

// spacesResponse models the v2 /api/v2/spaces payload used to resolve a space
// key to its numeric id.
type spacesResponse struct {
	Results []struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"results"`
}

// labelRequest is one entry in the v1 add-label payload.
type labelRequest struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

type updatePageRequest struct {
	ID      string     `json:"id"`
	Status  string     `json:"status"`
	Title   string     `json:"title"`
	Body    pageBody   `json:"body"`
	Version versionRef `json:"version"`
}

type pageBody struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

type versionRef struct {
	Number int `json:"number"`
}
