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

func sameBackends(a, b []Backend) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The whole point of the verify step: an encoder being compiled into ffmpeg
// must NOT put its backend in the available list unless the device actually
// initializes. This mirrors the reported bug - an Intel-only laptop whose static
// ffmpeg has h264_nvenc/h264_qsv/h264_vaapi all compiled in, but only the Intel
// iGPU is present, so nvenc must not be claimed.
func TestSelectBackends_ExcludesUnusableHardware(t *testing.T) {
	compiled := map[string]bool{
		"h264_nvenc": true,
		"h264_qsv":   true,
		"h264_vaapi": true,
	}
	// Only VA-API initializes on this box; nvenc reports no capable device and
	// qsv can't load its runtime.
	verify := func(b Backend, _ string) (bool, string) {
		if b == VAAPI {
			return true, ""
		}
		return false, "no device"
	}
	got := selectBackends(compiled, verify)
	want := []Backend{VAAPI, Software}
	if !sameBackends(got, want) {
		t.Fatalf("selectBackends = %v, want %v (nvenc/qsv compiled but unusable must be dropped)", got, want)
	}
	if chooseBackend(got, "") == NVENC {
		t.Fatal("must never choose NVENC when no NVIDIA device initialized")
	}
}

// A compiled-in encoder whose backend has no hardware at all collapses to
// software - never a false hardware claim.
func TestSelectBackends_AllUnusableIsSoftware(t *testing.T) {
	compiled := map[string]bool{"h264_nvenc": true, "h264_qsv": true}
	verify := func(Backend, string) (bool, string) { return false, "no device" }
	if got := selectBackends(compiled, verify); !sameBackends(got, []Backend{Software}) {
		t.Fatalf("selectBackends = %v, want [software]", got)
	}
}

// A backend not compiled into ffmpeg is never even probed.
func TestSelectBackends_SkipsUncompiled(t *testing.T) {
	compiled := map[string]bool{"h264_qsv": true}
	probed := map[Backend]bool{}
	verify := func(b Backend, _ string) (bool, string) {
		probed[b] = true
		return true, ""
	}
	got := selectBackends(compiled, verify)
	if !sameBackends(got, []Backend{QSV, Software}) {
		t.Fatalf("selectBackends = %v, want [qsv software]", got)
	}
	if probed[NVENC] || probed[VAAPI] {
		t.Fatalf("probed a backend that was not compiled in: %v", probed)
	}
}
