package hwaccel

import (
	"runtime"
	"testing"
)

func TestChooseBackend_PrefersHardware(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("preference order differs on darwin")
	}
	avail := []Backend{QSV, Software}
	if got := chooseBackend(avail, ""); got != QSV {
		t.Errorf("expected QSV preferred over software, got %s", got)
	}
}

func TestChooseBackend_Override(t *testing.T) {
	avail := []Backend{NVENC, QSV, Software}
	if got := chooseBackend(avail, "qsv"); got != QSV {
		t.Errorf("override to qsv failed, got %s", got)
	}
	// "none" forces software.
	if got := chooseBackend(avail, "none"); got != Software {
		t.Errorf("override none should be software, got %s", got)
	}
	// Unavailable override falls back to the best available per the platform's
	// preference order (QSV is not preferred on darwin, so skip there).
	if runtime.GOOS != "darwin" {
		got := chooseBackend([]Backend{QSV, Software}, "nvenc")
		if got != QSV {
			t.Errorf("unavailable override should fall back, got %s", got)
		}
	}
}

func TestChooseBackend_SoftwareFallback(t *testing.T) {
	if got := chooseBackend([]Backend{Software}, ""); got != Software {
		t.Errorf("expected software, got %s", got)
	}
}
