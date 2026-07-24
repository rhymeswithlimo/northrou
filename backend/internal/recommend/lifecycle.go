package recommend

import (
	"context"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Home-row lifecycle: persistence, fatigue, engagement-based ordering, and safe
// retirement. The generators still produce candidate rows every build; this
// layer remembers what was shown, how often, and whether it was ever engaged
// with, and uses that memory to suppress over-shown items, promote rows that
// work, and rest rows the household reliably ignores.

const (
	// fatiguePerServe is the score penalty added per prior impression of an
	// unwatched title (after the first free showing); fatigueMax caps it. This
	// only ever lowers a score, so it can't empty a row - it just lets fresher
	// candidates overtake one that's been shown and skipped repeatedly.
	fatiguePerServe = 0.03
	fatigueMax      = 0.30

	// retireServedThreshold: a row shown this many times with zero clicks (while
	// the profile has clicked *other* rows) goes dormant. dormantCooldown is how
	// long before it gets a fresh chance. served_count counts home *renders*
	// (roughly page loads past the 60s cache), not distinct sessions, so this is
	// set high enough that a row has to be genuinely, persistently ignored - a
	// single evening of browsing shouldn't rest anything.
	retireServedThreshold = 20
	dormantCooldown       = 14 * 24 * time.Hour

	// engagementBoostMax scales a row's confidence by its click-through rate, so
	// rows the household actually plays from rise over time.
	engagementBoostMax = 0.5

	// maxSameStrategyRows caps how many rows of one strategy (e.g. "Because You
	// Watched") appear, so the page doesn't stack near-duplicates.
	maxSameStrategyRows = 2
)

// fatiguePenalty returns the score penalty for a movie shown repeatedly but
// still unwatched. Impressions reflect prior renders (recorded after serving).
func (rc *rowContext) fatiguePenalty(movieID int64) float64 {
	n := rc.impressions[movieID]
	if n <= 1 {
		return 0 // one free showing before fatigue kicks in
	}
	p := fatiguePerServe * float64(n-1)
	if p > fatigueMax {
		p = fatigueMax
	}
	return p
}

// duplicationProneStrategies are the warm families that can emit many
// near-duplicate rows (one per director, theme, seed, etc.). Only these get
// collapsed to a shared strategy so the diversity cap can limit them. Every
// other key (for-you, rewatch, contrast, timectx-*, and all cold-* category
// rows) is its own strategy and is never capped - those are already distinct.
var duplicationProneStrategies = []string{
	"becausewatched-", "theme-", "director-", "decgenre-", "collection-",
}

// alwaysOnRow reports whether a row is foundational and must never be rested by
// the lifecycle. "for-you" is the primary recommendations row and load-bearing
// on a ~10-row home screen: unlike a service with 25-40 collections to rotate,
// retiring it would leave a visible hole. It can accrue zero clicks legitimately
// (the household plays via Continue Watching, Search, or a themed row instead),
// so engagement-based retirement must not touch it.
func alwaysOnRow(key string) bool {
	return key == "for-you"
}

// strategyOf derives a row's strategy family from its key. Duplication-prone
// families collapse to a shared name; anything else keeps its full, unique key.
func strategyOf(key string) string {
	for _, p := range duplicationProneStrategies {
		if strings.HasPrefix(key, p) {
			return strings.TrimSuffix(p, "-")
		}
	}
	return key
}

// applyLifecycle filters and re-weights candidate rows against the persisted
// registry: dormant rows still in cooldown are dropped; dormant rows past their
// cooldown are revived; and each surviving row's confidence is scaled by its
// historical click-through rate. It returns the surviving rows and the keys to
// revive (their counters reset on the caller's write).
func applyLifecycle(rows []Row, reg map[string]db.HomeCollectionRow, now time.Time) (kept []Row, revive []string) {
	for _, row := range rows {
		rc, ok := reg[row.Key]
		if ok && rc.State == "dormant" {
			if rc.DormantUntil.IsZero() || now.Before(rc.DormantUntil) {
				continue // still resting
			}
			revive = append(revive, row.Key)
			rc.ServedCount, rc.ClickCount = 0, 0 // treat as fresh from here
		}
		if rc.ServedCount > 0 {
			ctr := float64(rc.ClickCount) / float64(rc.ServedCount)
			row.Confidence *= 1 + engagementBoostMax*ctr
		}
		kept = append(kept, row)
	}
	return kept, revive
}

// recordServe persists the lifecycle effects of a completed home render:
// per-item impressions, per-row served counts + membership, and retirement of
// rows that have earned it. reg is the pre-render registry (for the retirement
// decision). Failures are non-fatal to serving the page, so callers log-and-go.
func (e *Engine) recordServe(ctx context.Context, userID int64, rows []Row, reg map[string]db.HomeCollectionRow, now time.Time) error {
	// A profile with any click anywhere is "engaged": only then is a zero-click
	// row meaningful evidence of disinterest rather than of a user who never
	// plays from home. This gate is what stops retirement from emptying the page.
	userHasClicks := false
	for _, r := range reg {
		if r.ClickCount > 0 {
			userHasClicks = true
			break
		}
	}

	seen := map[db.ImpressionRef]bool{}
	var imps []db.ImpressionRef
	served := make([]db.HomeCollectionServe, 0, len(rows))
	for _, row := range rows {
		var movieIDs []int64
		for _, it := range row.Items {
			ref := db.ImpressionRef{Kind: it.Kind, ID: it.ID}
			if !seen[ref] {
				seen[ref] = true
				imps = append(imps, ref)
			}
			if it.Kind == "movie" {
				movieIDs = append(movieIDs, it.ID)
			}
		}
		served = append(served, db.HomeCollectionServe{
			Key: row.Key, Title: row.Title, Strategy: strategyOf(row.Key), ItemIDs: movieIDs,
		})
	}

	if err := e.db.RecordItemImpressions(ctx, userID, imps); err != nil {
		return err
	}
	if err := e.db.RecordHomeCollectionsServed(ctx, userID, served); err != nil {
		return err
	}

	if !userHasClicks {
		return nil // never retire without engagement evidence
	}
	for _, row := range rows {
		if alwaysOnRow(row.Key) {
			continue // foundational rows are load-bearing; never rest them
		}
		prev := reg[row.Key]
		if prev.State == "dormant" {
			continue // just revived (or resting); don't immediately re-retire
		}
		newServed := prev.ServedCount + 1
		if newServed >= retireServedThreshold && prev.ClickCount == 0 {
			if err := e.db.SetHomeCollectionState(ctx, userID, row.Key, "dormant", now.Add(dormantCooldown)); err != nil {
				return err
			}
		}
	}
	return nil
}
