package build

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/store"
)

func TestInvokeResultSummary(t *testing.T) {
	tests := []struct {
		name string
		in   adapter.InvokeResult
		want []string
	}{
		{
			name: "completed with tokens",
			in:   adapter.InvokeResult{ExitReason: "completed", Tokens: adapter.TokenUsage{Input: 100, Output: 50, Total: 150}},
			want: []string{"completed", "150 tokens", "in 100", "out 50"},
		},
		{
			name: "error with class",
			in:   adapter.InvokeResult{ExitReason: "error", ErrorClass: "auto_retry_exhausted"},
			want: []string{"error", "auto_retry_exhausted"},
		},
		{
			name: "unknown reason",
			in:   adapter.InvokeResult{},
			want: []string{"unknown"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := invokeResultSummary(&tt.in)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("summary %q missing %q", got, w)
				}
			}
		})
	}
}

func TestLogInvokeResult_WritesBuildResultEvent(t *testing.T) {
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	SetActivityDB(db)
	t.Cleanup(func() { SetActivityDB(nil) })

	result := &adapter.InvokeResult{
		ExitReason: "error",
		ErrorClass: "auto_retry_exhausted",
		Tokens:     adapter.TokenUsage{Input: 200, Output: 80, Total: 280},
	}
	logInvokeResult("SPEC-001", result)

	entries, err := db.ActivityForSpec("SPEC-001", 10)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.ActivityEntry
	for i := range entries {
		if entries[i].EventType == "build_result" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no build_result activity event recorded")
	}
	if !strings.Contains(found.Summary, "auto_retry_exhausted") {
		t.Errorf("summary = %q, want the error class", found.Summary)
	}

	// Metadata must be the structured result, debuggable after the fact.
	var decoded adapter.InvokeResult
	if err := json.Unmarshal([]byte(found.Metadata), &decoded); err != nil {
		t.Fatalf("metadata is not valid InvokeResult JSON: %v\n%s", err, found.Metadata)
	}
	if decoded.Tokens.Total != 280 || decoded.ErrorClass != "auto_retry_exhausted" {
		t.Errorf("decoded metadata = %+v, want token/error detail preserved", decoded)
	}
}

func TestLogInvokeResult_NilResultIsNoop(t *testing.T) {
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	SetActivityDB(db)
	t.Cleanup(func() { SetActivityDB(nil) })

	logInvokeResult("SPEC-002", nil)

	entries, err := db.ActivityForSpec("SPEC-002", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no activity for a nil result, got %d", len(entries))
	}
}
