// Package ffmpeg locates or downloads the static ffmpeg/ffprobe binaries
// Northrou depends on. Nothing is bundled into the Go binary; on first run the
// pinned static build for the current OS/arch is fetched into the data dir and
// checksum-verified. A system-installed ffmpeg can be used instead when
// configured.
package ffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Paths holds the resolved locations of the two binaries.
type Paths struct {
	FFmpeg  string
	FFprobe string
}

// Manager resolves and, if necessary, installs ffmpeg/ffprobe.
type Manager struct {
	binDir       string // data_dir/bin
	preferSystem bool
	httpClient   *http.Client

	mu    sync.Mutex
	paths Paths
}

// NewManager returns a Manager that stores managed binaries under
// dataDir/bin. If preferSystem is true, a system ffmpeg on PATH is used when
// present and usable.
func NewManager(dataDir string, preferSystem bool) *Manager {
	return &Manager{
		binDir:       filepath.Join(dataDir, "bin"),
		preferSystem: preferSystem,
		httpClient:   &http.Client{Timeout: 30 * time.Minute},
	}
}

// managedPaths returns the expected managed binary paths for this platform.
func (m *Manager) managedPaths() Paths {
	return Paths{
		FFmpeg:  filepath.Join(m.binDir, binaryName("ffmpeg", runtime.GOOS)),
		FFprobe: filepath.Join(m.binDir, binaryName("ffprobe", runtime.GOOS)),
	}
}

// Locate returns already-usable ffmpeg/ffprobe paths without downloading:
// managed binaries if present, or system binaries when preferSystem is set (or
// as a fallback). It returns ok=false if neither is available.
func (m *Manager) Locate() (Paths, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.locateLocked()
}

func (m *Manager) locateLocked() (Paths, bool) {
	if m.paths.FFmpeg != "" {
		return m.paths, true
	}
	// System first when preferred.
	if m.preferSystem {
		if p, ok := systemPaths(); ok {
			m.paths = p
			return p, true
		}
	}
	// Managed.
	mp := m.managedPaths()
	if fileExists(mp.FFmpeg) && fileExists(mp.FFprobe) {
		m.paths = mp
		return mp, true
	}
	// System fallback even when not preferred.
	if p, ok := systemPaths(); ok {
		m.paths = p
		return p, true
	}
	return Paths{}, false
}

// EnsureInstalled returns usable paths, downloading the managed static build
// for the current platform if nothing is available yet.
func (m *Manager) EnsureInstalled(ctx context.Context) (Paths, error) {
	if p, ok := m.Locate(); ok {
		return p, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check under lock.
	if p, ok := m.locateLocked(); ok {
		return p, nil
	}

	rel, ok := releaseFor()
	if !ok {
		return Paths{}, fmt.Errorf("no managed ffmpeg build for %s; install ffmpeg and set transcode.prefer_system_ffmpeg", runtime.GOOS+"/"+runtime.GOARCH)
	}

	slog.Info("downloading managed ffmpeg", "platform", runtime.GOOS+"/"+runtime.GOARCH, "dest", m.binDir)
	if err := m.install(ctx, rel); err != nil {
		return Paths{}, fmt.Errorf("install ffmpeg: %w", err)
	}

	mp := m.managedPaths()
	if !fileExists(mp.FFmpeg) || !fileExists(mp.FFprobe) {
		return Paths{}, errors.New("ffmpeg install completed but binaries are missing")
	}
	m.paths = mp
	slog.Info("managed ffmpeg ready", "ffmpeg", mp.FFmpeg, "ffprobe", mp.FFprobe)
	return mp, nil
}

// install downloads and extracts the binaries described by rel.
func (m *Manager) install(ctx context.Context, rel release) error {
	want := map[string]bool{
		binaryName("ffmpeg", runtime.GOOS):  true,
		binaryName("ffprobe", runtime.GOOS): true,
	}
	if rel.Bundle != nil {
		got, err := m.downloadAndExtract(ctx, *rel.Bundle, want)
		if err != nil {
			return err
		}
		return requireAll(got, want)
	}

	all := map[string]bool{}
	for _, a := range []*asset{rel.FFmpeg, rel.FFprobe} {
		if a == nil {
			continue
		}
		got, err := m.downloadAndExtract(ctx, *a, want)
		if err != nil {
			return err
		}
		for k := range got {
			all[k] = true
		}
	}
	return requireAll(all, want)
}

// downloadAndExtract streams the asset, verifies its checksum (if pinned), and
// extracts wanted binaries into binDir.
func (m *Manager) downloadAndExtract(ctx context.Context, a asset, want map[string]bool) (map[string]bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", a.URL, resp.StatusCode)
	}

	// Resolve the expected checksum: a static pin if any, else an upstream-
	// published checksum fetched at download time (rolling-URL-safe).
	wantSum := a.SHA256
	if wantSum == "" && a.SHA256URL != "" {
		wantSum, err = m.fetchChecksum(ctx, a.SHA256URL, a.URL)
		if err != nil {
			return nil, fmt.Errorf("fetch checksum: %w", err)
		}
	}

	// Cap the download to bound memory/disk against an oversized or
	// decompression-bomb response (the URLs are rolling upstream endpoints, so
	// the bytes are not under our control).
	var reader io.Reader = io.LimitReader(resp.Body, maxFFmpegDownload+1)
	var hasher = sha256.New()
	if wantSum != "" {
		reader = io.TeeReader(reader, hasher)
	}

	// Extract into a private staging dir and only promote a binary into the
	// managed binDir AFTER its checksum verifies. Writing straight to the final
	// path (the old behavior) meant a tampered or truncated download left a
	// binary at binDir that Locate() would find and EXECUTE unverified on the
	// next run - defeating the checksum the moment one is pinned.
	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(m.binDir, ".staging-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)

	got, err := extractBinaries(reader, a.Kind, staging, want)
	if err != nil {
		return nil, err
	}

	if wantSum != "" {
		// Drain any trailer so the hash covers the full stream.
		_, _ = io.Copy(io.Discard, reader)
		sum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(sum, wantSum) {
			return nil, fmt.Errorf("checksum mismatch for %s: got %s want %s", a.URL, sum, wantSum)
		}
	} else {
		slog.Warn("ffmpeg asset checksum not pinned; skipping verification", "url", a.URL)
	}

	// Verified (or unpinned): promote the staged binaries into the managed dir.
	for base := range got {
		if err := os.Rename(filepath.Join(staging, base), filepath.Join(m.binDir, base)); err != nil {
			return nil, fmt.Errorf("promote %s: %w", base, err)
		}
	}
	return got, nil
}

// fetchChecksum downloads a checksum document and extracts the SHA-256 for the
// asset at assetURL. The body may be a bare hex digest or an sha256sum-style
// listing ("<hex>  <name>"); in the latter case the line whose name matches the
// asset's base name (or, failing that, the sole 64-hex token) is used.
func (m *Manager) fetchChecksum(ctx context.Context, checksumURL, assetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	wantName := path.Base(assetURL)
	return parseChecksumDoc(string(body), wantName)
}

// parseChecksumDoc pulls a SHA-256 hex digest out of a checksum document, keying
// on wantName when the document lists multiple files.
func parseChecksumDoc(doc, wantName string) (string, error) {
	isHex64 := func(s string) bool {
		if len(s) != 64 {
			return false
		}
		_, err := hex.DecodeString(s)
		return err == nil
	}
	var only string
	var haveOnly bool
	for _, line := range strings.Split(doc, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Bare digest on its own line.
		if len(fields) == 1 && isHex64(fields[0]) {
			only, haveOnly = fields[0], !haveOnly && only == ""
			continue
		}
		// "<hex>  <name>" (sha256sum) or "<name>: <hex>" style.
		for i, f := range fields {
			if isHex64(f) {
				name := ""
				if i == 0 && len(fields) > 1 {
					name = fields[len(fields)-1]
				} else if i > 0 {
					name = strings.TrimSuffix(fields[0], ":")
				}
				if path.Base(name) == wantName {
					return strings.ToLower(f), nil
				}
				if only == "" {
					only = f
				}
			}
		}
	}
	if isHex64(only) {
		return strings.ToLower(only), nil
	}
	return "", fmt.Errorf("no sha256 for %s in checksum document", wantName)
}

// Version runs `ffmpeg -version` and returns the first line, verifying the
// binary is executable.
func (m *Manager) Version(ctx context.Context) (string, error) {
	p, ok := m.Locate()
	if !ok {
		return "", errors.New("ffmpeg not available")
	}
	out, err := exec.CommandContext(ctx, p.FFmpeg, "-version").Output()
	if err != nil {
		return "", err
	}
	line := string(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line), nil
}

// systemPaths looks up ffmpeg/ffprobe on PATH.
func systemPaths() (Paths, bool) {
	ff, err1 := exec.LookPath("ffmpeg")
	fp, err2 := exec.LookPath("ffprobe")
	if err1 == nil && err2 == nil {
		return Paths{FFmpeg: ff, FFprobe: fp}, true
	}
	return Paths{}, false
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func requireAll(got, want map[string]bool) error {
	var missing []string
	for k := range want {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("archive did not contain expected binaries: %s", strings.Join(missing, ", "))
	}
	return nil
}
