package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWantColor(t *testing.T) {
	// A regular file stands in for piped/redirected output: not a char device,
	// so never coloured regardless of env.
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	noEnv := func(string) (string, bool) { return "", false }
	withNoColor := func(k string) (string, bool) {
		if k == "NO_COLOR" {
			return "1", true
		}
		return "", false
	}

	if wantColor(f, noEnv) {
		t.Error("regular file must not be coloured (not a terminal)")
	}
	if wantColor(f, withNoColor) {
		t.Error("NO_COLOR set must never colour")
	}
	// /dev/null is a character device but writing escape codes there is
	// harmless; the point is that NO_COLOR still wins over a char device.
	if dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		defer dev.Close()
		if wantColor(dev, withNoColor) {
			t.Error("NO_COLOR must win even for a character device")
		}
	}
}
