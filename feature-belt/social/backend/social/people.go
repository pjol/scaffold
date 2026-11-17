package social

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// likeEscape escapes LIKE wildcards ('_' is one, and handles contain it).
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// searchPeople matches handles by prefix and display names by the prefix
// of any word: exact handle first, then handle prefixes, then names, most
// followed first within each. Fewer than 2 characters match nothing.
func (s *Service) searchPeople(ctx context.Context, viewer uuid.UUID, q string, limit int) ([]Person, []time.Time, error) {
	q = strings.TrimSpace(q)
	h := NormalizeHandle(q)
	if utf8.RuneCountInString(strings.TrimPrefix(q, "@")) < 2 {
		return []Person{}, nil, nil
	}
	_, blocked, err := blockedSet(ctx, s.d.Pool, viewer)
	if err != nil {
		return nil, nil, err
	}
	name := likeEscape(strings.ToLower(strings.TrimPrefix(q, "@")))
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+personCols+`, p.updated_at
		FROM social_profiles p JOIN users u ON u.id = p.user_id AND u.status IN ('active', 'suspended')
		WHERE (p.handle LIKE $2 || '%' OR lower(u.display_name) LIKE $3 || '%' OR lower(u.display_name) LIKE '% ' || $3 || '%')
		  AND u.id <> ALL($4)
		ORDER BY (p.handle = $5) DESC, (p.handle LIKE $2 || '%') DESC, p.followers DESC, u.id
		LIMIT $6`, viewer, likeEscape(h), name, append(blocked, uuid.Nil), h, limit)
	if err != nil {
		return nil, nil, err
	}
	var ats []time.Time
	people, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		var at time.Time
		err := row.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.AvatarURL, &p.Followers, &p.IsFollowing, &p.FollowsYou, &at)
		ats = append(ats, at)
		return p, err
	})
	if people == nil {
		people = []Person{}
	}
	return people, ats, err
}

// getPeople: GET /v1/social/people?q=&limit= (at most 50).
func (s *Service) getPeople(w http.ResponseWriter, r *http.Request) error {
	limit := min(httpx.ParsePage(r).Limit, 50)
	people, _, err := s.searchPeople(r.Context(), auth.MustFromContext(r.Context()).UserID, r.URL.Query().Get("q"), limit)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": people})
	return nil
}

// suggestions are people followed by the people viewer follows (most
// shared first; the 200 newest follows each way are looked at), then the
// most followed, never anyone viewer follows, blocks or is blocked by.
func (s *Service) suggestions(ctx context.Context, viewer uuid.UUID, limit int) ([]Person, error) {
	_, blocked, err := blockedSet(ctx, s.d.Pool, viewer)
	if err != nil {
		return nil, err
	}
	blocked = append(blocked, uuid.Nil, viewer)
	rows, err := s.d.Pool.Query(ctx, `
		WITH mine AS (
			SELECT followee_id FROM social_follows WHERE follower_id = $1 ORDER BY created_at DESC LIMIT 200),
		cand AS (
			SELECT f2.followee_id AS id, count(*)::int AS via
			FROM mine m CROSS JOIN LATERAL (
				SELECT followee_id FROM social_follows WHERE follower_id = m.followee_id ORDER BY created_at DESC LIMIT 200) f2
			WHERE f2.followee_id <> ALL($2)
			  AND NOT EXISTS (SELECT 1 FROM social_follows x WHERE x.follower_id = $1 AND x.followee_id = f2.followee_id)
			GROUP BY f2.followee_id ORDER BY via DESC, f2.followee_id LIMIT $3)
		SELECT `+personCols+`, c.via
		FROM cand c JOIN users u ON u.id = c.id AND u.status = 'active' JOIN social_profiles p ON p.user_id = u.id
		ORDER BY c.via DESC, p.followers DESC, u.id`, viewer, blocked, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		var via int
		err := row.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.AvatarURL, &p.Followers, &p.IsFollowing, &p.FollowsYou, &via)
		p.Mutuals = &via
		return p, err
	})
	if err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}
	skip := append([]uuid.UUID{}, blocked...)
	for _, p := range out {
		skip = append(skip, p.UserID)
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT `+personCols+`
		FROM social_profiles p JOIN users u ON u.id = p.user_id AND u.status = 'active'
		WHERE u.id <> ALL($2)
		  AND NOT EXISTS (SELECT 1 FROM social_follows x WHERE x.follower_id = $1 AND x.followee_id = u.id)
		ORDER BY p.followers DESC, u.created_at DESC, u.id
		LIMIT $3`, viewer, skip, limit-len(out))
	if err != nil {
		return nil, err
	}
	popular, err := pgx.CollectRows(rows, scanPerson)
	if err != nil {
		return nil, err
	}
	return append(out, popular...), nil
}

// getSuggestions: GET /v1/social/suggestions?limit= (default 10, at most 30).
func (s *Service) getSuggestions(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	limit := 10
	if r.URL.Query().Get("limit") != "" {
		limit = min(httpx.ParsePage(r).Limit, 30)
	}
	if err := ensureProfiles(ctx, s.d.Pool, []uuid.UUID{me}); err != nil {
		return err
	}
	people, err := s.suggestions(ctx, me, limit)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": people})
	return nil
}

// Search implements app.SearchProvider: people by handle or name, as GET
// /social/people finds them (blocks either way left out).
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	people, ats, err := s.searchPeople(ctx, caller.UserID, query, limit)
	if err != nil {
		return nil, err
	}
	h := NormalizeHandle(query)
	out := make([]app.SearchResult, 0, len(people))
	for i, p := range people {
		score := 0.5
		switch {
		case p.Handle == h:
			score = 1
		case strings.HasPrefix(p.Handle, h):
			score = 0.8
		}
		title := p.DisplayName
		if strings.TrimSpace(title) == "" {
			title = "@" + p.Handle
		}
		out = append(out, app.SearchResult{
			Kind: "social.person", ID: p.UserID.String(), Title: title, Snippet: "@" + p.Handle,
			Link: "/u/" + p.Handle, Icon: "user-round", Score: score, At: ats[i], AuthorID: p.UserID.String(),
		})
	}
	return out, nil
}
