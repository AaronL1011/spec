package tui

import "testing"

func TestIntakeForm_Open(t *testing.T) {
	var f intakeFormState
	f.open()

	if !f.active {
		t.Error("form should be active after open")
	}
	if f.field != intakeFieldTitle {
		t.Errorf("field = %d, want title field", f.field)
	}
	if f.priority != "medium" {
		t.Errorf("default priority = %q, want medium", f.priority)
	}
}

func TestIntakeForm_FieldNavigation(t *testing.T) {
	var f intakeFormState
	f.open()

	f.nextField()
	if f.field != intakeFieldPriority {
		t.Errorf("field = %d, want priority", f.field)
	}

	f.nextField()
	if f.field != intakeFieldSource {
		t.Errorf("field = %d, want source", f.field)
	}

	// Past end — stays.
	f.nextField()
	if f.field != intakeFieldSource {
		t.Errorf("field = %d, should stay at source", f.field)
	}

	f.prevField()
	if f.field != intakeFieldPriority {
		t.Errorf("field = %d, want priority after prev", f.field)
	}
}

func TestIntakeForm_CyclePriority(t *testing.T) {
	var f intakeFormState
	f.open()

	f.cyclePriority() // medium → high
	if f.priority != "high" {
		t.Errorf("priority = %q, want high", f.priority)
	}

	f.cyclePriority() // high → critical
	f.cyclePriority() // critical → low
	f.cyclePriority() // low → medium
	if f.priority != "medium" {
		t.Errorf("priority = %q after full cycle, want medium", f.priority)
	}
}

func TestIntakeForm_TextInput(t *testing.T) {
	var f intakeFormState
	f.open()

	f.appendToField("Bug")
	f.appendToField(" report")
	if f.title != "Bug report" {
		t.Errorf("title = %q, want 'Bug report'", f.title)
	}

	f.backspaceField()
	if f.title != "Bug repor" {
		t.Errorf("title after backspace = %q, want 'Bug repor'", f.title)
	}

	// Source field.
	f.field = intakeFieldSource
	f.appendToField("slack")
	if f.source != "slack" {
		t.Errorf("source = %q, want 'slack'", f.source)
	}
}

func TestIntakeForm_Validation(t *testing.T) {
	var f intakeFormState
	f.open()

	if f.valid() {
		t.Error("empty title should not be valid")
	}

	f.title = "Something"
	if !f.valid() {
		t.Error("non-empty title should be valid")
	}
}

func TestIntakeForm_Close(t *testing.T) {
	var f intakeFormState
	f.open()
	f.title = "test"
	f.close()

	if f.active {
		t.Error("form should not be active after close")
	}
}
