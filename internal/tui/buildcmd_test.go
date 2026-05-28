package tui

import "testing"

func TestCmdResult(t *testing.T) {
	cmd := cmdResult("test", "SPEC-001", nil)
	if cmd == nil {
		t.Error("cmdResult should return a non-nil command")
	}
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.Action != "test" {
		t.Errorf("action = %q, want 'test'", result.Action)
	}
	if result.SpecID != "SPEC-001" {
		t.Errorf("specID = %q, want SPEC-001", result.SpecID)
	}
}
