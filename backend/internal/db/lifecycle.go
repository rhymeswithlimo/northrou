package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// ImpressionRef identifies one served home-row item.
type ImpressionRef struct {
	Kind string // "movie" | "show"
	ID   int64
}

// RecordItemImpressions bumps the served count (and last-shown time) for each
// item shown on the home screen. Deduplicate before calling: a title in three
// rows is still one impression this render.
func (d *DB) RecordItemImpressions(ctx context.Context, userID int64, items []ImpressionRef) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now()
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		for _, it := range items {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO item_impressions (user_id, kind, item_id, served_count, last_shown)
				VALUES (?, ?, ?, 1, ?)
				ON CONFLICT(user_id, kind, item_id) DO UPDATE SET
					served_count = served_count + 1,
					last_shown   = excluded.last_shown`,
				userID, it.Kind, it.ID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetMovieImpressions returns served counts for movie items, keyed by movie id.
// Movies are what the scoring path applies fatigue to.
func (d *DB) GetMovieImpressions(ctx context.Context, userID int64) (map[int64]int, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT item_id, served_count FROM item_impressions WHERE user_id = ? AND kind = 'movie'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// HomeCollectionRow is the persisted lifecycle record for one home row.
type HomeCollectionRow struct {
	Key          string
	Title        string
	Strategy     string
	ItemIDs      []int64
	ServedCount  int
	ClickCount   int
	CreatedAt    time.Time
	LastShown    time.Time
	State        string // "active" | "dormant"
	DormantUntil time.Time
}

// HomeCollectionServe is the per-render input for persisting a served row.
type HomeCollectionServe struct {
	Key      string
	Title    string
	Strategy string
	ItemIDs  []int64 // movie ids the row served this render
}

// GetHomeCollections returns a profile's persisted home-row lifecycle records,
// keyed by row key.
func (d *DB) GetHomeCollections(ctx context.Context, userID int64) (map[string]HomeCollectionRow, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT key, title, strategy, item_ids, served_count, click_count,
			created_at, last_shown, state, dormant_until
		FROM home_collections WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HomeCollectionRow{}
	for rows.Next() {
		var r HomeCollectionRow
		var itemIDs string
		var dormantUntil sql.NullTime
		if err := rows.Scan(&r.Key, &r.Title, &r.Strategy, &itemIDs, &r.ServedCount,
			&r.ClickCount, &r.CreatedAt, &r.LastShown, &r.State, &dormantUntil); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(itemIDs), &r.ItemIDs)
		if dormantUntil.Valid {
			r.DormantUntil = dormantUntil.Time
		}
		out[r.Key] = r
	}
	return out, rows.Err()
}

// RecordHomeCollectionsServed upserts the rows served this render: it bumps
// served_count, refreshes membership and last_shown, and preserves click_count,
// created_at, and lifecycle state. New rows start active.
func (d *DB) RecordHomeCollectionsServed(ctx context.Context, userID int64, served []HomeCollectionServe) error {
	if len(served) == 0 {
		return nil
	}
	now := time.Now()
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		for _, s := range served {
			ids, _ := json.Marshal(s.ItemIDs)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO home_collections
					(user_id, key, title, strategy, item_ids, served_count, click_count, created_at, last_shown, state)
				VALUES (?, ?, ?, ?, ?, 1, 0, ?, ?, 'active')
				ON CONFLICT(user_id, key) DO UPDATE SET
					title        = excluded.title,
					strategy     = excluded.strategy,
					item_ids     = excluded.item_ids,
					served_count = home_collections.served_count + 1,
					last_shown   = excluded.last_shown`,
				userID, s.Key, s.Title, s.Strategy, string(ids), now, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetHomeCollectionState updates a row's lifecycle state. A dormant row carries
// a revival time; reviving it (state='active') resets its counters so it gets a
// clean second chance.
func (d *DB) SetHomeCollectionState(ctx context.Context, userID int64, key, state string, dormantUntil time.Time) error {
	if state == "active" {
		_, err := d.ExecContext(ctx, `
			UPDATE home_collections
			SET state='active', dormant_until=NULL, served_count=0, click_count=0
			WHERE user_id=? AND key=?`, userID, key)
		return err
	}
	var du any
	if !dormantUntil.IsZero() {
		du = dormantUntil
	}
	_, err := d.ExecContext(ctx, `
		UPDATE home_collections SET state=?, dormant_until=? WHERE user_id=? AND key=?`,
		state, du, userID, key)
	return err
}

// CreditHomeCollectionWatch credits every active home row that surfaced the
// given movie: it increments the row's click_count. This turns the existing
// watch signal into the "was this row engaged with?" signal the lifecycle needs,
// with no extra client telemetry. Returns the number of rows credited.
func (d *DB) CreditHomeCollectionWatch(ctx context.Context, userID, movieID int64) (int, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT key, item_ids FROM home_collections WHERE user_id=? AND state='active'`, userID)
	if err != nil {
		return 0, err
	}
	var toCredit []string
	for rows.Next() {
		var key, itemIDs string
		if err := rows.Scan(&key, &itemIDs); err != nil {
			rows.Close()
			return 0, err
		}
		var ids []int64
		_ = json.Unmarshal([]byte(itemIDs), &ids)
		for _, id := range ids {
			if id == movieID {
				toCredit = append(toCredit, key)
				break
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, key := range toCredit {
		if _, err := d.ExecContext(ctx,
			`UPDATE home_collections SET click_count = click_count + 1 WHERE user_id=? AND key=?`,
			userID, key); err != nil {
			return 0, err
		}
	}
	return len(toCredit), nil
}
