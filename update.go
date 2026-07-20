package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	releaseAPIURL = "https://api.github.com/repos/tanmoysrt/pulse/releases/latest"
	releasesURL   = "https://github.com/tanmoysrt/pulse/releases/latest"
	releaseCacheT = time.Hour
)

// releaseCache remembers the last GitHub check so opening Settings repeatedly
// doesn't hit the API every time.
var releaseCache struct {
	mu      sync.Mutex
	latest  string
	checked time.Time
}

// latestRelease returns the newest release tag (without a leading "v"), or
// "" if it can't be determined.
func latestRelease() string {
	releaseCache.mu.Lock()
	if time.Since(releaseCache.checked) < releaseCacheT {
		v := releaseCache.latest
		releaseCache.mu.Unlock()
		return v
	}
	releaseCache.mu.Unlock()

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, releaseAPIURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return ""
	}
	latest := strings.TrimPrefix(body.TagName, "v")

	releaseCache.mu.Lock()
	releaseCache.latest = latest
	releaseCache.checked = time.Now()
	releaseCache.mu.Unlock()
	return latest
}

// apiVersion reports the running version alongside the newest GitHub release
// so the UI can point out when `pulse update` has something to fetch.
func (d *Daemon) apiVersion(c echo.Context) error {
	latest := latestRelease()
	available := version != "dev" && latest != "" && latest != version
	return c.JSON(http.StatusOK, map[string]any{
		"current":   version,
		"latest":    latest,
		"available": available,
		"url":       releasesURL,
	})
}
