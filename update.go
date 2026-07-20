package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	githubRepo    = "tanmoysrt/pulse"
	releaseAPIURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
	releasesURL   = "https://github.com/" + githubRepo + "/releases/latest"
	releaseCacheT = time.Hour
)

// releaseCache remembers the last GitHub check so opening Settings repeatedly
// doesn't hit the API every time.
var releaseCache struct {
	mu      sync.Mutex
	latest  string
	checked time.Time
}

// normalizeVersion strips the "v" release tags use so it can be compared
// against the compiled-in version, which keeps whatever prefix the release
// workflow was given (currently "vX.Y.Z").
func normalizeVersion(s string) string { return strings.TrimPrefix(s, "v") }

// latestRelease returns the newest release tag (without a leading "v"), or
// "" if it can't be determined. force bypasses the hourly cache for an
// explicit, user-triggered check.
func latestRelease(force bool) string {
	releaseCache.mu.Lock()
	if !force && time.Since(releaseCache.checked) < releaseCacheT {
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
	latest := normalizeVersion(body.TagName)

	releaseCache.mu.Lock()
	releaseCache.latest = latest
	releaseCache.checked = time.Now()
	releaseCache.mu.Unlock()
	return latest
}

// apiVersion reports the running version alongside the newest GitHub release
// so the UI can point out when an update is available. ?fresh=1 bypasses the
// cache for an explicit check (e.g. the Settings "Check for updates" click).
func (d *Daemon) apiVersion(c echo.Context) error {
	latest := latestRelease(c.QueryParam("fresh") != "")
	available := version != "dev" && latest != "" && latest != normalizeVersion(version)
	return c.JSON(http.StatusOK, map[string]any{
		"current":   version,
		"latest":    latest,
		"available": available,
		"url":       releasesURL,
		"tunnel":    d.tunnel,
	})
}

// releaseAsset names this platform's release binary, matching install.sh's
// "pulse-<os>-<arch>" convention. ok is false on a platform pulse isn't
// built for (see Makefile's PLATFORMS).
func releaseAsset() (name string, ok bool) {
	switch runtime.GOOS {
	case "linux", "darwin":
	default:
		return "", false
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		return "", false
	}
	return fmt.Sprintf("pulse-%s-%s", runtime.GOOS, runtime.GOARCH), true
}

// minReleaseSize guards against treating a truncated download or an error
// page (GitHub redirects a missing asset to an HTML 404) as a real binary.
const minReleaseSize = 1 << 20 // 1MB; real release binaries run ~8-10MB

// replaceBinary downloads latest's release asset for this platform, verifies
// it actually runs and reports the expected version, then atomically swaps
// it in at exe (runDaemon's startup-captured path — see its comment; must
// not be re-resolved via os.Executable() after the rename below). The old
// binary is kept as "<exe>.prev" as a manual escape hatch — self-restart
// happens separately once this returns successfully, so a bad binary is
// never exec'd blind.
func replaceBinary(exe, latest string) error {
	asset, ok := releaseAsset()
	if !ok {
		return fmt.Errorf("no release build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	dir := filepath.Dir(exe)

	tmp, err := os.CreateTemp(dir, ".pulse-update-*")
	if err != nil {
		return fmt.Errorf("no write access to %s; the browser can't prompt for a password, run `pulse update` from a terminal instead", dir)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", githubRepo, asset)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	n, err := io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if n < minReleaseSize {
		return fmt.Errorf("downloaded file looks truncated (%d bytes)", n)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("could not make the download executable: %w", err)
	}

	// Run it before trusting it: catches a corrupt download or a wrong-platform
	// asset without ever touching the binary that's currently serving.
	out, err := exec.Command(tmpPath, "--version").Output()
	got := strings.TrimSpace(string(out))
	if err != nil || normalizeVersion(got) != latest {
		return fmt.Errorf("downloaded build failed to verify (got %q): %w", got, err)
	}

	os.Rename(exe, exe+".prev") // best-effort backup; fine if it fails
	if err := os.Rename(tmpPath, exe); err != nil {
		return fmt.Errorf("could not install the new binary: %w", err)
	}
	return nil
}

// apiUpdate downloads and verifies the latest release, then schedules a
// self-restart into it. It responds before restarting so the browser gets a
// clean reply; the daemon comes back on the same port/token a moment later
// with agent sessions intact (same mechanism as backgrounding via "bg").
func (d *Daemon) apiUpdate(c echo.Context) error {
	if version == "dev" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "dev builds can't self-update"})
	}
	if d.tunnel {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "can't self-update while a tunnel is active"})
	}
	latest := latestRelease(true)
	if latest == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "could not reach GitHub to check for a release"})
	}
	if latest == normalizeVersion(version) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "already on the latest version"})
	}
	if err := replaceBinary(d.exePath, latest); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if err := c.JSON(http.StatusOK, map[string]bool{"ok": true}); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // let the response above actually reach the browser
		d.requestRestart()
	}()
	return nil
}
