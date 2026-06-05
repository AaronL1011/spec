package build

import (
	"encoding/json"
	"testing"
)

func TestNewBuildStrategy_Selection(t *testing.T) {
	cases := []struct {
		in   string
		name string
	}{
		{"", "stacked-draft-pr"},
		{"stacked-draft-pr", "stacked-draft-pr"},
		{"STACKED-DRAFT-PR", "stacked-draft-pr"},
		{"none", "none"},
		{"local", "none"},
		{"  none  ", "none"},
	}
	for _, c := range cases {
		if got := newBuildStrategy(Options{Strategy: c.in}).Name(); got != c.name {
			t.Errorf("newBuildStrategy(%q).Name() = %q, want %q", c.in, got, c.name)
		}
	}
}

func TestStrategy_FinishingTools(t *testing.T) {
	if got := (stackedDraftPRStrategy{}).FinishingTools(); len(got) != 3 {
		t.Errorf("stacked-draft-pr should expose 3 finishing tools, got %v", got)
	}
	if got := (noneStrategy{}).FinishingTools(); len(got) != 0 {
		t.Errorf("none should expose no finishing tools, got %v", got)
	}
	if !exposesFinishingTool(stackedDraftPRStrategy{}, "spec_open_pr") {
		t.Error("stacked-draft-pr should expose spec_open_pr")
	}
	if exposesFinishingTool(noneStrategy{}, "spec_open_pr") {
		t.Error("none must not expose spec_open_pr")
	}
}

func TestNoneStrategy_CompleteOnAllNodes(t *testing.T) {
	session := &SessionState{Steps: []PRStep{{Number: 1}, {Number: 2}}}
	c := (noneStrategy{}).Complete(session, nil)
	if !c.Done {
		t.Errorf("none strategy should be done once all nodes complete: %+v", c)
	}
}

func TestStrategy_GatesFinishingToolCall(t *testing.T) {
	s := &MCPServer{strategy: noneStrategy{}}
	if _, err := s.finishingTool("spec_open_pr", nil, func(json.RawMessage) (*MCPToolResult, error) {
		t.Fatal("dispatcher must not be reached under none strategy")
		return nil, nil
	}); err == nil {
		t.Error("expected an error calling a finishing tool under the none strategy")
	}
}

func TestToolSpecs_GatedByStrategy(t *testing.T) {
	stacked := (&MCPServer{strategy: stackedDraftPRStrategy{}}).ToolSpecs()
	none := (&MCPServer{strategy: noneStrategy{}}).ToolSpecs()
	if len(stacked) != len(none)+3 {
		t.Errorf("stacked should advertise 3 more tools than none: stacked=%d none=%d", len(stacked), len(none))
	}
	for _, spec := range none {
		if spec.Name == "spec_open_pr" {
			t.Error("none strategy must not advertise spec_open_pr")
		}
	}
}
