package ffmpeg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ulikunitz/xz"
)

// Download/extraction size caps. Static ffmpeg builds are tens of MB compressed
// and ~100 MB per binary decompressed; these ceilings are far above that but
// bound memory/disk against an oversized response or a decompression bomb, since
// the download URLs are rolling upstream endpoints whose bytes we do not control.
const (
	maxFFmpegDownload = 512 << 20 // cap on the compressed download stream
	maxFFmpegBinary   = 512 << 20 // cap on a single extracted binary
)

// archiveKind identifies how a downloaded release asset is packed.
type archiveKind int

const (
	kindZip   archiveKind = iota // .zip (BtbN Windows, evermeet macOS)
	kindTarXz                    // .tar.xz (BtbN Linux, johnvansickle)
)

// wantBinary reports whether the archive entry with the given path is one of
// the binaries we want (matched by base name, ignoring the archive's internal
// directory layout which varies by build source).
func wantBinary(entryPath string, wanted map[string]bool) (string, bool) {
	base := path.Base(filepath.ToSlash(entryPath))
	if wanted[base] {
		return base, true
	}
	return "", false
}

// extractBinaries reads an archive from r and writes any entries whose base
// name is in wanted into destDir (flattened, executable). It returns the set of
// base names it actually extracted.
func extractBinaries(r io.Reader, kind archiveKind, destDir string, wanted map[string]bool) (map[string]bool, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	switch kind {
	case kindZip:
		return extractZip(r, destDir, wanted)
	case kindTarXz:
		return extractTarXz(r, destDir, wanted)
	default:
		return nil, fmt.Errorf("unknown archive kind %d", kind)
	}
}

func extractTarXz(r io.Reader, destDir string, wanted map[string]bool) (map[string]bool, error) {
	xzr, err := xz.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xz: %w", err)
	}
	tr := tar.NewReader(xzr)
	got := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return got, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base, ok := wantBinary(hdr.Name, wanted)
		if !ok {
			continue
		}
		if err := writeExecutable(filepath.Join(destDir, base), tr); err != nil {
			return got, err
		}
		got[base] = true
	}
	return got, nil
}

func extractZip(r io.Reader, destDir string, wanted map[string]bool) (map[string]bool, error) {
	// archive/zip needs a ReaderAt; buffer the (small) archive in memory.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	got := map[string]bool{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base, ok := wantBinary(f.Name, wanted)
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return got, err
		}
		err = writeExecutable(filepath.Join(destDir, base), rc)
		rc.Close()
		if err != nil {
			return got, err
		}
		got[base] = true
	}
	return got, nil
}

// writeExecutable writes r to path with 0o755 permissions, replacing any
// existing file.
func writeExecutable(dst string, r io.Reader) error {
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	// Cap the copy so a decompression bomb (a tiny archive entry that inflates to
	// gigabytes) cannot fill the disk.
	if n, err := io.Copy(f, io.LimitReader(r, maxFFmpegBinary+1)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	} else if n > maxFFmpegBinary {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("archive entry exceeds %d bytes", maxFFmpegBinary)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Ensure exec bit survived umask.
	_ = os.Chmod(tmp, 0o755)
	return os.Rename(tmp, dst)
}

// binaryName returns the platform-appropriate executable name.
func binaryName(base string, goos string) string {
	if goos == "windows" && !strings.HasSuffix(base, ".exe") {
		return base + ".exe"
	}
	return base
}

// ExecName returns the platform-appropriate executable name for base (adding
// ".exe" on Windows).
func ExecName(base string) string {
	return binaryName(base, runtime.GOOS)
}
