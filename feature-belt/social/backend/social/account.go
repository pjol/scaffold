package social

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount removes the person's profile, follows (both ways), follow
// notices and activity (theirs, and items about them). Their handles are
// held for SOCIAL_HANDLE_REDIRECT without any link to them, so nobody takes
// one over and inherits old links; then they are free.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		DELETE FROM social_follows WHERE follower_id = $1 OR followee_id = $1
		RETURNING CASE WHEN follower_id = $1 THEN followee_id ELSE follower_id END`, userID)
	if err != nil {
		return err
	}
	others, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM social_follow_notices WHERE follower_id = $1 OR followee_id = $1`,
		`DELETE FROM social_activity WHERE actor_id = $1 OR subject_id = $1`,
		`UPDATE social_handle_history SET user_id = NULL WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	var handle string
	err = tx.QueryRow(ctx, `DELETE FROM social_profiles WHERE user_id = $1 RETURNING handle`, userID).Scan(&handle)
	if err != nil && !isNoRows(err) {
		return err
	}
	if err := holdHandle(ctx, tx, handle, s.handleRedirect); err != nil {
		return err
	}
	if len(others) > 0 {
		return recount(ctx, tx, uniqueIDs(others))
	}
	return nil
}

// ExportAccount returns the profile, old handles still held for the person,
// who they follow and who follows them, and their activity (newest 5,000).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := map[string]any{"profile": nil}
	var p struct {
		Handle          string          `json:"handle"`
		Bio             string          `json:"bio"`
		Links           json.RawMessage `json:"links"`
		Privacy         json.RawMessage `json:"privacy"`
		HandleChangedAt *time.Time      `json:"handle_changed_at"`
		CreatedAt       time.Time       `json:"created_at"`
	}
	err := s.d.Pool.QueryRow(ctx, `SELECT handle, bio, links, privacy, handle_changed_at, created_at FROM social_profiles WHERE user_id = $1`, userID).
		Scan(&p.Handle, &p.Bio, &p.Links, &p.Privacy, &p.HandleChangedAt, &p.CreatedAt)
	if err == nil {
		out["profile"] = p
	} else if !isNoRows(err) {
		return nil, err
	}

	type held struct {
		Handle    string    `json:"handle"`
		ExpiresAt time.Time `json:"redirects_until"`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT handle, expires_at FROM social_handle_history WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	old, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (held, error) {
		var h held
		return h, r.Scan(&h.Handle, &h.ExpiresAt)
	})
	if err != nil {
		return nil, err
	}
	out["old_handles"] = old

	type edge struct {
		Handle string    `json:"handle"`
		Since  time.Time `json:"since"`
	}
	edges := func(sql string) ([]edge, error) {
		rows, err := s.d.Pool.Query(ctx, sql, userID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (edge, error) {
			var e edge
			return e, r.Scan(&e.Handle, &e.Since)
		})
	}
	if out["following"], err = edges(`SELECT p.handle, f.created_at FROM social_follows f JOIN social_profiles p ON p.user_id = f.followee_id
		WHERE f.follower_id = $1 ORDER BY f.created_at DESC`); err != nil {
		return nil, err
	}
	if out["followers"], err = edges(`SELECT p.handle, f.created_at FROM social_follows f JOIN social_profiles p ON p.user_id = f.follower_id
		WHERE f.followee_id = $1 ORDER BY f.created_at DESC`); err != nil {
		return nil, err
	}

	type item struct {
		Kind      string          `json:"kind"`
		Section   string          `json:"section"`
		RefID     string          `json:"ref_id,omitempty"`
		Data      json.RawMessage `json:"data"`
		CreatedAt time.Time       `json:"created_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT kind, section, ref_id, data, created_at FROM social_activity WHERE actor_id = $1
		ORDER BY created_at DESC LIMIT 5000`, userID)
	if err != nil {
		return nil, err
	}
	if out["activity"], err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var it item
		return it, r.Scan(&it.Kind, &it.Section, &it.RefID, &it.Data, &it.CreatedAt)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- jobs ------------------------------------------------------------------

// backfillProfiles gives accounts without a profile one (500 per run), so
// search and suggestions find people who never opened anything social.
func (s *Service) backfillProfiles(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id FROM users u
		WHERE u.status IN ('active', 'suspended') AND NOT EXISTS (SELECT 1 FROM social_profiles p WHERE p.user_id = u.id)
		ORDER BY u.created_at LIMIT 500`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) == 0 {
		return err
	}
	return ensureProfiles(ctx, s.d.Pool, ids)
}

// prune deletes activity older than SOCIAL_ACTIVITY_RETENTION (in batches),
// handle holds that expired, and follow-notice stamps nobody needs.
func (s *Service) prune(ctx context.Context) error {
	var total int64
	for round := 0; round < 20; round++ {
		tag, err := s.d.Pool.Exec(ctx, `DELETE FROM social_activity WHERE id IN (
			SELECT id FROM social_activity WHERE created_at < now() - $1::interval ORDER BY created_at LIMIT 10000)`, interval(s.retention))
		if err != nil {
			return err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < 10000 {
			break
		}
	}
	if total > 0 {
		slog.InfoContext(ctx, "social: activity pruned", "rows", total)
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM social_handle_history WHERE expires_at <= now()`); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM social_follow_notices WHERE sent_at < now() - $1::interval`, interval(2*noticeEvery))
	return err
}

// ---- development data ------------------------------------------------------

// SeedDev gives the dev accounts bios, links and follows (which record
// "followed" activity and notify through the real path). Idempotent: bios
// and links are set only while empty; follows that exist stay.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	if err := ensureProfiles(ctx, s.d.Pool, users); err != nil {
		return err
	}
	type seed struct {
		bio   string
		links []Link
	}
	seeds := []seed{
		{"Runs the team behind this app. Ask me about the roadmap.", nil},
		{"Product designer. Sketching interfaces on the morning train.", []Link{{URL: "https://example.com/sam", Label: "Portfolio"}}},
		{"Backend engineer: Go, Postgres and long bike rides.", []Link{{URL: "https://example.org/jordan", Label: "Blog"}}},
		{"Community lead. Book club on Thursdays.", nil},
	}
	for i, sd := range seeds {
		raw, _ := json.Marshal(sd.links)
		if sd.links == nil {
			raw = []byte("[]")
		}
		if _, err := s.d.Pool.Exec(ctx, `UPDATE social_profiles SET bio = $2, links = $3 WHERE user_id = $1 AND bio = '' AND links = '[]'::jsonb`,
			users[i], sd.bio, raw); err != nil {
			return err
		}
	}
	// Jordan shows followers and following only to followers.
	if _, err := s.d.Pool.Exec(ctx, `UPDATE social_profiles SET privacy = privacy || '{"connections": "followers"}'::jsonb
		WHERE user_id = $1 AND NOT privacy ? 'connections'`, users[2]); err != nil {
		return err
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	for _, e := range [][2]uuid.UUID{{sam, jordan}, {sam, priya}, {jordan, sam}, {priya, sam}, {priya, admin}, {jordan, admin}, {admin, priya}} {
		if _, err := s.follow(ctx, e[0], e[1]); err != nil && err != ErrBlocked {
			return err
		}
	}
	return nil
}
