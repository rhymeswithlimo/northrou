// Package logging gives the daemon a persistent, size-rotated log file next to
// its other state, so `northrou logs` (and the settings page) can show what the
// server has been doing regardless of which init system captured stderr - or
// whether anything captured it at all.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// File is the log file's name inside Dir under the data directory.
const (
	Dir  = "logs"
	File = "northrou.log"
)

// Path returns the log file path for a data directory.
func Path(dataDir string) string {
	return filepath.Join(dataDir, Dir, File)
}

// rotatingFile is an append-only writer that renames the file aside and starts
// fresh once it grows past maxBytes. One rotation (a single .old file) is
// plenty: this exists so "what happened recently" survives a restart, not as
// an archival system.
type rotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	size     int64
	f        *os.File
}

// newRotatingFile opens (creating if needed) the log file for appending.
func newRotatingFile(path string, maxBytes int64) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotatingFile{path: path, maxBytes: maxBytes, size: info.Size(), f: f}, nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			// Rotation failing must not lose the log line; keep appending.
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) rotate() error {
	r.f.Close()
	if err := os.Rename(r.path, r.path+".old"); err != nil && !os.IsNotExist(err) {
		// Reopen the original either way so writes keep going somewhere.
		f, ferr := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if ferr != nil {
			return ferr
		}
		r.f = f
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	r.f = f
	r.size = 0
	return nil
}

// teeHandler fans records out to two slog handlers (terminal + file).
type teeHandler struct{ a, b slog.Handler }

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return t.a.Enabled(ctx, l) || t.b.Enabled(ctx, l)
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errA, errB error
	if t.a.Enabled(ctx, r.Level) {
		errA = t.a.Handle(ctx, r.Clone())
	}
	if t.b.Enabled(ctx, r.Level) {
		errB = t.b.Handle(ctx, r.Clone())
	}
	if errA != nil {
		return errA
	}
	return errB
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return teeHandler{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
}

// Tail returns the last n lines of the log file at path. It reads at most the
// file's final 512 KB - log lines are short, and a bounded read keeps a huge
// file from being slurped whole. A missing file is reported as os.ErrNotExist.
func Tail(path string, n int) ([]byte, error) {
	const maxTail = 512 << 10
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(info.Size()-maxTail, 0)
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return LastLines(buf, n, start > 0), nil
}

// LastLines returns the final n lines of buf. When truncated is set the first
// (possibly partial) line is dropped, since the read started mid-file.
func LastLines(buf []byte, n int, truncated bool) []byte {
	if len(buf) == 0 {
		return buf
	}
	end := len(buf)
	// Ignore a trailing newline when counting back.
	scan := end
	if buf[scan-1] == '\n' {
		scan--
	}
	count := 0
	start := 0
	for i := scan - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			count++
			if count == n {
				start = i + 1
				break
			}
		}
	}
	if start == 0 && truncated {
		// Drop the partial first line from the bounded read.
		for i := range buf {
			if buf[i] == '\n' {
				start = i + 1
				break
			}
		}
	}
	return buf[start:end]
}

// AttachFile tees the process's default slog output into the rotating log file
// under dataDir, keeping the current default handler for the terminal/service
// manager. Best effort: an unwritable data dir logs a warning and changes
// nothing.
func AttachFile(dataDir string) {
	const maxBytes = 5 << 20 // 5 MB per file, plus one rotated .old
	rf, err := newRotatingFile(Path(dataDir), maxBytes)
	if err != nil {
		slog.Warn("could not open log file; file logging disabled", "err", err)
		return
	}
	fileHandler := slog.NewTextHandler(rf, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(teeHandler{a: slog.Default().Handler(), b: fileHandler}))
}
