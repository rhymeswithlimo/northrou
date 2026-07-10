package transcode

import (
	"runtime"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

func TestModeCost(t *testing.T) {
	cases := []struct {
		mode Mode
		want int
	}{
		{ModeDirectPlay, 0},
		{ModeRemux, 0},
		{ModeAudioTranscode, 1},
		{ModeVideoTranscode, 2},
	}
	for _, c := range cases {
		if got := modeCost(c.mode); got != c.want {
			t.Errorf("modeCost(%s) = %d, want %d", c.mode, got, c.want)
		}
	}
}

func TestVideoCapacity_FloorAndHardware(t *testing.T) {
	// Software: derived from CPU cores, never below 1.
	soft := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	if got := soft.videoCapacity(); got < 1 {
		t.Fatalf("software videoCapacity = %d, want >= 1", got)
	}
	if want := runtime.NumCPU() / 2; want > 0 && soft.videoCapacity() != want {
		t.Errorf("software videoCapacity = %d, want %d", soft.videoCapacity(), want)
	}

	// Hardware backends use the encoder estimate.
	nvenc := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.NVENC})
	if got := nvenc.videoCapacity(); got != 4 {
		t.Errorf("nvenc videoCapacity = %d, want 4", got)
	}
}

func TestTryAcquire_FreeModesNeverBlock(t *testing.T) {
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	// Direct and remux are stream copies: always admitted, no budget consumed.
	for i := 0; i < 100; i++ {
		if _, ok := sm.TryAcquire(ModeDirectPlay); !ok {
			t.Fatal("direct play should never be rejected")
		}
		if _, ok := sm.TryAcquire(ModeRemux); !ok {
			t.Fatal("remux should never be rejected")
		}
	}
	if sm.inFlightCost != 0 {
		t.Errorf("free modes consumed budget: inFlightCost = %d", sm.inFlightCost)
	}
}

func TestTryAcquire_VideoBudgetAndRelease(t *testing.T) {
	// Pin capacity to a known value by using NVENC (videoCapacity = 4).
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.NVENC})
	capN := sm.videoCapacity() // 4 video transcodes; budget = 8 units

	releases := make([]func(), 0, capN)
	for i := 0; i < capN; i++ {
		rel, ok := sm.TryAcquire(ModeVideoTranscode)
		if !ok {
			t.Fatalf("video transcode %d should be admitted (cap=%d)", i, capN)
		}
		releases = append(releases, rel)
	}
	// One past capacity must be rejected.
	if _, ok := sm.TryAcquire(ModeVideoTranscode); ok {
		t.Fatalf("video transcode %d should be rejected at capacity", capN+1)
	}
	// Releasing one frees exactly one slot.
	releases[0]()
	if _, ok := sm.TryAcquire(ModeVideoTranscode); !ok {
		t.Fatal("a slot should be available after release")
	}
	// Release is idempotent (safe to call again without over-crediting budget).
	releases[0]()
	if sm.inFlightCost != 2*capN {
		t.Errorf("inFlightCost = %d, want %d after double-release", sm.inFlightCost, 2*capN)
	}
}

func TestTryAcquire_SoftwareFloorAllowsOneTranscode(t *testing.T) {
	// Even on a 1-core box, a single video transcode must be admitted.
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	if _, ok := sm.TryAcquire(ModeVideoTranscode); !ok {
		t.Fatal("a single video transcode must always be admitted (floor of 1)")
	}
}

func TestTryAcquire_AudioLighterThanVideo(t *testing.T) {
	// NVENC budget = 8 units. Audio costs 1, so more audio streams fit than video.
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.NVENC})
	budget := 2 * sm.videoCapacity()
	for i := 0; i < budget; i++ {
		if _, ok := sm.TryAcquire(ModeAudioTranscode); !ok {
			t.Fatalf("audio transcode %d should fit within budget %d", i, budget)
		}
	}
	if _, ok := sm.TryAcquire(ModeAudioTranscode); ok {
		t.Fatal("audio transcode past budget should be rejected")
	}
}
