// Package recommend implements Northrou's fully-local recommendation engine: a
// per-user taste profile updated incrementally from watch history with
// completion-weighted, time-decayed scoring, plus a set of row generators that
// query the profile against the unwatched library. No collaborative filtering
// is used, so everything runs against a single household's own data.
package recommend

import (
	"math"
	"strconv"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Affinity dimensions.
const (
	DimGenre    = "genre"
	DimDecade   = "decade"
	DimDirector = "director"
	DimActor    = "actor"
	DimLanguage = "language"
	DimRuntime  = "runtime"
	DimHour     = "hour"
)

// halfLife is the time-decay half-life: a signal's weight halves over this
// period, so recent behavior dominates without erasing history.
const halfLife = 180 * 24 * time.Hour

// completionThreshold: watches below this fraction are negative signals.
const completionThreshold = 0.40

// maxActorsScored limits how many top-billed actors a watch updates.
const maxActorsScored = 5

// signalFromCompletion maps a completion fraction to a taste signal in roughly
// [-0.5, 1.0]. Finishing is a strong positive; abandoning early is negative.
func signalFromCompletion(c float64) float64 {
	switch {
	case c >= 0.9:
		return 1.0
	case c >= completionThreshold:
		// Linear ramp from +0.2 (at 40%) to +0.9 (at 90%).
		return 0.2 + (c-completionThreshold)/(0.9-completionThreshold)*0.7
	default:
		return -0.5
	}
}

// amplifyRewatch increases a positive signal for rewatched titles.
func amplifyRewatch(signal float64, rewatchCount int) float64 {
	if signal <= 0 || rewatchCount <= 0 {
		return signal
	}
	n := rewatchCount
	if n > 3 {
		n = 3
	}
	return signal * (1 + float64(n)*0.4)
}

// decayFactor returns the multiplicative decay applied to an accumulator given
// the elapsed time since it was last updated.
func decayFactor(elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 1
	}
	return math.Pow(0.5, elapsed.Seconds()/halfLife.Seconds())
}

// updateAccumulator applies a new signal to an affinity accumulator with time
// decay and returns the updated row (score/weight are decayed running sums).
func updateAccumulator(a db.AffinityRow, signal float64, now time.Time) db.AffinityRow {
	f := 1.0
	if !a.UpdatedAt.IsZero() {
		f = decayFactor(now.Sub(a.UpdatedAt))
	}
	a.Score = a.Score*f + signal
	a.Weight = a.Weight*f + 1
	a.UpdatedAt = now
	return a
}

// normalized returns the mean signal for an accumulator (its affinity), in
// roughly [-0.5, 1.5]. Weight doubles as a confidence measure.
func normalized(a db.AffinityRow) float64 {
	if a.Weight <= 0 {
		return 0
	}
	return a.Score / a.Weight
}

// decayBucket / key helpers.

func decadeKey(year int) string {
	if year <= 0 {
		return ""
	}
	return strconv.Itoa((year / 10) * 10)
}

func runtimeBucket(mins int) string {
	switch {
	case mins <= 0:
		return ""
	case mins < 90:
		return "short"
	case mins < 120:
		return "medium"
	case mins < 150:
		return "long"
	default:
		return "epic"
	}
}

func hourBucket(t time.Time) string {
	h := t.Hour()
	switch {
	case h < 6:
		return "night"
	case h < 12:
		return "morning"
	case h < 18:
		return "afternoon"
	default:
		return "evening"
	}
}

func personKey(id int64) string { return strconv.FormatInt(id, 10) }
