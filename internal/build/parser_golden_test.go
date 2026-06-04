package build

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden regenerates the .golden.json fixtures: go test -run Golden -update.
var updateGolden = flag.Bool("update", false, "update golden files")

// TestParsePRStack_Golden parses each testdata/parser/*.md fixture and compares
// the parsed []PRStep against its .golden.json. Covers the flat list, the
// layered+after form, the prose form, and a mixed plan.
func TestParsePRStack_Golden(t *testing.T) {
	cases := []string{"flat_list", "layered", "prose", "mixed"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			input := filepath.Join("testdata", "parser", name+".md")
			golden := filepath.Join("testdata", "parser", name+".golden.json")

			data, err := os.ReadFile(input)
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			steps, err := ParsePRStack(string(data))
			if err != nil {
				t.Fatalf("ParsePRStack: %v", err)
			}

			got, err := json.MarshalIndent(steps, "", "  ")
			if err != nil {
				t.Fatalf("marshalling steps: %v", err)
			}
			got = append(got, '\n')

			if *updateGolden {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading golden (run with -update to create): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("parsed steps differ from golden %s\n--- got ---\n%s\n--- want ---\n%s",
					golden, got, want)
			}
		})
	}
}

// TestParsePRStack_NoEdgesEmptyDependsOn asserts the explicit acceptance rule:
// a plan with no (after: ...) annotations yields empty DependsOn on every node.
func TestParsePRStack_NoEdgesEmptyDependsOn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "parser", "flat_list.md"))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParsePRStack(string(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if len(s.DependsOn) != 0 {
			t.Errorf("step %d: DependsOn = %v, want empty", s.Number, s.DependsOn)
		}
		if s.ID == "" {
			t.Errorf("step %d: ID should be assigned", s.Number)
		}
	}
}
