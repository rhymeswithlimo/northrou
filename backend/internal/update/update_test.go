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

func TestLatestAndSelectArchive(t *testing.T) {
	assetName := fmt.Sprintf("northrou_1.2.0_%s_%s.tar.gz", runtime.GOOS, archSuffix())
	if runtime.GOOS == "windows" {
		assetName = fmt.Sprintf("northrou_1.2.0_%s_%s.zip", runtime.GOOS, archSuffix())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name":"v1.2.0","body":"release notes here",
			"assets":[
				{"name":%q,"browser_download_url":"http://x/%s"},
				{"name":"checksums.txt","browser_download_url":"http://x/checksums.txt"}
			]}`, assetName, assetName)
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
	name, _, err := u.selectArchive(rel)
	if err != nil {
		t.Fatalf("selectArchive: %v", err)
	}
	if name != assetName {
		t.Errorf("selected %q, want %q", name, assetName)
	}
}
