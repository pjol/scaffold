package crm

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// StageSummary is one open stage's visible deals.
type StageSummary struct {
	StageID uuid.UUID    `json:"stage_id"`
	Name    string       `json:"name"`
	Count   int          `json:"count"`
	Totals  []MoneyTotal `json:"totals"`
}

// ClosedSummary is deals won or lost in a period.
type ClosedSummary struct {
	Count  int          `json:"count"`
	Totals []MoneyTotal `json:"totals"`
}

// OwnerActivity counts what someone logged this week on records the caller can see.
type OwnerActivity struct {
	User     UserRef `json:"user"`
	Notes    int     `json:"notes"`
	Calls    int     `json:"calls"`
	Emails   int     `json:"emails"`
	Meetings int     `json:"meetings"`
	Tasks    int     `json:"tasks_done"`
	Total    int     `json:"total"`
}

// Dashboard is GET /v1/crm/orgs/{orgID}/dashboard. Every number comes from
// records the caller can see.
type Dashboard struct {
	Pipeline   Pipeline        `json:"pipeline"`
	Currency   string          `json:"currency"`
	Open       []StageSummary  `json:"open_by_stage"`
	OpenTotals []MoneyTotal    `json:"open_totals"`
	Won        ClosedSummary   `json:"won_this_month"`
	Lost       ClosedSummary   `json:"lost_this_month"`
	Activity   []OwnerActivity `json:"activity_this_week"`
	Tasks      TaskSummary     `json:"my_tasks"`
}

// dashboard: GET /v1/crm/orgs/{orgID}/dashboard?pipeline_id=&tz=
func (s *Service) dashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	tz, err := location(r.URL.Query().Get("tz"))
	if err != nil {
		return err
	}
	var pid *uuid.UUID
	if v := r.URL.Query().Get("pipeline_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("pipeline_id must be a pipeline id.")
		}
		pid = &id
	}
	d, err := s.loadDashboard(ctx, me(r), orgID, pid, tz)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) loadDashboard(ctx context.Context, userID, orgID uuid.UUID, pid *uuid.UUID, tz string) (*Dashboard, error) {
	board, err := s.boardTotals(ctx, userID, orgID, pid, listSpec{})
	if err != nil {
		return nil, err
	}
	d := &Dashboard{Pipeline: board.Pipeline, Open: []StageSummary{}, OpenTotals: board.Open, Activity: []OwnerActivity{},
		Won: ClosedSummary{Totals: []MoneyTotal{}}, Lost: ClosedSummary{Totals: []MoneyTotal{}}}
	if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&d.Currency); err != nil {
		return nil, err
	}
	for _, c := range board.Columns {
		if c.Stage.Outcome == "open" {
			d.Open = append(d.Open, StageSummary{StageID: c.Stage.ID, Name: c.Stage.Name, Count: c.Count, Totals: c.Totals})
		}
	}
	// Won and lost this month (in the caller's time zone).
	b := newSQL(ctx, userID)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT st.outcome, r.currency, count(*), COALESCE(sum(r.amount), 0)::bigint
		`+visibleFrom(objDeal)+`
		WHERE r.org_id = `+b.arg(orgID)+` AND r.pipeline_id = `+b.arg(board.Pipeline.ID)+` AND st.outcome <> 'open'
		  AND r.closed_at >= (date_trunc('month', now() AT TIME ZONE `+b.arg(tz)+`) AT TIME ZONE $5)
		GROUP BY st.outcome, r.currency ORDER BY count(*) DESC`, b.args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var outcome string
		var t MoneyTotal
		var n int
		if err := rows.Scan(&outcome, &t.Currency, &n, &t.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		sum := &d.Won
		if outcome == "lost" {
			sum = &d.Lost
		}
		sum.Count += n
		sum.Totals = append(sum.Totals, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Activity this week by author (and tasks done by assignee).
	rows, err = s.d.Pool.Query(ctx, `
		WITH wk AS (SELECT (date_trunc('week', now() AT TIME ZONE $4) AT TIME ZONE $4) AS start),
		acts AS (
			SELECT a.created_by AS uid, a.type, 1 AS n FROM crm_activities a, wk
			WHERE a.org_id = $3 AND a.type <> 'task' AND a.occurred_at >= wk.start AND a.occurred_at <= now() AND `+taskVisible+`
			UNION ALL
			SELECT a.assignee_id, 'task', 1 FROM crm_activities a, wk
			WHERE a.org_id = $3 AND a.type = 'task' AND a.done_at >= wk.start AND `+taskVisible+`
		)
		SELECT u.id, u.display_name, u.avatar_url,
		       count(*) FILTER (WHERE x.type = 'note'), count(*) FILTER (WHERE x.type = 'call'),
		       count(*) FILTER (WHERE x.type = 'email'), count(*) FILTER (WHERE x.type = 'meeting'),
		       count(*) FILTER (WHERE x.type = 'task'), count(*)
		FROM acts x JOIN users u ON u.id = x.uid AND u.status <> 'deleted'
		GROUP BY u.id, u.display_name, u.avatar_url ORDER BY count(*) DESC, u.display_name LIMIT 10`,
		userID, b.args[1], orgID, tz)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a OwnerActivity
		if err := rows.Scan(&a.User.ID, &a.User.Name, &a.User.AvatarURL, &a.Notes, &a.Calls, &a.Emails, &a.Meetings, &a.Tasks, &a.Total); err != nil {
			rows.Close()
			return nil, err
		}
		d.Activity = append(d.Activity, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sum, err := s.summary(ctx, userID, orgID, tz)
	if err != nil {
		return nil, err
	}
	d.Tasks = *sum
	return d, nil
}

// boardTotals is the board without cards: each stage's visible count and sums.
func (s *Service) boardTotals(ctx context.Context, userID, orgID uuid.UUID, pid *uuid.UUID, spec listSpec) (*Board, error) {
	pipelines, err := s.loadPipelines(ctx, s.d.Pool, orgID)
	if err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, httpx.NotFound("Add a pipeline in CRM settings first.")
	}
	pl := pipelines[0]
	if pid != nil {
		found := false
		for _, p := range pipelines {
			if p.ID == *pid {
				pl, found = p, true
			}
		}
		if !found {
			return nil, httpx.NotFound("Pipeline not found.")
		}
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objDeal)
	if err != nil {
		return nil, err
	}
	spec.where = append(append([]func(*sqlb) string{}, spec.where...), func(b *sqlb) string { return "r.pipeline_id = " + b.arg(pl.ID) })
	totals, counts, err := s.stageTotals(ctx, userID, orgID, props, spec)
	if err != nil {
		return nil, err
	}
	board := &Board{Pipeline: pl, Columns: []BoardColumn{}, Open: []MoneyTotal{}}
	open := map[string]*MoneyTotal{}
	for _, st := range pl.Stages {
		col := BoardColumn{Stage: st, Count: counts[st.ID], Totals: totals[st.ID], Deals: []recordish{}}
		if col.Totals == nil {
			col.Totals = []MoneyTotal{}
		}
		if st.Outcome == "open" {
			for _, t := range col.Totals {
				if open[t.Currency] == nil {
					open[t.Currency] = &MoneyTotal{Currency: t.Currency}
				}
				open[t.Currency].Amount += t.Amount
				open[t.Currency].Weighted += t.Weighted
			}
		}
		board.Columns = append(board.Columns, col)
	}
	for _, t := range open {
		board.Open = append(board.Open, *t)
	}
	return board, nil
}

// ---- search -------------------------------------------------------------------------

// SearchHit is a record found by name, email, phone or domain.
type SearchHit struct {
	Type   objectType `json:"type"`
	ID     uuid.UUID  `json:"id"`
	OrgID  uuid.UUID  `json:"org_id"`
	Title  string     `json:"title"`
	Detail string     `json:"detail"` // email, domain or stage
	Score  float64    `json:"score"`
	At     time.Time  `json:"updated_at"`
}

// searchRoute: GET /v1/crm/orgs/{orgID}/search?q=&types=company,contact,deal&limit=
func (s *Service) searchRoute(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	types := parseTypes(r.URL.Query().Get("types"))
	limit := httpx.ParsePage(r).Limit
	hits, err := s.search(r.Context(), me(r), []uuid.UUID{orgParam(r)}, q, types, limit)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": hits})
	return nil
}

func parseTypes(v string) []objectType {
	out := []objectType{}
	for _, t := range strings.Split(v, ",") {
		if o, ok := parseObject(t); ok {
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return objectTypes
	}
	return out
}

// search finds visible records in orgs (the same access_visible join as the
// lists, agent-capped through ctx). An empty q lists recently updated ones.
func (s *Service) search(ctx context.Context, userID uuid.UUID, orgIDs []uuid.UUID, q string, types []objectType, limit int) ([]SearchHit, error) {
	out := []SearchHit{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q = clip(q, 200)
	for _, o := range types {
		b := newSQL(ctx, userID)
		orgs := b.arg(orgIDs)
		var cond, score, detail string
		if q == "" {
			cond, score = "true", "0.5"
		} else {
			exact, pre, pat := b.arg(strings.ToLower(q)), b.arg(likeEscape(strings.ToLower(q))+"%"), b.arg(likePattern(q))
			switch o {
			case objCompany:
				cond = "(r.name ILIKE " + pat + " OR COALESCE(r.domain, '') ILIKE " + pat + ")"
				score = "CASE WHEN lower(r.name) = " + exact + " OR r.domain = " + exact + " THEN 1.0 WHEN lower(r.name) LIKE " + pre + " OR r.domain LIKE " + pre + " THEN 0.8 ELSE 0.5 END"
			case objContact:
				cond = "((r.first_name || ' ' || r.last_name) ILIKE " + pat + " OR array_to_string(r.emails, ' ') ILIKE " + pat + " OR array_to_string(r.phones, ' ') ILIKE " + pat + ")"
				score = "CASE WHEN lower(btrim(r.first_name || ' ' || r.last_name)) = " + exact + " OR " + exact + " = ANY (r.emails) THEN 1.0 " +
					"WHEN lower(r.first_name) LIKE " + pre + " OR lower(r.last_name) LIKE " + pre + " OR COALESCE(r.emails[1], '') LIKE " + pre + " THEN 0.8 ELSE 0.5 END"
			default:
				cond = "r.name ILIKE " + pat
				score = "CASE WHEN lower(r.name) = " + exact + " THEN 1.0 WHEN lower(r.name) LIKE " + pre + " THEN 0.8 ELSE 0.5 END"
			}
		}
		switch o {
		case objCompany:
			detail = "COALESCE(r.domain, r.industry)"
		case objContact:
			detail = "COALESCE(r.emails[1], r.job_title)"
		default:
			detail = "st.name"
		}
		rows, err := s.d.Pool.Query(ctx, `SELECT r.id, r.org_id, `+o.titleExpr()+`, COALESCE(`+detail+`, ''), (`+score+`)::float8, r.updated_at
			`+visibleFrom(o)+` WHERE r.org_id = ANY (`+orgs+`) AND `+cond+`
			ORDER BY 5 DESC, r.updated_at DESC, r.id LIMIT `+b.arg(limit), b.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			h := SearchHit{Type: o}
			if err := rows.Scan(&h.ID, &h.OrgID, &h.Title, &h.Detail, &h.Score, &h.At); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sortHits(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func likeEscape(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}

// Search is the app.SearchProvider: records in every org where the caller
// uses the CRM (or the one the search is scoped to, app.SearchOrg), with the
// same visibility as the lists. Context is the org, then "CRM". Workspaces
// with connected CRMs add the caller's HubSpot and Salesforce hits, searched
// live with their own connection and time-boxed (Context: the org, then
// "<platform> · <account>"; Source set).
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []app.SearchResult{}, nil
	}
	orgIDs, err := s.crmOrgs(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	if scope, ok := app.SearchOrg(ctx); ok {
		orgIDs = slices.DeleteFunc(orgIDs, func(id uuid.UUID) bool { return id != scope })
	}
	appOrgs := []uuid.UUID{}
	modes := map[uuid.UUID]sourceSettings{}
	for _, id := range orgIDs {
		st, err := s.sourceSettings(ctx, id)
		if db.IsNoRows(err) {
			st = sourceSettings{Mode: SourceModeApp}
		} else if err != nil {
			return nil, err
		}
		modes[id] = st
		if st.allowsApp() {
			appOrgs = append(appOrgs, id)
		}
	}
	hits, err := s.search(ctx, caller.UserID, appOrgs, q, objectTypes, limit)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	if len(orgIDs) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT id, name FROM orgs WHERE id = ANY ($1)`, orgIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			names[id] = name
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]app.SearchResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, app.SearchResult{Kind: h.Type.kind(), ID: h.ID.String(), Title: h.Title, Snippet: h.Detail,
			Link: h.Type.link(h.ID), Icon: h.Type.icon(), Score: h.Score, At: h.At,
			OrgID: h.OrgID.String(), Context: app.SearchContext(names[h.OrgID], "CRM")})
	}
	seen := map[string]bool{}
	for _, orgID := range orgIDs {
		st := modes[orgID]
		if !st.allowsConnected() {
			continue
		}
		xhits := s.connectedSearch(ctx, caller.UserID, orgID, st, q, objectTypes, limit)
		xhits, err := s.agentFilterHits(ctx, caller, xhits)
		if err != nil {
			return nil, err
		}
		for _, h := range xhits {
			if seen[h.ID] {
				continue
			}
			seen[h.ID] = true
			label := providerLabel(h.Source.Provider)
			if h.Source.Account != "" {
				label += " · " + h.Source.Account
			}
			out = append(out, app.SearchResult{Kind: h.Type.kind(), ID: h.ID, Title: h.Title, Snippet: h.Detail,
				Link: "/crm/" + h.Type.path() + "/" + h.ID, Icon: h.Type.icon(), Score: h.Score, At: h.At,
				OrgID: orgID.String(), Context: app.SearchContext(names[orgID], label), Source: h.Source})
		}
	}
	return out, nil
}

func sortHits(h []SearchHit) {
	sort.SliceStable(h, func(i, j int) bool {
		if h[i].Score != h[j].Score {
			return h[i].Score > h[j].Score
		}
		return h[i].At.After(h[j].At)
	})
}
