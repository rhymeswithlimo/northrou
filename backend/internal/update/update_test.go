package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// makeTarGz builds a .tar.gz containing a single "northrou" binary.
func makeTarGz(t *testing.T, binaryContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	name := "northrou"
	if runtime.GOOS == "windows" {
		name = "northrou.exe"
	}
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(binaryContent)), Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(binaryContent))
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}

func TestExtractBinaryTarGz(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tar.gz path is for non-windows")
	}
	archive := makeTarGz(t, "BINARY-CONTENT")
	got, err := extractBinary(archive, "northrou_1.0.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if string(got) != "BINARY-CONTENT" {
		t.Errorf("got %q", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("some archive bytes")
	sum := sha256.Sum256(archive)
	name := "northrou_1.0.0_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\nabc123  other.txt\n", hex.EncodeToString(sum[:]), name)

	if err := verifyChecksum(archive, name, []byte(sums)); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	// Tampered archive.
	if err := verifyChecksum([]byte("tampered"), name, []byte(sums)); err == nil {
		t.Error("expected checksum mismatch error")
	}
	// Missing entry.
	if err := verifyChecksum(archive, "missing.tar.gz", []byte(sums)); err == nil {
		t.Error("expected missing-checksum error")
	}
}

func TestHasUpdate(t *testing.T) {
	u := New("owner/repo", "v1.0.0")
	if !u.HasUpdate(&Release{Version: "v1.1.0"}) {
		t.Error("expected update available")
	}
	if u.HasUpdate(&Release{Version: "v1.0.0"}) {
		t.Error("same version should not be an update")
	}
	// Dev builds never auto-update.
	dev := New("owner/repo", "dev")
	if dev.HasUpdate(&Release{Version: "v9.9.9"}) {
		t.Error("dev build should not report updates")
	}
}

// TestHasUpdateVPrefixMismatch guards against comparing GoReleaser's
// {{ .Version }} (no leading "v", what buildinfo.Version is stamped with)
// against GitHub's tag_name (keeps the "v"): the same release must not report
// itself as an update, or the auto-update watcher would loop forever.
func TestHasUpdateVPrefixMismatch(t *testing.T) {
	u := New("owner/repo", "0.1.0")
	if u.HasUpdate(&Release{Version: "v0.1.0"}) {
		t.Error("same release with mismatched v-prefix reported as an update")
	}
	if !u.HasUpdate(&Release{Version: "v0.2.0"}) {
		t.Error("expected a genuinely newer release to be reported")
	}
}

func TestLatestAndSelectArchive(t *testing.T) {
	assetName := fmt.Sprintf("northrou_1.2.0_%s_%s.tar.gz", runtime.GOOS, archSuffix())
	if runtime.GOOS == "windows" {
		assetName = fmt.Sprintf("northrou_1.2.0_%s_%s.zip", runtime.GOOS, archSuffix())
	}
	// Sibling coordinator_/relay_ archives that share this host's exact
	// _<os>_<arch> suffix must NOT be selected. Using the host token (not a
	// hardcoded linux one) reproduces the collision on whatever OS the test
	// runs on, so `make test` on the dev Mac catches a regression too.
	coordName := fmt.Sprintf("coordinator_1.2.0_%s_%s.tar.gz", runtime.GOOS, archSuffix())
	relayName := fmt.Sprintf("relay_1.2.0_%s_%s.tar.gz", runtime.GOOS, archSuffix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name":"v1.2.0","body":"release notes here",
			"assets":[
				{"name":%[3]q,"browser_download_url":"http://x/coordinator"},
				{"name":%[4]q,"browser_download_url":"http://x/relay"},
				{"name":%[1]q,"browser_download_url":"http://x/%[2]s"},
				{"name":"checksums.txt","browser_download_url":"http://x/checksums.txt"}
			]}`, assetName, assetName, coordName, relayName)
	}))
	defer srv.Close()

	u := New("owner/repo", "v1.0.0")
	u.http = srv.Client()
	// Point Latest at the test server by overriding via a custom request path.
	rel, err := u.latestFrom(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if rel.Version != "v1.2.0" || rel.Notes != "release notes here" {
		t.Errorf("unexpected release: %+v", rel)
	}
	// assets is a map, so selectArchive ranges in a random order each call.
	// Repeat enough that a prefix-blind selector would hit a sibling almost
	// surely at least once, making the regression fail deterministically.
	for i := range 64 {
		name, _, err := u.selectArchive(rel)
		if err != nil {
			t.Fatalf("selectArchive: %v", err)
		}
		if name != assetName {
			t.Fatalf("selected %q, want %q (iteration %d)", name, assetName, i)
		}
	}
}
