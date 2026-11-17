package copytrade

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes copy trading to agents, run as the caller: the caller's
// copies and the leader directory to read, and stopping a copy as the one
// write (previewed, with the copy as Target). Agents never start copies,
// change allocations or move funds: those are the person's decisions.
func (s *Service) Tools() []app.Tool {
	copyID := json.RawMessage(`{
		"type": "object",
		"properties": {"copy_id": {"type": "string", "format": "uuid"}},
		"required": ["copy_id"],
		"additionalProperties": false
	}`)
	return []app.Tool{
		{
			Name:        "copytrade_list_copies",
			Description: "List the user's copy trades (newest first): the trader copied, status, allocation, equity, profit and loss.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {"open_only": {"type": "boolean"}}, "additionalProperties": false}`),
			ReadOnly:    true,
			Handler:     s.toolList,
		},
		{
			Name:        "copytrade_get_copy",
			Description: "One of the user's copy trades: settings, positions, skipped orders and the 20 latest events (orders, skips, transfers, stops).",
			InputSchema: copyID,
			ReadOnly:    true,
			Handler:     s.toolGet,
		},
		{
			Name:        "copytrade_list_leaders",
			Description: "Traders who accept copiers, with time-weighted 30 and 90 day returns, 90 day max drawdown, copiers, assets copying them and profit share. Past results don't predict future ones.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"sort": {"type": "string", "enum": ["return_90d", "return_30d", "copiers", "aum"]}},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolLeaders,
		},
		{
			Name:        "copytrade_stop_copy",
			Description: "Stop one of the user's copy trades. close_positions (default true) closes its positions at market; either way free funds return to the user's main account.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"copy_id": {"type": "string", "format": "uuid"}, "close_positions": {"type": "boolean"}},
				"required": ["copy_id"],
				"additionalProperties": false
			}`),
			Handler: s.toolStop,
			Target: func(raw json.RawMessage) (string, string) {
				var in struct {
					CopyID string `json:"copy_id"`
				}
				_ = json.Unmarshal(raw, &in)
				return "copytrade.copy", in.CopyID
			},
			Preview: s.previewStop,
		},
	}
}

type toolCopy struct {
	ID         string `json:"id"`
	Leader     string `json:"leader"`
	Status     string `json:"status"`
	Allocation string `json:"allocation"`
	Equity     string `json:"equity"`
	Pnl        string `json:"pnl"`
	PnlPct     string `json:"pnl_pct"`
	Link       string `json:"link"`
}

func (s *Service) toolList(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		OpenOnly bool `json:"open_only"`
	}
	_ = json.Unmarshal(raw, &in)
	rt, err := s.running()
	if err != nil {
		return nil, err
	}
	where := `WHERE follower_id = $1`
	if in.OpenOnly {
		where += ` AND status <> 'stopped'`
	}
	recs, err := listCopies(ctx, s.d.Pool, where+` ORDER BY created_at DESC LIMIT 50`, caller.UserID)
	if err != nil {
		return nil, err
	}
	out := []toolCopy{}
	for _, rec := range recs {
		if cs, ok := rt.state(rec.ID); ok {
			cs.mu.Lock()
			rec = cs.rec.clone()
			cs.mu.Unlock()
		}
		v, err := rt.view(ctx, rec, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, toolCopy{ID: v.ID, Leader: v.Leader.DisplayName, Status: v.Status, Allocation: v.Allocation, Equity: v.Equity,
			Pnl: v.Pnl, PnlPct: v.PnlPct, Link: "/copy/" + v.ID})
	}
	return map[string]any{"copies": out}, nil
}

func (s *Service) toolCopy(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (*runtime, *copyState, error) {
	var in struct {
		CopyID string `json:"copy_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, nil, httpx.BadRequest("copy_id is required.")
	}
	rt, err := s.running()
	if err != nil {
		return nil, nil, err
	}
	cs, err := rt.copyFor(ctx, caller.UserID, in.CopyID)
	return rt, cs, err
}

func (s *Service) toolGet(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	rt, cs, err := s.toolCopy(ctx, caller, raw)
	if err != nil {
		return nil, err
	}
	v, err := rt.viewState(ctx, cs)
	if err != nil {
		return nil, err
	}
	evs, err := listEvents(ctx, s.d.Pool, cs.rec.ID, pageCursor{}, 20)
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(evs))
	for i, e := range evs {
		events[i] = eventView(e)
	}
	return map[string]any{"copy": v, "events": events, "link": "/copy/" + v.ID}, nil
}

func (s *Service) toolLeaders(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Sort string `json:"sort"`
	}
	_ = json.Unmarshal(raw, &in)
	switch in.Sort {
	case "", "return_90d", "return_30d", "copiers", "aum":
	default:
		return nil, httpx.BadRequest("sort must be return_90d, return_30d, copiers or aum.")
	}
	cards, err := s.Leaders(ctx, caller.UserID, in.Sort)
	if err != nil {
		return nil, err
	}
	if len(cards) > 25 {
		cards = cards[:25]
	}
	return map[string]any{"leaders": cards, "note": "Past results don't predict future ones.", "link": "/leaders"}, nil
}

func (s *Service) toolStop(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		ClosePositions *bool `json:"close_positions"`
	}
	_ = json.Unmarshal(raw, &in)
	rt, cs, err := s.toolCopy(ctx, caller, raw)
	if err != nil {
		return nil, err
	}
	closeIt := in.ClosePositions == nil || *in.ClosePositions
	if err := rt.Stop(ctx, cs, StopUser, closeIt); err != nil {
		return nil, apiErr(err)
	}
	return rt.viewState(ctx, cs)
}

func (s *Service) previewStop(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var in struct {
		CopyID         string `json:"copy_id"`
		ClosePositions *bool  `json:"close_positions"`
	}
	_ = json.Unmarshal(raw, &in)
	id, err := uuid.Parse(in.CopyID)
	if err != nil {
		return app.ToolPreview{}, errNotFound
	}
	rec, ok, err := getCopy(ctx, s.d.Pool, id)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if !ok || rec.FollowerID != caller.UserID {
		return app.ToolPreview{}, errNotFound
	}
	what := "close its positions at market"
	if in.ClosePositions != nil && !*in.ClosePositions {
		what = "keep its positions"
	}
	leader := person(ctx, s.d.Pool, rec.LeaderID).DisplayName
	return app.ToolPreview{Summary: fmt.Sprintf("Stop copying %s, %s and return free funds", leader, what),
		Before: "Status: " + rec.Status, After: "Status: stopped", Link: "/copy/" + rec.ID.String()}, nil
}
