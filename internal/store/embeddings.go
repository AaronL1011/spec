package store

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// EmbeddingEntry represents a stored embedding.
type EmbeddingEntry struct {
	ID        int64
	SpecID    string
	Section   string
	Content   string
	Vector    []float32
	Model     string
	UpdatedAt time.Time
}

// EmbeddingUpsert inserts or updates an embedding for a spec section.
func (db *DB) EmbeddingUpsert(specID, section, content string, vector []float32, model string) error {
	blob := encodeVector(vector)
	now := time.Now().Unix()

	_, err := db.conn.Exec(
		`INSERT INTO embeddings (spec_id, section, content, vector, model, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`,
		specID, section, content, blob, model, now,
	)
	if err != nil {
		return fmt.Errorf("embedding upsert %q/%q: %w", specID, section, err)
	}
	return nil
}

// EmbeddingSearch finds the most similar embeddings to the query vector.
// Uses brute-force cosine similarity (sufficient for hundreds of specs).
func (db *DB) EmbeddingSearch(queryVector []float32, limit int) ([]EmbeddingEntry, error) {
	rows, err := db.conn.Query(
		"SELECT id, spec_id, section, content, vector, model, updated_at FROM embeddings",
	)
	if err != nil {
		return nil, fmt.Errorf("embedding search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type scored struct {
		entry EmbeddingEntry
		score float64
	}
	var results []scored

	for rows.Next() {
		var e EmbeddingEntry
		var blob []byte
		var updatedAt int64
		if err := rows.Scan(&e.ID, &e.SpecID, &e.Section, &e.Content, &blob, &e.Model, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning embedding: %w", err)
		}
		e.Vector = decodeVector(blob)
		e.UpdatedAt = time.Unix(updatedAt, 0)

		sim := cosineSimilarity(queryVector, e.Vector)
		results = append(results, scored{entry: e, score: sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by similarity descending (simple selection for small N)
	for i := 0; i < len(results) && i < limit; i++ {
		maxIdx := i
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[maxIdx].score {
				maxIdx = j
			}
		}
		results[i], results[maxIdx] = results[maxIdx], results[i]
	}

	n := limit
	if n > len(results) {
		n = len(results)
	}
	entries := make([]EmbeddingEntry, n)
	for i := 0; i < n; i++ {
		entries[i] = results[i].entry
	}
	return entries, nil
}

// EmbeddingDeleteSpec removes all embeddings for a spec.
func (db *DB) EmbeddingDeleteSpec(specID string) error {
	_, err := db.conn.Exec("DELETE FROM embeddings WHERE spec_id = ?", specID)
	return err
}

func encodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
