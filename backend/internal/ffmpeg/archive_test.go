package ffmpeg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulikunitz/xz"
)

func makeTarXz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var xzBuf bytes.Buffer
	xw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		t.Fatalf("xz writer: %v", err)
	}
	tw := tar.NewWriter(xw)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	return xzBuf.Bytes()
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractTarXz_FlattensByBasename(t *testing.T) {
	// BtbN-style layout: binaries nested under ffmpeg-*/bin/.
	data := makeTarXz(t, map[string]string{
		"ffmpeg-master/bin/ffmpeg":  "FFMPEG-BINARY",
		"ffmpeg-master/bin/ffprobe": "FFPROBE-BINARY",
		"ffmpeg-master/README.txt":  "ignore me",
		"ffmpeg-master/bin/ffplay":  "unwanted",
	})
	dest := t.TempDir()
	want := map[string]bool{"ffmpeg": true, "ffprobe": true}

	got, err := extractBinaries(bytes.NewReader(data), kindTarXz, dest, want)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !got["ffmpeg"] || !got["ffprobe"] {
		t.Fatalf("did not extract both binaries: %v", got)
	}
	assertFileContent(t, filepath.Join(dest, "ffmpeg"), "FFMPEG-BINARY")
	assertFileContent(t, filepath.Join(dest, "ffprobe"), "FFPROBE-BINARY")
	if _, err := os.Stat(filepath.Join(dest, "ffplay")); err == nil {
		t.Fatal("ffplay should not have been extracted")
	}
}

func TestExtractZip_FlattensByBasename(t *testing.T) {
	data := makeZip(t, map[string]string{
		"ffmpeg":  "ZIPPED-FFMPEG",
		"ffprobe": "ZIPPED-FFPROBE",
	})
	dest := t.TempDir()
	want := map[string]bool{"ffmpeg": true, "ffprobe": true}

	got, err := extractBinaries(bytes.NewReader(data), kindZip, dest, want)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 binaries, got %v", got)
	}
	assertExecutable(t, filepath.Join(dest, "ffmpeg"))
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(b) != want {
		t.Fatalf("%s: got %q want %q", path, b, want)
	}
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("%s is not executable: %v", path, info.Mode())
	}
}
