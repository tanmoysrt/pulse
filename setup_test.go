package main

import (
	"reflect"
	"testing"
)

func TestExposeStepChoosingAcmeInsertsDomainStep(t *testing.T) {
	m := wizModel{
		steps:  []string{"expose", "password", "notify"},
		cursor: 2, // "Let's Encrypt"
		input:  newWizardInput(),
	}

	next, _ := m.commit()
	m = next.(wizModel)

	want := []string{"expose", "domain", "password", "notify"}
	if !reflect.DeepEqual(m.steps, want) {
		t.Fatalf("steps = %v, want %v", m.steps, want)
	}
	if !m.o.acme {
		t.Fatal("o.acme = false, want true")
	}
	if got := m.step(); got != "domain" {
		t.Fatalf("step = %q, want domain", got)
	}
	if !m.input.Focused() {
		t.Fatal("domain input is not focused")
	}
}

func TestExposeStepChoosingLanSkipsDomainStep(t *testing.T) {
	m := wizModel{
		steps:  []string{"expose", "password"},
		cursor: 0, // "Local network"
		input:  newWizardInput(),
	}

	next, _ := m.commit()
	m = next.(wizModel)

	want := []string{"expose", "password"}
	if !reflect.DeepEqual(m.steps, want) {
		t.Fatalf("steps = %v, want %v", m.steps, want)
	}
	if m.o.acme {
		t.Fatal("o.acme = true, want false")
	}
}

func TestDomainStepPrefillsFromSavedSetup(t *testing.T) {
	m := wizModel{
		steps: []string{"domain"},
		input: newWizardInput(),
		saved: &setupRecord{Domain: "example.com"},
	}
	m.focusStep()

	if got := m.input.Value(); got != "example.com" {
		t.Fatalf("prefilled domain = %q, want example.com", got)
	}
}

func TestParseArgsAcme(t *testing.T) {
	_, _, o := parseArgs([]string{"--acme", "example.com"})
	if !o.acme || !o.acmeSet {
		t.Fatal("expected acme and acmeSet to be true")
	}
	if o.domain != "example.com" {
		t.Fatalf("domain = %q, want example.com", o.domain)
	}
}

func TestDetachArgsAcme(t *testing.T) {
	got := detachArgs(opts{acme: true, domain: "example.com"}, 443)
	want := []string{"--listen-port", "443", "--acme", "example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detachArgs = %v, want %v", got, want)
	}
}

func TestCertNameSanitizesTarget(t *testing.T) {
	cases := map[string]string{
		"example.com": "example.com",
		"203.0.113.5": "203.0.113.5",
		"::1":         "__1",
	}
	for in, want := range cases {
		if got := certName(in); got != want {
			t.Fatalf("certName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenewalWindow(t *testing.T) {
	if renewalWindow(true) >= renewalWindow(false) {
		t.Fatal("IP renewal window should be shorter than domain renewal window")
	}
}

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
