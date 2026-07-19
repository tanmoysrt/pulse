package main

import "testing"

func TestRedoSavedSetupInitializesPasswordInput(t *testing.T) {
	m := wizModel{
		steps:  []string{"saved"},
		cursor: 1,
		input:  newWizardInput(),
	}

	next, _ := m.commit()
	m = next.(wizModel)
	next, _ = m.commit()
	m = next.(wizModel)

	if got := m.step(); got != "password" {
		t.Fatalf("redo setup step = %q, want password", got)
	}
	if !m.input.Focused() {
		t.Fatal("password input is not focused")
	}
}
