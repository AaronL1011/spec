package store

import "testing"

func TestReaderPosition_RoundTrip(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.ReaderPositionSet("SPEC-1", "technical_implementation", 42); err != nil {
		t.Fatalf("ReaderPositionSet: %v", err)
	}
	section, offset, err := db.ReaderPositionGet("SPEC-1")
	if err != nil {
		t.Fatalf("ReaderPositionGet: %v", err)
	}
	if section != "technical_implementation" || offset != 42 {
		t.Errorf("position = (%q,%d), want (technical_implementation,42)", section, offset)
	}
}
