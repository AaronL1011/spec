package cmd

import (
	"fmt"
	"testing"
)

type mockSessionDB struct {
	specID string
	err    error
}

func (m *mockSessionDB) SessionMostRecent() (string, error) {
	return m.specID, m.err
}

func TestResolveSpecID_ExplicitFlag(t *testing.T) {
	db := &mockSessionDB{}
	id, err := resolveSpecID("spec-042", db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "SPEC-042" {
		t.Errorf("got %q, want SPEC-042", id)
	}
}

func TestResolveSpecID_FromSession(t *testing.T) {
	db := &mockSessionDB{specID: "SPEC-001"}
	id, err := resolveSpecID("", db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "SPEC-001" {
		t.Errorf("got %q, want SPEC-001", id)
	}
}

func TestResolveSpecID_NoSession_ReturnsError(t *testing.T) {
	db := &mockSessionDB{}
	_, err := resolveSpecID("", db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveSpecID_DBError_ReturnsError(t *testing.T) {
	db := &mockSessionDB{err: fmt.Errorf("db failure")}
	_, err := resolveSpecID("", db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
