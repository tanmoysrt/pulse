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
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
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

// registeredDomain is one target "pulse add-domain" has issued a
// certificate for, as offered in the setup wizard's domain picker.
type registeredDomain struct {
	Target   string
	NotAfter time.Time
	Expired  bool
}

// registeredDomains lists every target with a certificate on disk, read back
// from each certificate's own SAN — no separate metadata file needed, since
// issueCertificate always requests exactly one name (domain or IP) per cert.
func registeredDomains() []registeredDomain {
	entries, err := os.ReadDir(acmeDir())
	if err != nil {
		return nil
	}
	var out []registeredDomain
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(acmeDir(), e.Name(), "cert.pem"))
		if err != nil {
			continue
		}
		leaf, err := certcrypto.ParsePEMCertificate(b)
		if err != nil {
			continue
		}
		var target string
		switch {
		case len(leaf.DNSNames) > 0:
			target = leaf.DNSNames[0]
		case len(leaf.IPAddresses) > 0:
			target = leaf.IPAddresses[0].String()
		default:
			continue
		}
		out = append(out, registeredDomain{Target: target, NotAfter: leaf.NotAfter, Expired: time.Now().After(leaf.NotAfter)})
	}
	slices.SortFunc(out, func(a, b registeredDomain) int { return strings.Compare(a.Target, b.Target) })
	return out
}

// loadCert reads target's certificate from disk as-is, expired or not. pulse
// never issues or renews while running (see runDaemon) — that's "pulse
// add-domain"'s job — so at serve time this is the only source of truth.
func loadCert(target string) (*tls.Certificate, error) {
	certPath, keyPath := certPaths(target)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

// loadValidCert reads a previously issued cert for target, if it's not yet
// inside its renewal window — the "still active, don't reissue" case.
func loadValidCert(target string) (*tls.Certificate, bool) {
	pair, err := loadCert(target)
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
	return pair, true
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
		return fmt.Errorf("could not resolve %q, check the domain and its DNS record", target)
	}
	if pubErr != nil {
		return nil
	}
	if !slices.Contains(ips, pub) {
		return fmt.Errorf("%s resolves to %s, not this machine (%s), update its DNS A record", target, strings.Join(ips, ", "), pub)
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

	config := lego.NewConfig(user)
	// Let's Encrypt rejects a CSR with an IP address (or any identifier) in
	// the Common Name; the SAN entry is authoritative, so drop the CN.
	config.Certificate.DisableCommonName = true
	client, err := lego.NewClient(config)
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

// runAddDomain is the explicit, admin-run step that registers a domain or IP
// for Let's Encrypt: it validates the target, issues a certificate (or
// confirms the existing one is still valid, without reissuing), and grants
// pulse's own binary permission to bind privileged ports. The daemon itself
// never does any of this at startup — see runDaemon's acme handling, which
// only ever loads whatever this command last produced, expired or not.
func runAddDomain(target string) {
	ensureRoot()

	if cert, ok := loadValidCert(target); ok {
		leaf, _ := x509.ParseCertificate(cert.Certificate[0])
		fmt.Printf("pulse: %s already has a valid certificate (until %s), not reissuing\n", target, leaf.NotAfter.Format("2006-01-02"))
	} else {
		fmt.Printf("pulse: requesting a certificate for %s from Let's Encrypt…\n", target)
		if err := validateExposeTarget(target); err != nil {
			fmt.Fprintln(os.Stderr, "pulse:", err)
			os.Exit(1)
		}
		if _, err := issueCertificate(target); err != nil {
			fmt.Fprintln(os.Stderr, "pulse: could not obtain certificate:", err)
			os.Exit(1)
		}
		fmt.Printf("pulse: certificate ready for %s\n", target)
	}

	restoreOwnership()
	applyBindCapability()
}

// ensureRoot re-execs pulse under sudo if it isn't already running as root:
// issuing a certificate binds port 80 for the HTTP-01 challenge, and
// applyBindCapability's setcap call needs root to grant a capability.
func ensureRoot() {
	if os.Geteuid() == 0 {
		return
	}
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: this command needs root (it binds port 80), install sudo or run as root")
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not locate own executable:", err)
		os.Exit(1)
	}
	argv := append([]string{"sudo", exe}, os.Args[1:]...)
	if err := syscall.Exec(sudoPath, argv, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "pulse: could not elevate with sudo:", err)
		os.Exit(1)
	}
}

// restoreOwnership hands files written while elevated back to the user who
// ran sudo (sudo sets SUDO_UID/SUDO_GID), so the unprivileged `pulse` daemon
// can read its own certificates without needing root at every startup.
func restoreOwnership() {
	uid, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil {
		return // not invoked via sudo (already root) — nothing to hand back
	}
	gid, err := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err != nil {
		return
	}
	filepath.Walk(acmeDir(), func(path string, _ os.FileInfo, err error) error {
		if err == nil {
			os.Chown(path, uid, gid)
		}
		return nil
	})
}

// applyBindCapability grants pulse's own binary permission to bind ports
// below 1024 without root, in case a future run picks one (e.g. --listen-port
// 443). Re-run "pulse add-domain" (as root) any time the binary is replaced
// — e.g. after an update — since Linux clears file capabilities on any write
// to the file.
func applyBindCapability() {
	if runtime.GOOS != "linux" {
		fmt.Println("pulse: on this OS, binding a port below 1024 needs root, run pulse with sudo if you use one")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if _, err := exec.LookPath("setcap"); err != nil {
		fmt.Println("pulse: install setcap (e.g. `apt install libcap2-bin`), then run: sudo setcap cap_net_bind_service=+ep " + exe)
		return
	}
	if out, err := exec.Command("setcap", "cap_net_bind_service=+ep", exe).CombinedOutput(); err != nil {
		fmt.Println("pulse: run this once so pulse can bind a port below 1024 without root: sudo setcap cap_net_bind_service=+ep " + exe)
		if msg := strings.TrimSpace(string(out)); msg != "" {
			fmt.Println("       (" + msg + ")")
		}
		return
	}
	fmt.Println("pulse: granted " + exe + " permission to bind ports below 1024 without root")
}
