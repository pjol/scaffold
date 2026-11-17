package crm

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// The forecast: visible deals by close month and owner, split by forecast
// category, with the probability-weighted amount, in one currency. Goals:
// targets per person or team per month, quarter or year, with progress from
// the same visible deals (so a viewer who can't see a deal doesn't see it
// counted either).

// ForecastCell is one owner's (or the total's) deals closing in one month.
type ForecastCell struct {
	Month    string `json:"month"` // YYYY-MM
	Pipeline int64  `json:"pipeline"`
	BestCase int64  `json:"best_case"`
	Commit   int64  `json:"commit"`
	Closed   int64  `json:"closed"`
	Weighted int64  `json:"weighted"` // open deals × stage probability
	Count    int    `json:"count"`
}

// ForecastRow is one owner across the months.
type ForecastRow struct {
	Owner *UserRef       `json:"owner"` // nil: deals without an owner
	Cells []ForecastCell `json:"cells"`
	Total ForecastCell   `json:"total"`
}

// Forecast is GET /v1/crm/orgs/{orgID}/forecast.
type Forecast struct {
	Currency   string         `json:"currency"`
	PipelineID *uuid.UUID     `json:"pipeline_id"`
	Months     []string       `json:"months"`
	Rows       []ForecastRow  `json:"rows"`
	Totals     []ForecastCell `json:"totals"`
	Total      ForecastCell   `json:"total"`
}

func (c *ForecastCell) add(o ForecastCell) {
	c.Pipeline += o.Pipeline
	c.BestCase += o.BestCase
	c.Commit += o.Commit
	c.Closed += o.Closed
	c.Weighted += o.Weighted
	c.Count += o.Count
}

func monthStart(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01", s)
	return t, err == nil
}

// forecast computes the forecast for userID over months starting at from.
func (s *Service) forecast(ctx context.Context, userID, orgID uuid.UUID, from time.Time, months int, pipelineID *uuid.UUID, currency string, owner *uuid.UUID) (*Forecast, error) {
	if months < 1 || months > 12 {
		return nil, httpx.BadRequest("months is 1 to 12.")
	}
	if currency == "" {
		if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&currency); err != nil {
			return nil, err
		}
	}
	if !currencyRe.MatchString(currency) {
		return nil, httpx.BadRequest("currency must be a 3-letter code like USD.")
	}
	f := &Forecast{Currency: currency, PipelineID: pipelineID, Months: []string{}, Rows: []ForecastRow{}, Totals: []ForecastCell{}}
	idx := map[string]int{}
	for i := 0; i < months; i++ {
		m := from.AddDate(0, i, 0).Format("2006-01")
		f.Months = append(f.Months, m)
		idx[m] = i
		f.Totals = append(f.Totals, ForecastCell{Month: m})
	}
	b := newSQL(ctx, userID)
	where := `r.org_id = ` + b.arg(orgID) + ` AND r.currency = ` + b.arg(currency) + ` AND r.close_date >= ` + b.arg(from.Format("2006-01-02")) +
		`::date AND r.close_date < ` + b.arg(from.AddDate(0, months, 0).Format("2006-01-02")) + `::date`
	if pipelineID != nil {
		where += ` AND r.pipeline_id = ` + b.arg(*pipelineID)
	}
	if owner != nil {
		where += ` AND ar.owner_id = ` + b.arg(*owner)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT ar.owner_id, ou.display_name, ou.avatar_url, to_char(r.close_date, 'YYYY-MM'), `+forecastExpr+`,
		count(*), COALESCE(sum(r.amount), 0)::bigint, COALESCE(sum(r.amount * st.probability / 100), 0)::bigint
		`+visibleFrom(objDeal)+` WHERE `+where+` AND `+forecastExpr+` <> 'omitted'
		GROUP BY 1, 2, 3, 4, 5 ORDER BY lower(ou.display_name) NULLS LAST, 1`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byOwner := map[string]int{}
	for rows.Next() {
		var ownerID *uuid.UUID
		var name, avatar *string
		var month, cat string
		var n int
		var amount, weighted int64
		if err := rows.Scan(&ownerID, &name, &avatar, &month, &cat, &n, &amount, &weighted); err != nil {
			return nil, err
		}
		key := ""
		if ownerID != nil {
			key = ownerID.String()
		}
		ri, ok := byOwner[key]
		if !ok {
			row := ForecastRow{Cells: make([]ForecastCell, months)}
			for i, m := range f.Months {
				row.Cells[i].Month = m
			}
			if ownerID != nil && name != nil {
				row.Owner = &UserRef{ID: *ownerID, Name: *name, AvatarURL: avatar}
			}
			f.Rows = append(f.Rows, row)
			ri = len(f.Rows) - 1
			byOwner[key] = ri
		}
		c := ForecastCell{Month: month, Count: n}
		switch cat {
		case "pipeline":
			c.Pipeline, c.Weighted = amount, weighted
		case "best_case":
			c.BestCase, c.Weighted = amount, weighted
		case "commit":
			c.Commit, c.Weighted = amount, weighted
		case "closed":
			c.Closed = amount
		}
		i := idx[month]
		f.Rows[ri].Cells[i].add(c)
		f.Rows[ri].Total.add(c)
		f.Totals[i].add(c)
		f.Total.add(c)
	}
	return f, rows.Err()
}

// getForecast: GET /v1/crm/orgs/{orgID}/forecast?from=YYYY-MM&months=3&pipeline_id=&currency=&owner=
func (s *Service) getForecast(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	from := time.Now().UTC()
	from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	if v := q.Get("from"); v != "" {
		t, ok := monthStart(v)
		if !ok {
			return httpx.BadRequest("from is a month like 2026-11.")
		}
		from = t
	}
	months := 3
	if v := q.Get("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return httpx.BadRequest("months is 1 to 12.")
		}
		months = n
	}
	var pid, owner *uuid.UUID
	if v := q.Get("pipeline_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("pipeline_id must be a pipeline id.")
		}
		pid = &id
	}
	if v := q.Get("owner"); v != "" {
		if v == "me" {
			id := me(r)
			owner = &id
		} else {
			id, err := uuid.Parse(v)
			if err != nil {
				return httpx.BadRequest("owner is me or a person's id.")
			}
			owner = &id
		}
	}
	f, err := s.forecast(r.Context(), me(r), orgParam(r), from, months, pid, strings.ToUpper(q.Get("currency")), owner)
	if err != nil {
		return err
	}
	httpx.OK(w, f)
	return nil
}

// ---- goals ------------------------------------------------------------------------

// Goal is a target with the caller's view of progress.
type Goal struct {
	ID          uuid.UUID `json:"id"`
	Metric      string    `json:"metric"` // revenue_won | deals_won | deals_created
	User        *UserRef  `json:"user"`
	Team        *TeamRef  `json:"team"`
	Period      string    `json:"period"` // month | quarter | year
	PeriodStart string    `json:"period_start"`
	PeriodEnd   string    `json:"period_end"` // exclusive
	Target      int64     `json:"target"`
	Currency    *string   `json:"currency"`
	Progress    int64     `json:"progress"`
}

func periodEnd(start time.Time, period string) time.Time {
	switch period {
	case "quarter":
		return start.AddDate(0, 3, 0)
	case "year":
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0)
}

// goalProgress counts what goal g covers among the deals userID can open.
func (s *Service) goalProgress(ctx context.Context, userID, orgID uuid.UUID, g *Goal) error {
	start, _ := time.Parse("2006-01-02", g.PeriodStart)
	end := periodEnd(start, g.Period)
	b := newSQL(ctx, userID)
	where := `r.org_id = ` + b.arg(orgID)
	if g.User != nil {
		where += ` AND ar.owner_id = ` + b.arg(g.User.ID)
	} else if g.Team != nil {
		where += ` AND ar.owner_id IN (SELECT user_id FROM access_team_members WHERE team_id = ` + b.arg(g.Team.ID) + `)`
	}
	var measure string
	switch g.Metric {
	case "revenue_won":
		measure = `COALESCE(sum(r.amount), 0)::bigint`
		where += ` AND st.outcome = 'won' AND r.currency = ` + b.arg(deref(g.Currency)) +
			` AND r.closed_at >= ` + b.arg(start) + ` AND r.closed_at < ` + b.arg(end)
	case "deals_won":
		measure = `count(*)::bigint`
		where += ` AND st.outcome = 'won' AND r.closed_at >= ` + b.arg(start) + ` AND r.closed_at < ` + b.arg(end)
	default:
		measure = `count(*)::bigint`
		where += ` AND r.created_at >= ` + b.arg(start) + ` AND r.created_at < ` + b.arg(end)
	}
	return s.d.Pool.QueryRow(ctx, `SELECT `+measure+` `+visibleFrom(objDeal)+` WHERE `+where, b.args...).Scan(&g.Progress)
}

func (s *Service) loadGoals(ctx context.Context, userID, orgID uuid.UUID, at time.Time, id *uuid.UUID) ([]*Goal, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT g.id, g.metric, g.user_id, u.display_name, u.avatar_url, g.team_id, t.name, t.handle,
		g.period, to_char(g.period_start, 'YYYY-MM-DD'), g.target, g.currency
		FROM crm_goals g LEFT JOIN users u ON u.id = g.user_id LEFT JOIN access_teams t ON t.id = g.team_id
		WHERE g.org_id = $1 AND ($3::uuid IS NOT NULL AND g.id = $3 OR $3::uuid IS NULL AND g.period_start <= $2::date
		  AND $2::date < g.period_start + CASE g.period WHEN 'month' THEN interval '1 month' WHEN 'quarter' THEN interval '3 months' ELSE interval '1 year' END)
		ORDER BY g.metric, lower(COALESCE(u.display_name, t.name)), g.id LIMIT 500`, orgID, at.Format("2006-01-02"), id)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Goal, error) {
		g := &Goal{}
		var uid, tid *uuid.UUID
		var uname, avatar, tname, handle *string
		err := row.Scan(&g.ID, &g.Metric, &uid, &uname, &avatar, &tid, &tname, &handle, &g.Period, &g.PeriodStart, &g.Target, &g.Currency)
		if uid != nil {
			name := "Former member"
			if uname != nil {
				name = *uname
			}
			g.User = &UserRef{ID: *uid, Name: name, AvatarURL: avatar}
		}
		if tid != nil && tname != nil {
			g.Team = &TeamRef{ID: *tid, Name: *tname, Handle: deref(handle)}
		}
		return g, err
	})
	if err != nil {
		return nil, err
	}
	for _, g := range list {
		start, _ := time.Parse("2006-01-02", g.PeriodStart)
		g.PeriodEnd = periodEnd(start, g.Period).Format("2006-01-02")
		if err := s.goalProgress(ctx, userID, orgID, g); err != nil {
			return nil, err
		}
	}
	if list == nil {
		list = []*Goal{}
	}
	return list, nil
}

// listGoals: GET /v1/crm/orgs/{orgID}/goals?at=YYYY-MM-DD — goals whose period
// covers the date (default today), with progress over the caller's visible deals.
func (s *Service) listGoals(w http.ResponseWriter, r *http.Request) error {
	at := time.Now().UTC()
	if v := r.URL.Query().Get("at"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return httpx.BadRequest("at is a date like 2026-11-30.")
		}
		at = t
	}
	list, err := s.loadGoals(r.Context(), me(r), orgParam(r), at, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type goalReq struct {
	Metric      string     `json:"metric"`
	UserID      *uuid.UUID `json:"user_id"`
	TeamID      *uuid.UUID `json:"team_id"`
	Period      string     `json:"period"`
	PeriodStart string     `json:"period_start"`
	Target      int64      `json:"target"`
	Currency    *string    `json:"currency"`
}

// createGoal: POST /v1/crm/orgs/{orgID}/goals (crm.manage_settings, audited).
func (s *Service) createGoal(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req goalReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	switch req.Metric {
	case "revenue_won", "deals_won", "deals_created":
	default:
		return httpx.BadRequest("metric is revenue_won, deals_won or deals_created.")
	}
	if (req.UserID == nil) == (req.TeamID == nil) {
		return httpx.BadRequest("Set a goal for one person or one team.")
	}
	start, err := time.Parse("2006-01-02", req.PeriodStart)
	if err != nil {
		return httpx.BadRequest("period_start is a date like 2026-10-01.")
	}
	switch req.Period {
	case "month":
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "quarter":
		start = time.Date(start.Year(), time.Month((int(start.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
	case "year":
		start = time.Date(start.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		return httpx.BadRequest("period is month, quarter or year.")
	}
	if req.Target <= 0 || req.Target > maxMoney {
		return httpx.BadRequest("A target is a number above 0 (minor units for revenue).")
	}
	var currency *string
	if req.Metric == "revenue_won" {
		c := strings.ToUpper(deref(req.Currency))
		if c == "" {
			if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&c); err != nil {
				return err
			}
		}
		if !currencyRe.MatchString(c) {
			return httpx.BadRequest("currency must be a 3-letter code like USD.")
		}
		currency = &c
	}
	if req.UserID != nil {
		if ok, err := isOrgMember(ctx, s.d.Pool, orgID, *req.UserID); err != nil || !ok {
			return httpx.BadRequest("Choose a member of this " + orgs.Noun() + ".")
		}
	}
	if req.TeamID != nil {
		var exists bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_teams WHERE id = $1 AND org_id = $2)`, *req.TeamID, orgID).Scan(&exists); err != nil || !exists {
			return httpx.BadRequest("Choose a team from this " + orgs.Noun() + ".")
		}
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO crm_goals (org_id, metric, user_id, team_id, period, period_start, target, currency, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, orgID, req.Metric, req.UserID, req.TeamID, req.Period, start, req.Target, currency, me(r)).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.goal.create", "crm.goal", id.String(), map[string]any{"org_id": orgID, "metric": req.Metric, "target": req.Target})
	})
	if err != nil {
		return err
	}
	list, err := s.loadGoals(ctx, me(r), orgID, start, &id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.JSON(w, http.StatusCreated, list[0])
	return nil
}

// updateGoal: PATCH /v1/crm/orgs/{orgID}/goals/{goalID} {target} (crm.manage_settings, audited).
func (s *Service) updateGoal(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "goalID")
	if err != nil {
		return err
	}
	var req struct {
		Target int64 `json:"target"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Target <= 0 || req.Target > maxMoney {
		return httpx.BadRequest("A target is a number above 0 (minor units for revenue).")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE crm_goals SET target = $3, updated_at = now() WHERE id = $1 AND org_id = $2`, id, orgID, req.Target)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("Goal not found.")
		}
		return audit.Log(ctx, tx, me(r), "crm.goal.update", "crm.goal", id.String(), map[string]any{"org_id": orgID, "target": req.Target})
	})
	if err != nil {
		return err
	}
	list, err := s.loadGoals(ctx, me(r), orgID, time.Time{}, &id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// deleteGoal: DELETE /v1/crm/orgs/{orgID}/goals/{goalID} (crm.manage_settings, audited).
func (s *Service) deleteGoal(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "goalID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM crm_goals WHERE id = $1 AND org_id = $2`, id, orgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("Goal not found.")
		}
		return audit.Log(ctx, tx, me(r), "crm.goal.delete", "crm.goal", id.String(), map[string]any{"org_id": orgID})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

