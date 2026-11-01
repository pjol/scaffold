package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// seedClientKey marks the seeded agent, so re-running the seed adds nothing.
const seedClientKey = "dev-seed-claude"

// SeedDev gives the dev identities (admin, sam, jordan, priya) agent
// settings and Sam one pending suggestion from a real installed write tool.
//   - Sam: default read; the seeded suggestion's feature (else the first with write tools) at suggest; their notifications off.
//   - Jordan: default suggest, the same write feature at edit.
//   - Priya and Avery: defaults.
//
// Existing rows are left alone (a developer's changes survive re-seeding).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	sam, jordan := users[1], users[2]
	// Suggest on the feature the seeded suggestion comes from, else the first
	// feature with write tools; their notification inbox off.
	var writeFeature, offFeature string
	if c, ok := s.seedTool(); ok {
		writeFeature = c.feature
	}
	for _, e := range s.featureCatalog() {
		if e.WriteTools > 0 && writeFeature == "" {
			writeFeature = e.Feature
		}
	}
	// Off: their notification inbox (always installed with this feature).
	for _, e := range s.featureCatalog() {
		if e.Feature == "notifications" && e.Feature != writeFeature {
			offFeature = e.Feature
		}
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, u := range []struct {
			id  uuid.UUID
			def Level
		}{{users[0], LevelRead}, {sam, LevelRead}, {jordan, LevelSuggest}, {users[3], LevelRead}} {
			if _, err := tx.Exec(ctx, `INSERT INTO agents_settings (user_id, default_level) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`, u.id, u.def); err != nil {
				return err
			}
		}
		feature := func(user uuid.UUID, f string, l Level) error {
			if f == "" {
				return nil
			}
			_, err := tx.Exec(ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, user, f, l)
			return err
		}
		if err := feature(sam, writeFeature, LevelSuggest); err != nil {
			return err
		}
		if err := feature(sam, offFeature, LevelOff); err != nil {
			return err
		}
		if err := feature(jordan, writeFeature, LevelEdit); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO agents_clients (user_id, via, client_key, client, last_seen_at) VALUES
			  ($1, 'mcp', $2, 'Claude', now() - interval '12 minutes'),
			  ($1, 'assistant', 'Assistant', 'Assistant', now() - interval '2 days')
			ON CONFLICT DO NOTHING`, sam, seedClientKey)
		return err
	})
	if err != nil {
		return err
	}
	return s.seedSuggestion(ctx, users, sam)
}

// seedSuggestion leaves Sam one pending suggestion from Claude, made with a
// write tool this app has (the first of a few known ones), so the inbox and
// Accept work for real. Without one of those tools it adds none.
func (s *Service) seedSuggestion(ctx context.Context, users []uuid.UUID, userID uuid.UUID) error {
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agents_suggestions WHERE user_id = $1 AND client_key = $2)`,
		userID, seedClientKey).Scan(&exists); err != nil || exists {
		return err
	}
	c, ok := s.seedTool()
	if !ok {
		return nil
	}
	// The preview is stored as the person reads it (their time zone, names),
	// and the tool's feature seeds after this one in name order: seed it
	// first (seeds are idempotent).
	if err := s.seedFeature(ctx, c.feature, users); err != nil {
		return err
	}
	caller, err := seedIdentity(ctx, s.d.Pool, userID)
	if err != nil {
		return err
	}
	pv, err := s.preview(ctx, caller, c.feature, c.Tool, c.input)
	if err != nil {
		return fmt.Errorf("preview %s: %w", c.Name, err)
	}
	_, err = s.d.Pool.Exec(ctx, `
		INSERT INTO agents_suggestions (user_id, via, client_key, client, tool, feature, input, summary, before_text, after_text, link,
		       expires_at, created_at)
		VALUES ($1, 'mcp', $2, 'Claude', $3, $4, $5, $6, $7, $8, $9, now() + make_interval(secs => $10), now() - interval '12 minutes')`,
		userID, seedClientKey, c.Name, c.feature, []byte(c.input), pv.Summary, pv.Before, pv.After, pv.Link, s.ttl.Seconds())
	return err
}

// seedFeature runs the dev seed of the module named feature, if it has one.
func (s *Service) seedFeature(ctx context.Context, feature string, users []uuid.UUID) error {
	for _, m := range s.d.Modules {
		if sd, ok := m.(app.DevSeeder); ok && m.Name() == feature {
			if err := sd.SeedDev(ctx, users); err != nil {
				return fmt.Errorf("seed %s first: %w", feature, err)
			}
		}
	}
	return nil
}

type seedCall struct {
	indexedTool
	input json.RawMessage
}

// seedTool is the first known write tool this app has, with a realistic input.
func (s *Service) seedTool() (seedCall, bool) {
	next := nextWeekday(time.Now().UTC()).Add(15 * time.Hour)
	candidates := []struct {
		tool  string
		input map[string]any
	}{
		{"calendar_create_event", map[string]any{"title": "Design review", "starts_at": next.Format(time.RFC3339), "ends_at": next.Add(time.Hour).Format(time.RFC3339),
			"description": "Walk through the new onboarding screens before they ship."}},
		{"calls_create_meeting", map[string]any{"title": "Design review", "starts_at": next.Format(time.RFC3339), "ends_at": next.Add(time.Hour).Format(time.RFC3339)}},
	}
	for _, c := range candidates {
		if t, ok := s.toolIndex()[c.tool]; ok && !t.ReadOnly {
			input, _ := json.Marshal(c.input)
			return seedCall{indexedTool: t, input: input}, true
		}
	}
	return seedCall{}, false
}

func seedIdentity(ctx context.Context, q db.Querier, userID uuid.UUID) (*auth.Identity, error) {
	id := &auth.Identity{UserID: userID}
	var role *string
	err := q.QueryRow(ctx, `SELECT auth_subject, email, display_name, admin_role, status,
		COALESCE(terms_version_accepted, ''), COALESCE(privacy_version_accepted, '') FROM users WHERE id = $1`, userID).
		Scan(&id.Subject, &id.Email, &id.DisplayName, &role, &id.Status, &id.TermsVersion, &id.PrivacyVersion)
	if role != nil {
		id.AdminRole = *role
	}
	return id, err
}

// nextWeekday is the next Monday–Friday after t, at midnight UTC.
func nextWeekday(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return d
}
