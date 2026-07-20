package main

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

func acmeDir() string { return filepath.Join(filepath.Dir(statePath()), "certs") }

func certName(target string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "*", "_")
	return r.Replace(target)
}

func certPaths(target string) (certPath, keyPath string) {
	dir := filepath.Join(acmeDir(), certName(target))
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// renewalWindow is how far ahead of expiry to renew: generous for normal
// ~90-day certs, tight for Let's Encrypt's ~6-day short-lived IP certs.
func renewalWindow(isIP bool) time.Duration {
	if isIP {
		return 48 * time.Hour
	}
	return 30 * 24 * time.Hour
}

// loadValidCert reads a previously issued cert for target, if it's not yet
// inside its renewal window — the "still active, don't reissue" case.
func loadValidCert(target string) (*tls.Certificate, bool) {
	certPath, keyPath := certPaths(target)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, false
	}
	if time.Now().Add(renewalWindow(net.ParseIP(target) != nil)).After(leaf.NotAfter) {
		return nil, false
	}
	pair.Leaf = leaf
	return &pair, true
}

// ensureCertificate returns a valid certificate for target, reusing one
// already on disk when it isn't close to expiry, else validating that
// target actually points here and issuing a fresh one through Let's Encrypt.
func ensureCertificate(target string) (*tls.Certificate, error) {
	if cert, ok := loadValidCert(target); ok {
		return cert, nil
	}
	if err := validateExposeTarget(target); err != nil {
		return nil, err
	}
	return issueCertificate(target)
}

// validateExposeTarget confirms target (a domain or IP) actually resolves to
// this machine before we ask Let's Encrypt for a certificate — a typo'd
// domain or a stale/missing DNS record would otherwise fail validation on
// Let's Encrypt's side with a far more confusing error.
func validateExposeTarget(target string) error {
	pub, pubErr := publicIP()
	if ip := net.ParseIP(target); ip != nil {
		if pubErr != nil {
			return nil // can't verify right now; let the ACME challenge be the judge
		}
		if ip.String() != pub {
			return fmt.Errorf("this machine's public IP looks like %s, not %s", pub, target)
		}
		return nil
	}
	ips, err := net.LookupHost(target)
	if err != nil {
		return fmt.Errorf("could not resolve %q — check the domain and its DNS record", target)
	}
	if pubErr != nil {
		return nil
	}
	if !slices.Contains(ips, pub) {
		return fmt.Errorf("%s resolves to %s, not this machine (%s) — update its DNS A record", target, strings.Join(ips, ", "), pub)
	}
	return nil
}

// publicIP asks an external service what address this machine is reachable
// at — the local interface list isn't enough on VPS providers whose public
// IP is NAT'd rather than bound to any NIC.
func publicIP() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("unexpected response from ipify")
	}
	return ip, nil
}

// acmeUser implements lego's registration.User against a key persisted on
// disk, so the same ACME account is reused across issuances and renewals.
type acmeUser struct {
	reg *registration.Resource
	key crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return "" }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func accountKeyPath() string { return filepath.Join(acmeDir(), "account.key") }
func accountRegPath() string { return filepath.Join(acmeDir(), "account.json") }

func loadOrCreateAccount() (*acmeUser, error) {
	if b, err := os.ReadFile(accountKeyPath()); err == nil {
		key, err := certcrypto.ParsePEMPrivateKey(b)
		if err != nil {
			return nil, fmt.Errorf("read acme account key: %w", err)
		}
		user := &acmeUser{key: key}
		if rb, err := os.ReadFile(accountRegPath()); err == nil {
			var reg registration.Resource
			if json.Unmarshal(rb, &reg) == nil {
				user.reg = &reg
			}
		}
		return user, nil
	}
	key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
	if err != nil {
		return nil, fmt.Errorf("generate acme account key: %w", err)
	}
	if err := os.MkdirAll(acmeDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(accountKeyPath(), certcrypto.PEMEncode(key), 0o600); err != nil {
		return nil, err
	}
	return &acmeUser{key: key}, nil
}

func saveAccountReg(reg *registration.Resource) {
	b, _ := json.Marshal(reg)
	os.WriteFile(accountRegPath(), b, 0o600)
}

// issueCertificate requests a new certificate from Let's Encrypt, proving
// control of target with a standalone HTTP-01 challenge server on :80. IP
// targets go out under Let's Encrypt's short-lived profile — the only kind
// it issues for bare IP addresses.
func issueCertificate(target string) (*tls.Certificate, error) {
	user, err := loadOrCreateAccount()
	if err != nil {
		return nil, err
	}

	client, err := lego.NewClient(lego.NewConfig(user))
	if err != nil {
		return nil, fmt.Errorf("acme client: %w", err)
	}
	if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80")); err != nil {
		return nil, fmt.Errorf("acme http challenge: %w", err)
	}

	if user.reg == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("acme registration: %w", err)
		}
		user.reg = reg
		saveAccountReg(reg)
	}

	req := certificate.ObtainRequest{Domains: []string{target}, Bundle: true}
	if net.ParseIP(target) != nil {
		req.Profile = "shortlived"
	}
	res, err := client.Certificate.Obtain(req)
	if err != nil {
		return nil, fmt.Errorf("could not obtain certificate: %w", err)
	}

	certPath, keyPath := certPaths(target)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, res.Certificate, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, res.PrivateKey, 0o600); err != nil {
		return nil, err
	}

	pair, err := tls.X509KeyPair(res.Certificate, res.PrivateKey)
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

// certStore lets the TLS listener pick up a renewed certificate without a
// restart: renewCertLoop swaps it in, GetCertificate reads it per handshake.
type certStore struct {
	mu   sync.RWMutex
	cert *tls.Certificate
}

func newCertStore(cert *tls.Certificate) *certStore { return &certStore{cert: cert} }

func (s *certStore) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cert, nil
}

func (s *certStore) set(cert *tls.Certificate) {
	s.mu.Lock()
	s.cert = cert
	s.mu.Unlock()
}

// renewCertLoop periodically re-runs ensureCertificate for the life of the
// daemon, swapping the live certificate in on renewal (a no-op most days,
// since ensureCertificate reuses a cert until it nears expiry).
func renewCertLoop(d *Daemon) {
	for {
		time.Sleep(12 * time.Hour)
		cert, err := ensureCertificate(d.acmeDomain)
		if err != nil {
			fmt.Println("pulse: certificate renewal failed:", err)
			continue
		}
		d.certStore.set(cert)
	}
}
