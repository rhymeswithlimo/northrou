package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/scanner"
)

func TestFormatScanProgress(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	t.Run("total unknown yet prints nothing", func(t *testing.T) {
		_, ok := formatScanProgress(scanner.Progress{}, now)
		if ok {
			t.Fatal("expected ok=false when Total is 0")
		}
	})

	t.Run("no ETA before the first file completes", func(t *testing.T) {
		line, ok := formatScanProgress(scanner.Progress{
			Total: 81, Processed: 0, StartedAt: now,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if strings.Contains(line, "ETA") {
			t.Errorf("did not expect an ETA with zero completed files, got %q", line)
		}
		if !strings.Contains(line, "0/81") || !strings.Contains(line, "(0%)") {
			t.Errorf("expected 0/81 (0%%) in %q", line)
		}
	})

	t.Run("ETA projects remaining work from elapsed rate", func(t *testing.T) {
		started := now.Add(-40 * time.Second) // 40s for 40 files = 1s/file
		line, ok := formatScanProgress(scanner.Progress{
			Total: 81, Processed: 40, Matched: 30, Unmatched: 2, StartedAt: started,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		// 41 remaining * 1s/file = 41s.
		if !strings.Contains(line, "ETA 41s") {
			t.Errorf("expected ETA 41s in %q", line)
		}
		if !strings.Contains(line, "40/81 (49%)") {
			t.Errorf("expected 40/81 (49%%) in %q", line)
		}
		if !strings.Contains(line, "matched=30 unmatched=2") {
			t.Errorf("expected matched/unmatched counts in %q", line)
		}
	})

	t.Run("no ETA once every file is processed", func(t *testing.T) {
		line, ok := formatScanProgress(scanner.Progress{
			Total: 81, Processed: 81, StartedAt: now.Add(-time.Minute),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if strings.Contains(line, "ETA") {
			t.Errorf("did not expect an ETA once Processed == Total, got %q", line)
		}
	})

	t.Run("current file is appended by basename", func(t *testing.T) {
		line, _ := formatScanProgress(scanner.Progress{
			Total: 10, Processed: 1, StartedAt: now, CurrentFile: "/mnt/storage/Movies/Foo (2020)/foo.mkv",
		}, now)
		if !strings.HasSuffix(line, "foo.mkv") {
			t.Errorf("expected line to end with the current file's basename, got %q", line)
		}
	})
}
