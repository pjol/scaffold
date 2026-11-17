package copytrade

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
)

// Config is GET /copytrade/config.
type Config struct {
	DisclosureVersion    string   `json:"disclosure_version"`
	DisclosureMd         string   `json:"disclosure_md"`
	Paused               bool     `json:"paused"`
	MinAllocation        string   `json:"min_allocation"`
	MaxProfitSharePct    string   `json:"max_profit_share_pct"`
	MaxCopiersPerLeader  int      `json:"max_copiers_per_leader"`
	MaxCopiesPerFollower int      `json:"max_copies_per_follower"`
	SettleIntervalHours  float64  `json:"settle_interval_hours"`
	DefaultSettings      Settings `json:"default_settings"` // for the minimum allocation; max_position_notional defaults to 3 × the allocation
}

func (s *Service) configHandler(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, Config{DisclosureVersion: DisclosureVersion, DisclosureMd: DisclosureText, Paused: s.paused,
		MinAllocation: s.fixed(s.minAllocation), MaxProfitSharePct: s.maxProfitShare.String(),
		MaxCopiersPerLeader: s.maxCopiersPerLeader, MaxCopiesPerFollower: s.maxCopiesPerFollower,
		SettleIntervalHours: s.settleEvery.Hours(), DefaultSettings: DefaultSettings(s.minAllocation)})
	return nil
}

func me(r *http.Request) uuid.UUID { return auth.MustFromContext(r.Context()).UserID }

func pageParams(r *http.Request) (pageCursor, int, error) {
	pp := httpx.ParsePage(r)
	var c pageCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return c, 0, err
	}
	return c, pp.Limit, nil
}

// ---- leaders ----

func (s *Service) leadersHandler(w http.ResponseWriter, r *http.Request) error {
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "", "return_90d", "return_30d", "copiers", "aum":
	default:
		return httpx.BadRequest(`sort must be return_90d, return_30d, copiers or aum.`)
	}
	cards, err := s.Leaders(r.Context(), me(r), sortBy)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": cards})
	return nil
}

func (s *Service) leaderHandler(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		return httpx.NotFound("No such trader.")
	}
	c, ok, err := s.Leader(r.Context(), me(r), id)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.NotFound("No such trader.")
	}
	httpx.OK(w, c)
	return nil
}

func (s *Service) leaderStatusHandler(w http.ResponseWriter, r *http.Request) error {
	st, err := s.LeaderStatus(r.Context(), me(r))
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

func (s *Service) putLeaderHandler(w http.ResponseWriter, r *http.Request) error {
	cur, err := s.leaderRec(r.Context(), me(r))
	if err != nil {
		return err
	}
	in := cur.settings() // absent fields keep their values
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	st, err := s.PutLeader(r.Context(), me(r), in)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

func (s *Service) copiersHandler(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageParams(r)
	if err != nil {
		return err
	}
	list, err := s.Copiers(r.Context(), me(r), c, limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, limit, func(x Copier) any { return pageCursor{T: x.Since, ID: x.id} }))
	return nil
}

// ---- copies ----

func (s *Service) copiesHandler(w http.ResponseWriter, r *http.Request) error {
	rt, err := s.running()
	if err != nil {
		return err
	}
	c, limit, err := pageParams(r)
	if err != nil {
		return err
	}
	where, args := keyset(`WHERE follower_id = $1`, []any{me(r)}, c, "created_at", "id")
	switch st := r.URL.Query().Get("status"); st {
	case "":
	case "open":
		where += ` AND status <> 'stopped'`
	case StatusStopped:
		where += ` AND status = 'stopped'`
	default:
		return httpx.BadRequest("status must be open or stopped.")
	}
	args = append(args, limit+1)
	recs, err := listCopies(r.Context(), s.d.Pool, where+` ORDER BY created_at DESC, id DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(recs))
	for i, rec := range recs {
		if cs, ok := rt.state(rec.ID); ok { // memory is fresher than the row
			cs.mu.Lock()
			recs[i] = cs.rec.clone()
			cs.mu.Unlock()
		}
		if rec.AccountClosedAt == nil {
			ids = append(ids, rec.AccountID)
		}
	}
	sums, err := rt.ex.Summaries(ids)
	if err != nil {
		return err
	}
	views := make([]Copy, 0, len(recs))
	for _, rec := range recs {
		var sum *engine.AccountSummary
		if x, ok := sums[rec.AccountID]; ok && rec.AccountClosedAt == nil {
			sum = &x
		}
		v, err := rt.view(r.Context(), rec, sum)
		if err != nil {
			return err
		}
		views = append(views, v)
	}
	httpx.OK(w, httpx.NewPage(views, limit, func(v Copy) any { return pageCursor{T: v.CreatedAt, ID: v.ID} }))
	return nil
}

func (s *Service) startHandler(w http.ResponseWriter, r *http.Request) error {
	var in StartInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	rt, err := s.running()
	if err != nil {
		return err
	}
	c, err := rt.StartCopy(r.Context(), me(r), in)
	if err != nil {
		return apiErr(err)
	}
	httpx.OK(w, c)
	return nil
}

func (s *Service) mine(r *http.Request) (*runtime, *copyState, error) {
	rt, err := s.running()
	if err != nil {
		return nil, nil, err
	}
	cs, err := rt.copyFor(r.Context(), me(r), chi.URLParam(r, "id"))
	return rt, cs, err
}

func (s *Service) respondCopy(w http.ResponseWriter, r *http.Request, rt *runtime, cs *copyState) error {
	v, err := rt.viewState(r.Context(), cs)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

func (s *Service) copyHandler(w http.ResponseWriter, r *http.Request) error {
	rt, cs, err := s.mine(r)
	if err != nil {
		return err
	}
	return s.respondCopy(w, r, rt, cs)
}

func (s *Service) patchHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Settings *SettingsPatch `json:"settings"`
		Status   string         `json:"status"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	rt, cs, err := s.mine(r)
	if err != nil {
		return err
	}
	if err := rt.Patch(r.Context(), cs, body.Settings, body.Status); err != nil {
		return apiErr(err)
	}
	return s.respondCopy(w, r, rt, cs)
}

func (s *Service) fundsHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Amount string `json:"amount"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(body.Amount))
	if err != nil {
		return httpx.BadRequest("amount must be a decimal string (positive adds, negative withdraws).")
	}
	rt, cs, err := s.mine(r)
	if err != nil {
		return err
	}
	if err := rt.Funds(r.Context(), cs, amt); err != nil {
		return apiErr(err)
	}
	return s.respondCopy(w, r, rt, cs)
}

func (s *Service) stopHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		ClosePositions *bool `json:"close_positions"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	rt, cs, err := s.mine(r)
	if err != nil {
		return err
	}
	closeIt := body.ClosePositions == nil || *body.ClosePositions
	if err := rt.Stop(r.Context(), cs, StopUser, closeIt); err != nil {
		return apiErr(err)
	}
	return s.respondCopy(w, r, rt, cs)
}

func (s *Service) closeHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Sz string `json:"sz"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	rt, cs, err := s.mine(r)
	if err != nil {
		return err
	}
	ov, fills, err := rt.ClosePosition(r.Context(), cs, strings.ToUpper(chi.URLParam(r, "symbol")), strings.TrimSpace(body.Sz))
	if err != nil {
		return err
	}
	if fills == nil {
		fills = []engine.Fill{}
	}
	httpx.OK(w, map[string]any{"order": ov, "fills": fills})
	return nil
}

func (s *Service) eventsHandler(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageParams(r)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errNotFound
	}
	var follower uuid.UUID
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT follower_id FROM copytrade_copies WHERE id = $1`, id).Scan(&follower); err != nil || follower != me(r) {
		return errNotFound // events are read from the database: any instance
	}
	evs, err := listEvents(r.Context(), s.d.Pool, id, c, limit+1)
	if err != nil {
		return err
	}
	views := make([]Event, len(evs))
	for i, e := range evs {
		views[i] = eventView(e)
	}
	httpx.OK(w, httpx.NewPage(views, limit, func(v Event) any { return pageCursor{T: v.CreatedAt, ID: v.ID} }))
	return nil
}

// ---- admin ----

// Overview is GET /admin/copytrade/overview.
type Overview struct {
	Paused          bool   `json:"paused"`
	Running         bool   `json:"running"` // this instance runs the pipeline
	Active          int    `json:"active"`
	PausedCopies    int    `json:"paused_copies"`
	Stopping        int    `json:"stopping"`
	Stopped         int    `json:"stopped"`
	Leaders         int    `json:"leaders"`
	Followers       int    `json:"followers"`
	Aum             string `json:"aum"`
	ProfitSharePaid string `json:"profit_share_paid"`
	Orders24h       int    `json:"orders_24h"`
	Skips24h        int    `json:"skips_24h"`
	FillsDropped    int64  `json:"fills_dropped"`
}

func (s *Service) adminOverview(w http.ResponseWriter, r *http.Request) error {
	o := Overview{Paused: s.paused}
	if rt := s.rt.Load(); rt != nil {
		o.Running = true
		o.FillsDropped = rt.drops.Load() + rt.ex.FillsDropped()
	}
	var aum, paid string
	err := s.d.Pool.QueryRow(r.Context(), `SELECT
		  count(*) FILTER (WHERE status = 'active'), count(*) FILTER (WHERE status = 'paused'),
		  count(*) FILTER (WHERE status = 'stopping'), count(*) FILTER (WHERE status = 'stopped'),
		  (SELECT count(*) FROM copytrade_leaders WHERE enabled),
		  count(DISTINCT follower_id) FILTER (WHERE status <> 'stopped'),
		  COALESCE(sum(equity) FILTER (WHERE status <> 'stopped'), 0)::text, COALESCE(sum(profit_share_paid), 0)::text,
		  (SELECT count(*) FROM copytrade_events WHERE kind IN ('order', 'reconcile') AND created_at > now() - interval '24 hours'),
		  (SELECT count(*) FROM copytrade_events WHERE kind = 'skip' AND created_at > now() - interval '24 hours')
		FROM copytrade_copies`).Scan(&o.Active, &o.PausedCopies, &o.Stopping, &o.Stopped, &o.Leaders, &o.Followers, &aum, &paid,
		&o.Orders24h, &o.Skips24h)
	if err != nil {
		return err
	}
	o.Aum, o.ProfitSharePaid = s.fixed(dec(aum)), s.fixed(dec(paid))
	httpx.OK(w, o)
	return nil
}

// AdminCopy is one row of GET /admin/copytrade/copies.
type AdminCopy struct {
	ID         string     `json:"id"`
	Follower   Person     `json:"follower"`
	Leader     Person     `json:"leader"`
	Status     string     `json:"status"`
	StopReason string     `json:"stop_reason"`
	Allocation string     `json:"allocation"`
	Equity     string     `json:"equity"`
	CreatedAt  time.Time  `json:"created_at"`
	StoppedAt  *time.Time `json:"stopped_at"`
}

func (s *Service) adminCopies(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageParams(r)
	if err != nil {
		return err
	}
	where, args := keyset(`WHERE true`, nil, c, "c.created_at", "c.id")
	switch st := r.URL.Query().Get("status"); st {
	case "":
	case StatusActive, StatusPaused, StatusStopping, StatusStopped:
		args = append(args, st)
		where += ` AND c.status = $` + itoa(len(args))
	default:
		return httpx.BadRequest("status must be active, paused, stopping or stopped.")
	}
	args = append(args, limit+1)
	rows, err := s.d.Pool.Query(r.Context(), `SELECT c.id::text, f.id::text, f.display_name, f.avatar_url, l.id::text, l.display_name,
		  l.avatar_url, c.status, c.stop_reason, c.net_deposits::text, c.equity::text, c.created_at, c.stopped_at
		FROM copytrade_copies c JOIN users f ON f.id = c.follower_id JOIN users l ON l.id = c.leader_id `+where+`
		ORDER BY c.created_at DESC, c.id DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []AdminCopy{}
	for rows.Next() {
		var x AdminCopy
		var nd, eq string
		if err := rows.Scan(&x.ID, &x.Follower.ID, &x.Follower.DisplayName, &x.Follower.AvatarURL, &x.Leader.ID, &x.Leader.DisplayName,
			&x.Leader.AvatarURL, &x.Status, &x.StopReason, &nd, &eq, &x.CreatedAt, &x.StoppedAt); err != nil {
			return err
		}
		x.Allocation, x.Equity = s.fixed(dec(nd)), s.fixed(dec(eq))
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(out, limit, func(x AdminCopy) any { return pageCursor{T: x.CreatedAt, ID: x.ID} }))
	return nil
}
