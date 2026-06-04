package build

import (
	"testing"

	"github.com/aaronl1011/spec/internal/store"
)

// TestSession_KillAndResume verifies the resume primitive: with 2 of 4 diamond
// nodes complete, a reloaded session yields exactly the remaining ready nodes,
// and a completed node is never re-dispatched. State round-trips via :memory:.
func TestSession_KillAndResume(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Diamond: n1 → {n2, n3} → n4
	steps := []PRStep{
		{Number: 1, Description: "root"},
		{Number: 2, Description: "left", DependsOn: []int{1}},
		{Number: 3, Description: "right", DependsOn: []int{1}},
		{Number: 4, Description: "merge", DependsOn: []int{2, 3}},
	}

	session, err := CreateSession(db, "SPEC-700", steps, "/tmp")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Complete the root and the left branch.
	session.MarkNodeComplete("n1")
	session.MarkNodeComplete("n2")
	if err := SaveSession(db, session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Simulate a kill: reload from the store.
	reloaded, err := LoadSession(db, "SPEC-700")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if reloaded == nil {
		t.Fatal("reloaded session is nil")
	}

	// Ledger survived the round-trip.
	if reloaded.NodeStatus("n1") != NodeComplete || reloaded.NodeStatus("n2") != NodeComplete {
		t.Fatalf("completed nodes not persisted: %+v", reloaded.Nodes)
	}

	g, err := BuildGraph(reloaded.Steps)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	ready := g.ReadySet(reloaded.DoneSet())
	if len(ready) != 1 || ready[0].NodeID() != "n3" {
		t.Fatalf("ready set = %v, want only n3 (n4 still blocked, n1/n2 done)", idsOf(ready))
	}

	if reloaded.NodesComplete() {
		t.Error("session should not be complete with n3,n4 outstanding")
	}

	// Finish the survivors; n4 only becomes ready once n3 is done.
	reloaded.MarkNodeComplete("n3")
	ready = g.ReadySet(reloaded.DoneSet())
	if len(ready) != 1 || ready[0].NodeID() != "n4" {
		t.Fatalf("after n3, ready = %v, want n4", idsOf(ready))
	}
	reloaded.MarkNodeComplete("n4")
	if !reloaded.NodesComplete() {
		t.Error("session should be complete after all nodes done")
	}
}

func TestSession_MarkNodeFailed(t *testing.T) {
	s := &SessionState{SpecID: "SPEC-1", Steps: []PRStep{{Number: 1}}}
	s.InitNodes()
	s.MarkNodeFailed("n1", "tests failed")
	if s.NodeStatus("n1") != NodeFailed {
		t.Errorf("status = %q, want failed", s.NodeStatus("n1"))
	}
	if got := s.FailedNodes(); len(got) != 1 || got[0] != "n1" {
		t.Errorf("FailedNodes = %v, want [n1]", got)
	}
	// Completing clears the failure reason (recovery path).
	s.MarkNodeComplete("n1")
	if len(s.FailedNodes()) != 0 || s.Nodes["n1"].Reason != "" {
		t.Errorf("completing should clear failure: %+v", s.Nodes["n1"])
	}
}

func idsOf(steps []PRStep) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.NodeID())
	}
	return out
}
