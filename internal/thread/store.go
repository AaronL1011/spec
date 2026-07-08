package thread

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Store is the engine boundary for thread persistence. Callers depend on this
// interface, never on the concrete backend, so a future backend (e.g. a server
// or local cache) needs no caller changes.
type Store interface {
	// List returns all threads for a spec, in deterministic order.
	List(specID string) ([]Thread, error)
	// Create appends a new open thread anchored to a section and returns it.
	// mentions are explicit handles from the caller (e.g. a --to flag); they
	// are unioned with @handles parsed from question. Pass nil when there are
	// no explicit handles — inline mentions are still parsed either way.
	Create(specID, section, author, question string, mentions []string) (Thread, error)
	// Reply appends a reply to an existing thread. mentions are unioned with
	// @handles parsed from body, same as Create.
	Reply(specID, threadID, author, body string, mentions []string) (Thread, error)
	// Resolve marks a thread resolved. Resolving an already-resolved thread
	// is a no-op that returns the thread unchanged.
	Resolve(specID, threadID, by string) (Thread, error)
}

// document is the on-disk shape of a sidecar file.
type document struct {
	Threads []Thread `yaml:"threads"`
}

// SidecarStore persists threads as <specsDir>/<SPEC-ID>.threads.yaml.
//
// The file is the only new tracked artifact: it sits beside the spec and syncs
// through the existing specs-repo git flow. Serialization is deterministic so
// independent edits diff cleanly and merge associatively.
type SidecarStore struct {
	// dir is the directory holding spec files (and their sidecars).
	dir string
	// now is injectable for tests; defaults to time.Now().UTC.
	now func() time.Time
}

// NewSidecarStore returns a store rooted at the given specs directory.
func NewSidecarStore(dir string) *SidecarStore {
	return &SidecarStore{dir: dir, now: func() time.Time { return time.Now().UTC() }}
}

// SidecarPath returns the sidecar file path for a spec ID.
func (s *SidecarStore) SidecarPath(specID string) string {
	return filepath.Join(s.dir, normalizeID(specID)+".threads.yaml")
}

// List loads and returns all threads for a spec. A missing sidecar is not an
// error — it simply means the spec has no threads yet.
func (s *SidecarStore) List(specID string) ([]Thread, error) {
	doc, err := s.load(specID)
	if err != nil {
		return nil, err
	}
	return doc.Threads, nil
}

// Create appends a new open thread. mentions are explicit handles from the
// caller (e.g. a --to flag), unioned with @handles parsed from question.
func (s *SidecarStore) Create(specID, section, author, question string, mentions []string) (Thread, error) {
	return s.CreateQuoted(specID, section, author, question, mentions, "", "")
}

// CreateQuoted appends a new open thread carrying an optional quote anchor:
// a verbatim text span within the section (plus a disambiguating prefix).
// An empty quote produces a plain section-level thread, identical to Create.
func (s *SidecarStore) CreateQuoted(specID, section, author, question string, mentions []string, quote, quotePrefix string) (Thread, error) {
	section = strings.TrimSpace(section)
	if section == "" {
		return Thread{}, fmt.Errorf("section is required — a thread must anchor to a section")
	}
	q, err := validateQuestion(question)
	if err != nil {
		return Thread{}, err
	}

	doc, err := s.load(specID)
	if err != nil {
		return Thread{}, err
	}

	quote = strings.TrimSpace(quote)
	if quote == "" {
		quotePrefix = "" // a prefix is meaningless without a quote
	}
	t := Thread{
		ID:          s.uniqueID(doc.Threads),
		Section:     section,
		Status:      StatusOpen,
		Author:      strings.TrimSpace(author),
		Created:     s.now(),
		Question:    q,
		Mentions:    unionMentions(ParseMentions(q), mentions),
		Quote:       quote,
		QuotePrefix: strings.TrimSpace(quotePrefix),
	}
	doc.Threads = append(doc.Threads, t)
	if err := s.save(specID, doc); err != nil {
		return Thread{}, err
	}
	return t, nil
}

// Reanchor moves a thread to a new section slug. It is the repair path for
// threads whose section heading was reworded out from under them (the
// "unanchored" bucket in the reader) — an ordinary sidecar mutation that
// merges by thread ID like any other.
func (s *SidecarStore) Reanchor(specID, threadID, newSection string) (Thread, error) {
	newSection = strings.TrimSpace(newSection)
	if newSection == "" {
		return Thread{}, fmt.Errorf("section is required — a thread must anchor to a section")
	}
	doc, err := s.load(specID)
	if err != nil {
		return Thread{}, err
	}
	idx := indexOf(doc.Threads, threadID)
	if idx < 0 {
		return Thread{}, fmt.Errorf("thread %s not found in %s", threadID, normalizeID(specID))
	}
	if doc.Threads[idx].Section == newSection {
		return doc.Threads[idx], nil // already anchored there — idempotent
	}
	doc.Threads[idx].Section = newSection
	if err := s.save(specID, doc); err != nil {
		return Thread{}, err
	}
	return doc.Threads[idx], nil
}

// Reply appends a reply to an existing thread. mentions are unioned with
// @handles parsed from body, same as Create.
func (s *SidecarStore) Reply(specID, threadID, author, body string, mentions []string) (Thread, error) {
	b, err := validateReply(body)
	if err != nil {
		return Thread{}, err
	}

	doc, err := s.load(specID)
	if err != nil {
		return Thread{}, err
	}
	idx := indexOf(doc.Threads, threadID)
	if idx < 0 {
		return Thread{}, fmt.Errorf("thread %s not found in %s", threadID, normalizeID(specID))
	}

	doc.Threads[idx].Replies = append(doc.Threads[idx].Replies, Reply{
		Author:   strings.TrimSpace(author),
		At:       s.now(),
		Body:     b,
		Mentions: unionMentions(ParseMentions(b), mentions),
	})
	if err := s.save(specID, doc); err != nil {
		return Thread{}, err
	}
	return doc.Threads[idx], nil
}

// Resolve marks a thread resolved.
func (s *SidecarStore) Resolve(specID, threadID, by string) (Thread, error) {
	doc, err := s.load(specID)
	if err != nil {
		return Thread{}, err
	}
	idx := indexOf(doc.Threads, threadID)
	if idx < 0 {
		return Thread{}, fmt.Errorf("thread %s not found in %s", threadID, normalizeID(specID))
	}
	if !doc.Threads[idx].IsOpen() {
		return doc.Threads[idx], nil // already resolved — idempotent
	}

	at := s.now()
	doc.Threads[idx].Status = StatusResolved
	doc.Threads[idx].ResolvedBy = strings.TrimSpace(by)
	doc.Threads[idx].ResolvedAt = &at
	if err := s.save(specID, doc); err != nil {
		return Thread{}, err
	}
	return doc.Threads[idx], nil
}

// ── internals ──────────────────────────────────────────────────────────────

func (s *SidecarStore) load(specID string) (document, error) {
	path := s.SidecarPath(specID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return document{}, nil
		}
		return document{}, fmt.Errorf("reading threads for %s: %w", normalizeID(specID), err)
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("parsing threads for %s: %w", normalizeID(specID), err)
	}
	return doc, nil
}

// save writes the sidecar atomically with deterministic ordering. An empty
// thread set removes the sidecar so a thread-free spec leaves no artifact.
func (s *SidecarStore) save(specID string, doc document) error {
	path := s.SidecarPath(specID)
	if len(doc.Threads) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing empty threads file for %s: %w", normalizeID(specID), err)
		}
		return nil
	}

	sortThreads(doc.Threads)
	data, err := marshal(doc)
	if err != nil {
		return fmt.Errorf("serializing threads for %s: %w", normalizeID(specID), err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating threads dir for %s: %w", normalizeID(specID), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing threads for %s: %w", normalizeID(specID), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("committing threads for %s: %w", normalizeID(specID), err)
	}
	return nil
}

func (s *SidecarStore) uniqueID(existing []Thread) string {
	for {
		id := newID()
		if indexOf(existing, id) < 0 {
			return id
		}
	}
}

// marshal serializes a document deterministically.
func marshal(doc document) ([]byte, error) {
	return yaml.Marshal(doc)
}

// sortThreads orders threads by creation time then ID, and replies by time,
// so re-serialization of unchanged content produces no diff.
func sortThreads(threads []Thread) {
	sort.SliceStable(threads, func(i, j int) bool {
		if threads[i].Created.Equal(threads[j].Created) {
			return threads[i].ID < threads[j].ID
		}
		return threads[i].Created.Before(threads[j].Created)
	})
	for i := range threads {
		sort.SliceStable(threads[i].Replies, func(a, b int) bool {
			return threads[i].Replies[a].At.Before(threads[i].Replies[b].At)
		})
	}
}

func indexOf(threads []Thread, id string) int {
	for i, t := range threads {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func normalizeID(specID string) string {
	return strings.ToUpper(strings.TrimSpace(specID))
}
