package docs

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// recordView counts a member opening a page, once per ten minutes per person
// (so reloads don't inflate it). Views are kept a year (cleanup).
func (s *Service) recordView(ctx context.Context, me, pageID uuid.UUID) {
	if access.ViaAgent(ctx) {
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO docs_page_views (page_id, day, user_id) VALUES ($1, current_date, $2)
		ON CONFLICT (page_id, day, user_id) DO UPDATE SET views = docs_page_views.views + 1, last_viewed_at = now()
		WHERE docs_page_views.last_viewed_at < now() - interval '10 minutes'`, pageID, me); err != nil {
		slogWarn(ctx, "docs: record view", err)
	}
}

// AnalyticsDay is one day of a page's views.
type AnalyticsDay struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Views   int    `json:"views"`
	Viewers int    `json:"viewers"`
}

// AnalyticsViewer is one member who opened the page in the period.
type AnalyticsViewer struct {
	User         RefUser   `json:"user"`
	Views        int       `json:"views"`
	LastViewedAt time.Time `json:"last_viewed_at"`
}

// Analytics is GET /docs/pages/{id}/analytics.
type Analytics struct {
	Days    []AnalyticsDay    `json:"days"`
	Views   int               `json:"views"`
	Viewers int               `json:"viewers"`
	People  []AnalyticsViewer `json:"people"`
	// Others counts viewers not listed by name (they left the workspace or
	// were deleted).
	Others int `json:"others"`
}

// analytics: GET /docs/pages/{id}/analytics?days=28 (edit): views and unique
// viewers per day, oldest first, and who opened it. Names are shown only for
// people who are still members of the page's workspace (the people the
// caller can already see in the directory); the rest are counted in others.
func (s *Service) analytics(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	days := 28
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil {
		days = min(max(v, 1), 365)
	}
	out := Analytics{Days: []AnalyticsDay{}, People: []AnalyticsViewer{}}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COALESCE(sum(v.views), 0)::int, count(v.user_id)::int
		FROM generate_series(current_date - ($2::int - 1), current_date, interval '1 day') AS d(day)
		LEFT JOIN docs_page_views v ON v.page_id = $1 AND v.day = d.day::date
		GROUP BY d.day ORDER BY d.day`, id, days)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d AnalyticsDay
		if err := rows.Scan(&d.Date, &d.Views, &d.Viewers); err != nil {
			rows.Close()
			return err
		}
		out.Days = append(out.Days, d)
		out.Views += d.Views
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(DISTINCT user_id)::int FROM docs_page_views WHERE page_id = $1 AND day > current_date - $2::int`,
		id, days).Scan(&out.Viewers); err != nil {
		return err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url, sum(v.views)::int, max(v.last_viewed_at)
		FROM docs_page_views v
		JOIN users u ON u.id = v.user_id AND u.status <> 'deleted'
		JOIN org_members om ON om.org_id = $3 AND om.user_id = v.user_id
		WHERE v.page_id = $1 AND v.day > current_date - $2::int
		GROUP BY u.id, u.display_name, u.avatar_url
		ORDER BY max(v.last_viewed_at) DESC, u.id LIMIT 50`, id, days, p.OrgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v AnalyticsViewer
		if err := rows.Scan(&v.User.ID, &v.User.Name, &v.User.AvatarURL, &v.Views, &v.LastViewedAt); err != nil {
			return err
		}
		out.People = append(out.People, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var named int
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT count(DISTINCT v.user_id)::int FROM docs_page_views v
		JOIN users u ON u.id = v.user_id AND u.status <> 'deleted'
		JOIN org_members om ON om.org_id = $3 AND om.user_id = v.user_id
		WHERE v.page_id = $1 AND v.day > current_date - $2::int`, id, days, p.OrgID).Scan(&named); err != nil {
		return err
	}
	out.Others = max(out.Viewers-named, 0)
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}
