// Package confluence implements DocsAdapter using the Confluence REST API v2.
//
// Outbound: converts spec markdown to Confluence storage format (XHTML) and
// publishes the full page. Inserts <!-- spec-section: slug --> markers for
// reliable inbound re-mapping.
//
// Inbound: fetches the Confluence page, parses XHTML storage format back to
// markdown sections keyed by slug. The conversion is lossy for complex
// formatting but faithful for the structured content in spec sections (prose
// paragraphs, bullet lists, tables, code blocks, headings).
package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client implements adapter.DocsAdapter using the Confluence REST API.
type Client struct {
	baseURL  string // e.g. "https://myorg.atlassian.net/wiki"
	spaceKey string
	email    string
	token    string
	http     *http.Client

	// pageCache maps specID → pageID for the current session to avoid
	// redundant lookups. Not persisted across invocations.
	pageCache map[string]string
}

// NewClient creates a Confluence DocsAdapter.
// baseURL should include the /wiki path, e.g. "https://myorg.atlassian.net/wiki".
func NewClient(baseURL, spaceKey, email, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL:   baseURL,
		spaceKey:  spaceKey,
		email:     email,
		token:     token,
		http:      &http.Client{Timeout: 15 * time.Second},
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
// doesn't exist, or updates it if it does.
func (c *Client) PushFull(ctx context.Context, specID string, content string) error {
	storage := markdownToStorage(content, specID)

	pageID, err := c.findPage(ctx, specID)
	if err != nil {
		return err
	}

	if pageID == "" {
		return c.createPage(ctx, specID, storage)
	}
	return c.updatePage(ctx, pageID, specID, storage)
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

func (c *Client) findPage(ctx context.Context, specID string) (string, error) {
	if id, ok := c.pageCache[specID]; ok {
		return id, nil
	}

	// Search by title using CQL
	cql := fmt.Sprintf(`space="%s" AND title="%s"`, c.spaceKey, specID)
	url := fmt.Sprintf("%s/api/v2/pages?spaceKey=%s&title=%s&limit=1",
		c.baseURL, c.spaceKey, specID)

	resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("searching for page %s: %w — query: %s", specID, err, cql)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence API error (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var result pagesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing search response: %w", err)
	}

	if len(result.Results) == 0 {
		return "", nil
	}

	pageID := result.Results[0].ID
	c.pageCache[specID] = pageID
	return pageID, nil
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

func (c *Client) createPage(ctx context.Context, specID, storageBody string) error {
	payload := createPageRequest{
		SpaceID: c.spaceKey,
		Status:  "current",
		Title:   specID,
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
	if err := json.Unmarshal(body, &page); err == nil && page.ID != "" {
		c.pageCache[specID] = page.ID
	}
	return nil
}

func (c *Client) updatePage(ctx context.Context, pageID, specID, storageBody string) error {
	// Fetch current version for optimistic locking
	version, err := c.getPageVersion(ctx, pageID)
	if err != nil {
		return err
	}

	payload := updatePageRequest{
		ID:     pageID,
		Status: "current",
		Title:  specID,
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
// and bold/italic inline formatting. Inserts <!-- spec-section: slug --> markers
// before each heading for reliable inbound re-mapping.
func markdownToStorage(md, specID string) string {
	lines := strings.Split(md, "\n")
	var out strings.Builder
	inCodeBlock := false
	inList := false
	listType := "" // "ul" or "ol"

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Fenced code blocks
		if strings.HasPrefix(line, "```") {
			if inList {
				out.WriteString(closeList(listType))
				inList = false
			}
			if inCodeBlock {
				out.WriteString("</ac:plain-text-body></ac:structured-macro>\n")
				inCodeBlock = false
			} else {
				lang := strings.TrimPrefix(line, "```")
				lang = strings.TrimSpace(lang)
				out.WriteString(`<ac:structured-macro ac:name="code">`)
				if lang != "" {
					out.WriteString(fmt.Sprintf(`<ac:parameter ac:name="language">%s</ac:parameter>`, escapeXML(lang)))
				}
				out.WriteString("<ac:plain-text-body><![CDATA[")
				inCodeBlock = true
			}
			continue
		}
		if inCodeBlock {
			out.WriteString(escapeXML(line))
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
			out.WriteString(fmt.Sprintf("<!-- spec-section: %s -->\n", slug))
			out.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, formatInline(text), level))
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
			out.WriteString(fmt.Sprintf("<li>%s</li>\n", formatInline(text)))
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
			out.WriteString(fmt.Sprintf("<li>%s</li>\n", formatInline(text)))
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
		out.WriteString(fmt.Sprintf("<p>%s</p>\n", formatInline(trimmed)))
	}

	if inList {
		out.WriteString(closeList(listType))
	}
	if inCodeBlock {
		out.WriteString("</ac:plain-text-body></ac:structured-macro>\n")
	}

	return out.String()
}

// parseStorageToSections parses Confluence storage format back to markdown
// sections keyed by slug. Uses <!-- spec-section: slug --> markers for
// reliable mapping, falling back to heading-based slug derivation.
func parseStorageToSections(storage string) map[string]string {
	sections := make(map[string]string)

	// Split on spec-section markers
	markerPattern := regexp.MustCompile(`<!-- spec-section: ([a-z_]+) -->`)
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
	s = tablePattern.ReplaceAllStringFunc(s, func(match string) string {
		return storageTableToMarkdown(match)
	})

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

func formatInline(text string) string {
	// Bold: **text** → <strong>text</strong>
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "<strong>$1</strong>")
	// Italic: *text* → <em>text</em>
	text = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(text, "<em>$1</em>")
	// Inline code: `text` → <code>text</code>
	text = regexp.MustCompile("`(.+?)`").ReplaceAllString(text, "<code>$1</code>")
	return text
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
			out.WriteString(fmt.Sprintf("<%s>%s</%s>", tag, formatInline(cell), tag))
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

type pagesResponse struct {
	Results []pageResponse `json:"results"`
}

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
	SpaceID string   `json:"spaceId"`
	Status  string   `json:"status"`
	Title   string   `json:"title"`
	Body    pageBody `json:"body"`
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
