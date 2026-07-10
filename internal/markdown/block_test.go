package markdown

import "testing"

func TestBlockRanges_UsesSemanticAnchorUnits(t *testing.T) {
	source := "Paragraph one\ncontinues.\n\n- first item\n- second item\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n```go\nfmt.Println(1)\n```\n"
	blocks := BlockRanges(source)
	want := []BlockRange{
		{StartLine: 0, EndLine: 2},
		{StartLine: 3, EndLine: 4},
		{StartLine: 4, EndLine: 5},
		{StartLine: 6, EndLine: 7},
		{StartLine: 8, EndLine: 9},
		// Fence content only — the ``` markers never render, so anchoring
		// includes just the code line.
		{StartLine: 11, EndLine: 12},
	}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %+v, want %+v", blocks, want)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, blocks[i], want[i])
		}
	}
}
