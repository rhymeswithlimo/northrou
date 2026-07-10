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

	var reader io.Reader = resp.Body
	var hasher = sha256.New()
	if a.SHA256 != "" {
		reader = io.TeeReader(resp.Body, hasher)
	}

	got, err := extractBinaries(reader, a.Kind, m.binDir, want)
	if err != nil {
		return got, err
	}

	if a.SHA256 != "" {
		// Drain any trailer so the hash covers the full stream.
		_, _ = io.Copy(io.Discard, reader)
		sum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(sum, a.SHA256) {
			return got, fmt.Errorf("checksum mismatch for %s: got %s want %s", a.URL, sum, a.SHA256)
		}
	} else {
		slog.Warn("ffmpeg asset checksum not pinned; skipping verification", "url", a.URL)
	}
	return got, nil
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
