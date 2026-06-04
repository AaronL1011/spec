package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllLeavesHaveDraftPR(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty plan", "", false},
		{
			name:    "leaf missing PR",
			content: "1. [svc] root\n2. [svc] leaf (after: 1)\n",
			want:    false,
		},
		{
			name:    "single node without PR",
			content: "1. [svc] only\n",
			want:    false,
		},
		{
			name:    "leaf has PR (root need not)",
			content: "1. [svc] root\n2. [svc] leaf (after: 1) <!-- pr: https://gh/pull/2 -->\n",
			want:    true,
		},
		{
			name: "diamond — both leaves covered",
			content: "1. [svc] root\n2. [svc] left (after: 1)\n3. [svc] right (after: 1)\n" +
				"4. [svc] merge (after: 2,3) <!-- pr: https://gh/pull/4 -->\n",
			want: true,
		},
		{
			name:    "two independent leaves — one missing",
			content: "1. [svc] a <!-- pr: https://gh/pull/1 -->\n2. [svc] b\n",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllLeavesHaveDraftPR(tt.content); got != tt.want {
				t.Errorf("AllLeavesHaveDraftPR = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordPRInSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "SPEC-1.md")
	content := `# SPEC-1

### 7.3 PR Stack Plan
1. [svc] root
2. [svc] leaf (after: 1)
`
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := recordPRInSpec(specPath, 2, "https://github.com/o/svc/pull/9"); err != nil {
		t.Fatalf("recordPRInSpec: %v", err)
	}

	data, _ := os.ReadFile(specPath)
	steps, err := ParsePRStack(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if steps[1].PRURL != "https://github.com/o/svc/pull/9" {
		t.Errorf("node 2 PRURL = %q, want recorded URL", steps[1].PRURL)
	}
	// Description must remain clean (annotation stripped on parse).
	if steps[1].Description != "leaf" {
		t.Errorf("node 2 description = %q, want %q", steps[1].Description, "leaf")
	}
	// Now the gate verifier passes (the only leaf has a PR).
	if !AllLeavesHaveDraftPR(string(data)) {
		t.Error("expected leaf coverage after recording the PR")
	}

	// Idempotent: recording again replaces, not appends.
	if err := recordPRInSpec(specPath, 2, "https://github.com/o/svc/pull/10"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(specPath)
	steps, _ = ParsePRStack(string(data))
	if steps[1].PRURL != "https://github.com/o/svc/pull/10" {
		t.Errorf("re-record should replace URL, got %q", steps[1].PRURL)
	}
}
