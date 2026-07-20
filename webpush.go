package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

var b64 = base64.RawURLEncoding

// pushSub is a browser PushSubscription as delivered by the JS PushManager.
type pushSub struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// vapidKey is the application server identity: an ECDSA P-256 key whose public
// point is handed to the browser and whose private half signs the VAPID JWT.
type vapidKey struct {
	priv *ecdsa.PrivateKey
	pub  []byte
}

// loadOrCreateVapid keeps one keypair on disk so subscriptions made by a phone
// stay valid across pulse restarts. Returns nil if a key can't be established;
// callers degrade gracefully (web push simply stays off).
func loadOrCreateVapid() *vapidKey {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "pulse", "vapid.key")
	if b, err := os.ReadFile(path); err == nil {
		if der, err := b64.DecodeString(strings.TrimSpace(string(b))); err == nil {
			if priv, err := x509.ParseECPrivateKey(der); err == nil {
				return newVapid(priv)
			}
		}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil
	}
	if der, err := x509.MarshalECPrivateKey(priv); err == nil {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, []byte(b64.EncodeToString(der)), 0o600)
	}
	return newVapid(priv)
}

func newVapid(priv *ecdsa.PrivateKey) *vapidKey {
	ep, err := priv.ECDH()
	if err != nil {
		return nil
	}
	return &vapidKey{priv: priv, pub: ep.PublicKey().Bytes()}
}

// authHeader builds the "vapid t=<jwt>, k=<pubkey>" Authorization value the push
// service requires, with the JWT audience scoped to the endpoint's origin.
func (v *vapidKey) authHeader(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": "mailto:pulse@localhost",
	})
	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.priv, h[:])
	if err != nil {
		return "", err
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	jwt := signing + "." + b64.EncodeToString(sig)
	return "vapid t=" + jwt + ", k=" + b64.EncodeToString(v.pub), nil
}

// encryptPayload wraps plaintext for one subscriber using the aes128gcm content
// encoding (RFC 8188) keyed via ECDH + HKDF against the browser's keys (RFC 8291).
func encryptPayload(sub pushSub, plaintext []byte) ([]byte, error) {
	uaPub, err := b64.DecodeString(sub.Keys.P256dh)
	if err != nil {
		return nil, err
	}
	authSecret, err := b64.DecodeString(sub.Keys.Auth)
	if err != nil {
		return nil, err
	}
	curve := ecdh.P256()
	ua, err := curve.NewPublicKey(uaPub)
	if err != nil {
		return nil, err
	}
	as, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	info := append([]byte("WebPush: info\x00"), uaPub...)
	info = append(info, asPub...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(info), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, append(plaintext, 0x02), nil)

	var buf bytes.Buffer
	buf.Write(salt)
	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, 4096)
	buf.Write(rs)
	buf.WriteByte(byte(len(asPub)))
	buf.Write(asPub)
	buf.Write(ciphertext)
	return buf.Bytes(), nil
}

// sendPush posts one encrypted notification and returns the HTTP status so the
// caller can prune subscriptions the push service has retired (404/410).
func (d *Daemon) sendPush(sub pushSub, payload []byte) int {
	body, err := encryptPayload(sub, payload)
	if err != nil {
		return 0
	}
	auth, err := d.vapid.authHeader(sub.Endpoint)
	if err != nil {
		return 0
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "43200")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// pushAll notifies every subscribed browser. url is the app-relative path
// (e.g. "/s/3") the notification should jump to on click; empty for none.
func (d *Daemon) pushAll(title, body, url string) {
	d.mu.Lock()
	v := d.vapid
	subs := append([]pushSub(nil), d.pushSubs...)
	d.mu.Unlock()
	if v == nil || len(subs) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body, "url": url})
	var dead []string
	for _, sub := range subs {
		if code := d.sendPush(sub, payload); code == http.StatusNotFound || code == http.StatusGone {
			dead = append(dead, sub.Endpoint)
		}
	}
	if len(dead) > 0 {
		d.pruneSubs(dead)
	}
}

func (d *Daemon) addSub(sub pushSub) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, e := range d.pushSubs {
		if e.Endpoint == sub.Endpoint {
			return
		}
	}
	d.pushSubs = append(d.pushSubs, sub)
}

func (d *Daemon) pruneSubs(dead []string) {
	gone := map[string]bool{}
	for _, d := range dead {
		gone[d] = true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	kept := d.pushSubs[:0]
	for _, sub := range d.pushSubs {
		if !gone[sub.Endpoint] {
			kept = append(kept, sub)
		}
	}
	d.pushSubs = kept
}

func (d *Daemon) apiPushKey(c echo.Context) error {
	if d.vapid == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "push unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"key": b64.EncodeToString(d.vapid.pub)})
}

func (d *Daemon) apiPushSubscribe(c echo.Context) error {
	var sub pushSub
	if err := c.Bind(&sub); err != nil || sub.Endpoint == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid subscription"})
	}
	d.addSub(sub)
	return c.NoContent(http.StatusOK)
}
