package recommend

import (
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func TestSignalFromCompletion(t *testing.T) {
	if s := signalFromCompletion(1.0); s != 1.0 {
		t.Errorf("completed should be +1.0, got %f", s)
	}
	if s := signalFromCompletion(0.2); s >= 0 {
		t.Errorf("abandoned (20%%) should be negative, got %f", s)
	}
	mid := signalFromCompletion(0.65)
	if mid <= 0 || mid >= 1.0 {
		t.Errorf("partial watch should be a modest positive, got %f", mid)
	}
}

func TestAmplifyRewatch(t *testing.T) {
	base := 1.0
	if amplifyRewatch(base, 0) != base {
		t.Error("no rewatch should not amplify")
	}
	if amplifyRewatch(base, 2) <= base {
		t.Error("rewatch should amplify positive signal")
	}
	// Negative signals are not amplified.
	if amplifyRewatch(-0.5, 3) != -0.5 {
		t.Error("negative signal must not be amplified")
	}
}

func TestDecayFavorsRecent(t *testing.T) {
	now := time.Now()
	// Old strong-negative signal, then a recent strong-positive signal.
	a := db.AffinityRow{Dimension: DimGenre, Key: "Drama"}
	a = updateAccumulator(a, -0.5, now.Add(-2*halfLife)) // two half-lives ago
	a = updateAccumulator(a, 1.0, now)                    // now
	if normalized(a) <= 0 {
		t.Errorf("recent positive should dominate old negative, got %f", normalized(a))
	}
}

func TestDecayFactorHalfLife(t *testing.T) {
	f := decayFactor(halfLife)
	if f < 0.49 || f > 0.51 {
		t.Errorf("decay over one half-life should be ~0.5, got %f", f)
	}
}

func TestBuckets(t *testing.T) {
	if decadeKey(2014) != "2010" {
		t.Errorf("2014 => 2010s, got %s", decadeKey(2014))
	}
	if runtimeBucket(75) != "short" || runtimeBucket(200) != "epic" {
		t.Error("runtime bucketing wrong")
	}
}
