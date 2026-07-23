// Package update implements Northrou's self-update: it checks GitHub Releases
// for a newer version, downloads the matching archive for the current
// OS/architecture, verifies its SHA-256 against the published checksums file,
// extracts the binary, and atomically replaces the running executable.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

// DefaultRepo is the source of official release binaries.
const DefaultRepo = "rhymeswithlimo/northrou"

// Updater checks for and applies updates from a GitHub repository's releases.
type Updater struct {
	repo    string // "owner/name"
	current string // current version (e.g. "v1.2.3" or "dev")
	http    *http.Client
	baseURL string // overridable in tests; empty means the real GitHub API
}

// New builds an Updater for the given repo and current version.
func New(repo, current string) *Updater {
	return &Updater{repo: repo, current: current, http: &http.Client{Timeout: 60 * time.Second}}
}

// Release is a parsed GitHub release.
type Release struct {
	Version string
	Notes   string
	assets  map[string]string // asset name -> download URL
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest fetches the newest published release.
func (u *Updater) Latest(ctx context.Context) (*Release, error) {
	base := u.baseURL
	if base == "" {
		base = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.repo)
	}
	return u.latestFrom(ctx, base)
}

// latestFrom fetches a release from an explicit URL (used by tests).
func (u *Updater) latestFrom(ctx context.Context, url string) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var gh ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return nil, err
	}
	rel := &Release{Version: gh.TagName, Notes: gh.Body, assets: map[string]string{}}
	for _, a := range gh.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

// HasUpdate reports whether the latest release is newer than the current build.
// Development builds ("dev") never auto-update.
//
// Comparison is on the version string with an optional leading "v" stripped
// from both sides: GitHub's tag_name keeps it ("v1.2.3") but GoReleaser's
// {{ .Version }}, which stamps buildinfo.Version, does not ("1.2.3"). Without
// normalizing, the same release never matches itself and every check reports
// an update, which the auto-update watcher would then "apply" and restart
// into forever.
func (u *Updater) HasUpdate(latest *Release) bool {
	if !u.enabled() || latest.Version == "" {
		return false
	}
	return normalizeVersion(latest.Version) != normalizeVersion(u.current)
}

// normalizeVersion strips an optional leading "v" for version comparison.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// enabled reports whether this build participates in updates at all. A build
// with no stamped version (local `go build`/`go run`) or explicitly "dev"
// never checks or applies updates.
func (u *Updater) enabled() bool {
	return u.current != "" && u.current != "dev"
}

// Apply downloads, verifies, extracts, and installs the update, replacing the
// running binary in place. The caller should restart the service afterward.
func (u *Updater) Apply(ctx context.Context, latest *Release) error {
	archiveName, archiveURL, err := u.selectArchive(latest)
	if err != nil {
		return err
	}
	archive, err := u.downloadBytes(ctx, archiveURL)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}

	// Verify against the published checksums file when present.
	if sumsURL, ok := latest.assets["checksums.txt"]; ok {
		sums, err := u.downloadBytes(ctx, sumsURL)
		if err != nil {
			return fmt.Errorf("download checksums: %w", err)
		}
		if err := verifyChecksum(archive, archiveName, sums); err != nil {
			return err
		}
	}

	binary, err := extractBinary(archive, archiveName)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}
	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{}); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// selectArchive picks the release asset matching this OS/architecture.
//
// The GitHub release also carries the coordinator_* archive, which shares the
// exact same _<os>_<arch> suffix as the northrou archive
// (e.g. coordinator_1.2.0_linux_amd64.tar.gz). Matching on the OS/arch token
// alone is therefore ambiguous, and since assets is a map, iteration order is
// random - roughly a coin flip that lands on the wrong archive, after which
// extractBinary fails with "northrou not found in archive". Anchor on the
// "northrou_" project-name prefix (from the goreleaser name_template) so only
// this binary's own archive can match.
func (u *Updater) selectArchive(latest *Release) (name, url string, err error) {
	prefix := "northrou_"
	token := "_" + runtime.GOOS + "_" + archSuffix()
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	for name, url := range latest.assets {
		if strings.HasPrefix(name, prefix) && strings.Contains(name, token) && strings.HasSuffix(name, ext) {
			return name, url, nil
		}
	}
	return "", "", fmt.Errorf("no release asset for %s/%s", runtime.GOOS, archSuffix())
}

func (u *Updater) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// archSuffix maps GOARCH to the goreleaser arch suffix (arm builds use armv7).
func archSuffix() string {
	if runtime.GOARCH == "arm" {
		return "armv7"
	}
	return runtime.GOARCH
}

// verifyChecksum confirms sha256(archive) matches the entry for archiveName in a
// standard "<hex>  <name>" checksums file.
func verifyChecksum(archive []byte, archiveName string, sums []byte) error {
	want := ""
	for line := range strings.SplitSeq(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archiveName {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum listed for %s", archiveName)
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	return nil
}

// extractBinary pulls the northrou executable out of a .tar.gz or .zip archive.
func extractBinary(archive []byte, name string) ([]byte, error) {
	wanted := "northrou"
	if runtime.GOOS == "windows" {
		wanted = "northrou.exe"
	}
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == wanted {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in zip", wanted)
	}

	gzr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Base(hdr.Name) == wanted {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", wanted)
}
