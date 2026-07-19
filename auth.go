package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const authCookie = "pulse_token"

// Paths served without auth: the SPA shell (so it can render the login view),
// its static companions, and the login/logout endpoints.
var publicPaths = map[string]bool{
	"/": true, "/sw.js": true, "/manifest.webmanifest": true, "/api/login": true, "/api/logout": true,
}

// randomToken returns a URL-safe random secret used to gate access to the UI.
func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail; if it does, refuse to run with a weak token.
		panic("pulse: unable to generate auth token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// randomPassword returns a short, human-typeable password (no ambiguous chars) for
// the login page when the user doesn't supply their own.
func randomPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 10)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// withToken appends the auth token to a base URL as a query param so the first
// page load can hand it to the browser (which then keeps it in a cookie).
func withToken(base, token string) string {
	if token == "" {
		return base
	}
	return base + "/?t=" + token
}

// authMiddleware requires the token as a ?t= query param (first visit, e.g. from
// the QR) or the cookie it then sets; same-origin fetch/EventSource send the
// cookie for free. Public paths pass through so the login page can load.
func authMiddleware(token string) echo.MiddlewareFunc {
	want := []byte(token)
	ok := func(got string) bool {
		return subtle.ConstantTimeCompare([]byte(got), want) == 1
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Honor ?t= first (even on public paths) so the QR's /?t=<token> sets
			// the cookie on the very first page load.
			if q := c.QueryParam("t"); q != "" && ok(q) {
				setAuthCookie(c, token)
				return next(c)
			}
			if publicPaths[c.Path()] {
				return next(c)
			}
			if ck, err := c.Cookie(authCookie); err == nil && ok(ck.Value) {
				return next(c)
			}
			return c.NoContent(http.StatusUnauthorized)
		}
	}
}

func setAuthCookie(c echo.Context, token string) {
	c.SetCookie(&http.Cookie{
		Name:     authCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// loginLimiter throttles password guesses to maxAttempts per window, per client
// IP, with a fixed window that resets on the first attempt after it lapses.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptWindow
}

type attemptWindow struct {
	count int
	start time.Time
}

const (
	maxAttempts = 5
	loginWindow = 15 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{attempts: map[string]*attemptWindow{}} }

// retryAfter reports how long ip must wait, or 0 if it may attempt now.
func (l *loginLimiter) retryAfter(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.attempts[ip]
	if w == nil || time.Since(w.start) >= loginWindow {
		return 0
	}
	if w.count < maxAttempts {
		return 0
	}
	return loginWindow - time.Since(w.start)
}

// fail records a wrong guess, opening a fresh window if the last one lapsed.
func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.attempts[ip]
	if w == nil || time.Since(w.start) >= loginWindow {
		l.attempts[ip] = &attemptWindow{count: 1, start: time.Now()}
		return
	}
	w.count++
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// apiLogin verifies the password and, on success, sets the same cookie the QR
// token path sets — so everything downstream authenticates identically.
func (d *Daemon) apiLogin(c echo.Context) error {
	ip := c.RealIP()
	if wait := d.logins.retryAfter(ip); wait > 0 {
		return c.JSON(http.StatusTooManyRequests, map[string]any{
			"error": "too many attempts", "retryAfter": int(wait.Seconds()),
		})
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "bad request"})
	}
	if d.password == "" || subtle.ConstantTimeCompare([]byte(in.Password), []byte(d.password)) != 1 {
		d.logins.fail(ip)
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wrong password"})
	}
	d.logins.reset(ip)
	setAuthCookie(c, d.token)
	return c.NoContent(http.StatusOK)
}

// apiLogout clears the auth cookie; the browser then falls back to the login page.
func (d *Daemon) apiLogout(c echo.Context) error {
	c.SetCookie(&http.Cookie{
		Name: authCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	return c.NoContent(http.StatusOK)
}
