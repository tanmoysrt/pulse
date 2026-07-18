package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo/v4"
)

const authCookie = "pulse_token"

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

// authMiddleware requires the token as a ?t= query param (first visit) or the
// cookie it then sets; same-origin fetch/EventSource send the cookie for free.
func authMiddleware(token string) echo.MiddlewareFunc {
	want := []byte(token)
	ok := func(got string) bool {
		return subtle.ConstantTimeCompare([]byte(got), want) == 1
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if q := c.QueryParam("t"); q != "" && ok(q) {
				c.SetCookie(&http.Cookie{
					Name:     authCookie,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				return next(c)
			}
			if ck, err := c.Cookie(authCookie); err == nil && ok(ck.Value) {
				return next(c)
			}
			return c.NoContent(http.StatusUnauthorized)
		}
	}
}
