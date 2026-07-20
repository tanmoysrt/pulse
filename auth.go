package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

const authCookie = "pulse_token"

// The browser cookie is a JWT signed with the daemon's token, not the token
// itself, refreshed once past the halfway point of its life.
const (
	sessionTTL           = 30 * 24 * time.Hour
	sessionRefreshWindow = sessionTTL / 2
)

// Paths served without auth: the SPA shell (so it can render the login view),
// its static companions, and the login/logout endpoints.
var publicPaths = map[string]bool{
	"/": true, "/sw.js": true, "/manifest.webmanifest": true, "/api/login": true, "/api/logout": true,
	"/icons/icon-192.png": true, "/icons/icon-512.png": true, "/icons/apple-touch-icon.png": true,
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

// withToken appends the auth token to a base URL as a query param so the first
// page load can hand it to the browser (which then keeps it in a cookie).
func withToken(base, token string) string {
	if token == "" {
		return base
	}
	return base + "/?t=" + token
}

// signSession issues a JWT bound to secret, valid for sessionTTL.
func signSession(secret string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(sessionTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// verifySession reports whether tok is valid for secret, and its remaining life.
func verifySession(secret, tok string) (time.Duration, bool) {
	parsed, err := jwt.ParseWithClaims(tok, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		return 0, false
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.ExpiresAt == nil {
		return 0, false
	}
	return time.Until(claims.ExpiresAt.Time), true
}

// authMiddleware accepts the hook token, the current bootstrap token, or a
// signed session cookie. Public paths pass through so the login page can load.
func authMiddleware(d *Daemon) echo.MiddlewareFunc {
	hookToken := []byte(d.token)
	okHook := func(got string) bool {
		return subtle.ConstantTimeCompare([]byte(got), hookToken) == 1
	}
	issue := func(c echo.Context) {
		if tok, err := signSession(d.token); err == nil {
			setAuthCookie(c, tok)
		}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Honor ?t= first (even on public paths) so a bootstrap link works on load.
			if q := c.QueryParam("t"); q != "" && (okHook(q) || d.consumeBootstrap(q)) {
				issue(c)
				return next(c)
			}
			if publicPaths[c.Path()] {
				return next(c)
			}
			if ck, err := c.Cookie(authCookie); err == nil {
				if remaining, ok := verifySession(d.token, ck.Value); ok {
					if remaining < sessionRefreshWindow {
						issue(c)
					}
					return next(c)
				}
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
	if d.passwordHash == "" || bcrypt.CompareHashAndPassword([]byte(d.passwordHash), []byte(in.Password)) != nil {
		d.logins.fail(ip)
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wrong password"})
	}
	d.logins.reset(ip)
	tok, err := signSession(d.token)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create session"})
	}
	setAuthCookie(c, tok)
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
