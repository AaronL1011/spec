package build

import (
	"strings"
	"testing"
)

// step is a terse constructor for graph tests.
func step(num int, deps ...int) PRStep {
	return PRStep{Number: num, Description: "s", Status: "pending", DependsOn: deps}
}

func waveIDs(waves [][]PRStep) [][]string {
	out := make([][]string, len(waves))
	for i, w := range waves {
		for _, n := range w {
			out[i] = append(out[i], n.NodeID())
		}
	}
	return out
}

func TestBuildGraph_Waves(t *testing.T) {
	tests := []struct {
		name  string
		steps []PRStep
		want  [][]string
	}{
		{
			name:  "linear",
			steps: []PRStep{step(1), step(2, 1), step(3, 2)},
			want:  [][]string{{"n1"}, {"n2"}, {"n3"}},
		},
		{
			name:  "diamond",
			steps: []PRStep{step(1), step(2, 1), step(3, 1), step(4, 2, 3)},
			want:  [][]string{{"n1"}, {"n2", "n3"}, {"n4"}},
		},
		{
			name:  "multi-root",
			steps: []PRStep{step(1), step(2), step(3, 1, 2)},
			want:  [][]string{{"n1", "n2"}, {"n3"}},
		},
		{
			name:  "no edges single wave",
			steps: []PRStep{step(1), step(2), step(3)},
			want:  [][]string{{"n1", "n2", "n3"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := BuildGraph(tt.steps)
			if err != nil {
				t.Fatalf("BuildGraph: %v", err)
			}
			got := waveIDs(g.Waves())
			if len(got) != len(tt.want) {
				t.Fatalf("waves = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if strings.Join(got[i], ",") != strings.Join(tt.want[i], ",") {
					t.Errorf("wave %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildGraph_Cycle(t *testing.T) {
	// n1 → n2 → n3 → n1
	steps := []PRStep{step(1, 3), step(2, 1), step(3, 2)}
	_, err := BuildGraph(steps)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should name the cycle: %v", err)
	}
	// Actionable: names the offending nodes.
	for _, id := range []string{"n1", "n2", "n3"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("cycle error should mention %s: %v", id, err)
		}
	}
}

func TestBuildGraph_UnknownRef(t *testing.T) {
	steps := []PRStep{step(1), step(2, 99)}
	_, err := BuildGraph(steps)
	if err == nil {
		t.Fatal("expected unknown-ref error")
	}
	if !strings.Contains(err.Error(), "unknown step 99") {
		t.Errorf("error should name the unknown ref: %v", err)
	}
}

func TestBuildGraph_SelfEdge(t *testing.T) {
	steps := []PRStep{step(1, 1)}
	_, err := BuildGraph(steps)
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("expected self-edge error, got %v", err)
	}
}

func TestGraph_ReadySet(t *testing.T) {
	// Diamond: n1 → {n2, n3} → n4
	g, err := BuildGraph([]PRStep{step(1), step(2, 1), step(3, 1), step(4, 2, 3)})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		done map[string]bool
		want []string
	}{
		{"nothing done", map[string]bool{}, []string{"n1"}},
		{"root done opens wave", map[string]bool{"n1": true}, []string{"n2", "n3"}},
		{"one mid done", map[string]bool{"n1": true, "n2": true}, []string{"n3"}},
		{"both mid done opens leaf", map[string]bool{"n1": true, "n2": true, "n3": true}, []string{"n4"}},
		{"all done", map[string]bool{"n1": true, "n2": true, "n3": true, "n4": true}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, n := range g.ReadySet(tt.done) {
				got = append(got, n.NodeID())
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("ReadySet = %v, want %v", got, tt.want)
			}
		})
	}
}
