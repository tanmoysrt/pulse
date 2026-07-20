package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// writeFakeCert drops a self-signed cert/key pair at target's cert path so
// registeredDomains() (which reads the SAN back out of the leaf, no separate
// metadata file) picks it up, without needing a real Let's Encrypt issuance.
func writeFakeCert(t *testing.T, target string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	if ip := net.ParseIP(target); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{target}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := certPaths(target)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

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

func TestDomainStepPrefillsCursorFromSavedSetup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeFakeCert(t, "example.com", time.Now().Add(60*24*time.Hour))
	writeFakeCert(t, "other.example.com", time.Now().Add(60*24*time.Hour))

	m := wizModel{
		steps: []string{"domain"},
		input: newWizardInput(),
		saved: &setupRecord{Domain: "other.example.com"},
	}
	m.focusStep()

	doms := registeredDomains()
	if doms[m.cursor].Target != "other.example.com" {
		t.Fatalf("cursor landed on %q, want other.example.com", doms[m.cursor].Target)
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

func TestDomainStepRequiresARegisteredDomain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m := wizModel{steps: []string{"domain"}, input: newWizardInput()}
	next, _ := m.commit()
	m = next.(wizModel)

	if m.error == "" {
		t.Fatal("expected an error when no domain is registered")
	}
	if m.step() != "domain" {
		t.Fatal("should not advance past domain with nothing registered")
	}

	writeFakeCert(t, "example.com", time.Now().Add(60*24*time.Hour))

	next, _ = m.commit()
	m = next.(wizModel)
	if m.error != "" {
		t.Fatalf("unexpected error once a domain is registered: %v", m.error)
	}
	if m.o.domain != "example.com" {
		t.Fatalf("o.domain = %q, want example.com", m.o.domain)
	}
}

func TestRegisteredDomainsReportsExpiry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeFakeCert(t, "fresh.example.com", time.Now().Add(60*24*time.Hour))
	writeFakeCert(t, "stale.example.com", time.Now().Add(-24*time.Hour))

	doms := registeredDomains()
	if len(doms) != 2 {
		t.Fatalf("len(doms) = %d, want 2", len(doms))
	}
	byTarget := map[string]registeredDomain{}
	for _, d := range doms {
		byTarget[d.Target] = d
	}
	if byTarget["fresh.example.com"].Expired {
		t.Fatal("fresh.example.com should not be reported expired")
	}
	if !byTarget["stale.example.com"].Expired {
		t.Fatal("stale.example.com should be reported expired")
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
