package thread

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse decodes a sidecar document body into its thread set. An empty body
// yields an empty (non-nil-error) result so callers can merge cleanly.
func Parse(data []byte) ([]Thread, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing thread sidecar: %w", err)
	}
	return doc.Threads, nil
}

// Marshal serializes a thread set into the deterministic sidecar document
// shape, applying the same ordering as the on-disk store so merged output
// diffs cleanly.
func Marshal(threads []Thread) ([]byte, error) {
	sortThreads(threads)
	return marshal(document{Threads: threads})
}

// Merge reconciles two thread sets into one. It is used to resolve the rare
// case where two reviewers edited the same sidecar offline.
//
// Strategy:
//   - Threads are unioned by ID. A thread present in only one side is kept.
//   - For a thread present in both sides, replies are unioned (same author +
//     timestamp + body counts as the same reply); the resolved state wins if
//     either side resolved it. This makes merges associative and never drops a
//     reply.
//
// The result is returned in deterministic order so a merged file diffs cleanly.
func Merge(a, b []Thread) []Thread {
	index := make(map[string]int, len(a)+len(b))
	merged := make([]Thread, 0, len(a)+len(b))

	add := func(t Thread) {
		if i, ok := index[t.ID]; ok {
			merged[i] = mergeThread(merged[i], t)
			return
		}
		index[t.ID] = len(merged)
		merged = append(merged, t)
	}
	for _, t := range a {
		add(t)
	}
	for _, t := range b {
		add(t)
	}

	sortThreads(merged)
	return merged
}

// mergeThread combines two versions of the same thread.
func mergeThread(x, y Thread) Thread {
	out := x

	// Union replies, deduping on (author, timestamp, body).
	seen := make(map[replyKey]bool, len(x.Replies)+len(y.Replies))
	var replies []Reply
	for _, r := range append(append([]Reply{}, x.Replies...), y.Replies...) {
		k := replyKey{r.Author, r.At.UnixNano(), r.Body}
		if seen[k] {
			continue
		}
		seen[k] = true
		replies = append(replies, r)
	}
	out.Replies = replies

	// Resolution wins: if either side resolved, the thread is resolved.
	if !x.IsOpen() {
		out.Status, out.ResolvedBy, out.ResolvedAt = x.Status, x.ResolvedBy, x.ResolvedAt
	} else if !y.IsOpen() {
		out.Status, out.ResolvedBy, out.ResolvedAt = y.Status, y.ResolvedBy, y.ResolvedAt
	}
	return out
}

type replyKey struct {
	author string
	atNano int64
	body   string
}
