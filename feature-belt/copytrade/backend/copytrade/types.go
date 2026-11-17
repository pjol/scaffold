package copytrade

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

// DisclosureVersion is the risk disclosure a follower accepts before every
// new copy (DisclosureText). Changing its substance means a new version.
const DisclosureVersion = "copy-v1"

// Copy statuses, stop and pause reasons.
const (
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusStopping = "stopping" // closing positions; the monitor retries until done
	StatusStopped  = "stopped"

	StopUser           = "user"
	StopLoss           = "stop_loss"
	StopDrawdown       = "drawdown"
	StopLeaderDisabled = "leader_disabled"
	StopLeaderLeft     = "leader_left" // the leader deleted their account
	StopStaff          = "staff"
	StopBlocked        = "blocked"        // StopBetween: one of them blocked the other
	StopAccountClosed  = "account_closed" // the copy account was closed outside copy trading

	PauseUser             = "user"
	PauseLeaderIneligible = "leader_ineligible" // lifts by itself when the leader is eligible again
)

// Event kinds and skip classes.
const (
	EventStart       = "start"
	EventOrder       = "order"
	EventSkip        = "skip"
	EventStop        = "stop"
	EventTransfer    = "transfer"
	EventProfitShare = "profit_share"
	EventReconcile   = "reconcile"
	EventPause       = "pause"
	EventResume      = "resume"
	EventSettings    = "settings"

	SkipTooSmall  = "too_small"
	SkipRiskLimit = "risk_limit"
	SkipRejected  = "rejected"
	SkipSlippage  = "slippage"
)

// Person is someone as the app shows them: core display name and avatar.
type Person struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

// Settings are one copy's risk settings (amounts in the quote currency).
type Settings struct {
	MaxLeverage         int     `json:"max_leverage"`
	MaxTradeNotional    string  `json:"max_trade_notional"`
	MaxPositionNotional string  `json:"max_position_notional"`
	StopLossPct         *string `json:"stop_loss_pct"`    // null = none
	MaxDrawdownPct      *string `json:"max_drawdown_pct"` // null = none
	CopyOpenPositions   bool    `json:"copy_open_positions"`
	OnLeaderExit        string  `json:"on_leader_exit"` // close | keep
	SlippageBps         int     `json:"slippage_bps"`
}

// SkipCounts are a copy's skipped orders by class (last 30 days).
type SkipCounts struct {
	TooSmall  int `json:"too_small"`
	RiskLimit int `json:"risk_limit"`
	Rejected  int `json:"rejected"`
	Slippage  int `json:"slippage"`
}

func (s *SkipCounts) add(class string, n int) {
	switch class {
	case SkipTooSmall:
		s.TooSmall += n
	case SkipRiskLimit:
		s.RiskLimit += n
	case SkipRejected:
		s.Rejected += n
	case SkipSlippage:
		s.Slippage += n
	}
}

// Copy is one copy as its follower sees it.
type Copy struct {
	ID              string                 `json:"id"`
	Leader          Person                 `json:"leader"`
	Status          string                 `json:"status"`
	StopReason      string                 `json:"stop_reason"`
	PauseReason     string                 `json:"pause_reason"`
	Allocation      string                 `json:"allocation"` // net amount moved in
	Equity          string                 `json:"equity"`
	Pnl             string                 `json:"pnl"`
	PnlPct          string                 `json:"pnl_pct"` // of everything moved in
	PeakEquity      string                 `json:"peak_equity"`
	Ratio           string                 `json:"ratio"` // copy equity ÷ leader equity
	Settings        Settings               `json:"settings"`
	Account         *engine.AccountSummary `json:"account"` // the copy's sub-account; null once closed
	ProfitSharePct  string                 `json:"profit_share_pct"`
	ProfitSharePaid string                 `json:"profit_share_paid"`
	Skipped30d      SkipCounts             `json:"skipped_30d"`
	IdentityVisible bool                   `json:"identity_visible"` // the leader sees the follower's name
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	StoppedAt       *time.Time             `json:"stopped_at"`
}

// Event is one entry of a copy's activity log.
type Event struct {
	ID        string    `json:"id"`
	CopyID    string    `json:"copy_id"`
	Kind      string    `json:"kind"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Sz        *string   `json:"sz"`
	Notional  *string   `json:"notional"`
	OrderID   *string   `json:"order_id"`
	SkipClass string    `json:"skip_class"`
	Reason    string    `json:"reason"`
	LatencyMs *int64    `json:"latency_ms"`
	CreatedAt time.Time `json:"created_at"`
}

// LeaderSettings are what a leader offers.
type LeaderSettings struct {
	Enabled        bool   `json:"enabled"`
	ProfitSharePct string `json:"profit_share_pct"`
	MinAllocation  string `json:"min_allocation"`
	MaxCopiers     int    `json:"max_copiers"`
	ShowCopiers    bool   `json:"show_copiers"` // copiers who start while this is on let the leader see their name
}

// Requirement is one leader eligibility rule and whether it is met.
type Requirement struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Met      bool   `json:"met"`
	Progress string `json:"progress"`
}

// LeaderStatus is the caller's own leading.
type LeaderStatus struct {
	Settings          LeaderSettings `json:"settings"`
	Eligible          bool           `json:"eligible"`
	Requirements      []Requirement  `json:"requirements"`
	Copiers           int            `json:"copiers"`
	Aum               string         `json:"aum"`
	ProfitShareEarned string         `json:"profit_share_earned"`
}

// LeaderCard is one leader in the directory. Returns are time-weighted from
// daily snapshots of the leader's main account (deposits and withdrawals
// don't count); null until there are two snapshots.
type LeaderCard struct {
	User              Person    `json:"user"`
	Accepting         bool      `json:"accepting"`
	Return30dPct      *string   `json:"return_30d_pct"`
	Return90dPct      *string   `json:"return_90d_pct"`
	MaxDrawdown90dPct *string   `json:"max_drawdown_90d_pct"`
	HistoryDays       int       `json:"history_days"`
	Copiers           int       `json:"copiers"`
	Aum               string    `json:"aum"`
	ProfitSharePct    string    `json:"profit_share_pct"`
	MinAllocation     string    `json:"min_allocation"`
	ShowCopiers       bool      `json:"show_copiers"`
	Copying           bool      `json:"copying"` // the caller copies this leader now
	IsMe              bool      `json:"is_me"`
	LeadingSince      time.Time `json:"leading_since"`
}

// Copier is one row of the leader's copier list: allocation as a bucket, the
// person only when they started while the leader showed copiers.
type Copier struct {
	id         string
	User       *Person   `json:"user"`
	Allocation string    `json:"allocation"` // lt_100 | 100_1k | 1k_10k | gt_10k (quote currency)
	Since      time.Time `json:"since"`
}

// ---- settings: defaults and validation ----

var (
	d10    = decimal.NewFromInt(10)
	d100   = decimal.NewFromInt(100)
	d50k   = decimal.NewFromInt(50_000)
	d1m    = decimal.NewFromInt(1_000_000)
	pctMin = decimal.NewFromInt(5)
	pctMax = decimal.NewFromInt(90)
)

// DefaultSettings for an allocation: 3× leverage cap, 500 per trade, 3 ×
// the allocation per market, a 30 % stop loss, no drawdown stop, new
// positions only, close on leader exit, 100 bps slippage.
func DefaultSettings(allocation decimal.Decimal) Settings {
	sl := "30"
	pos := allocation.Mul(decimal.NewFromInt(3)).Round(2)
	if pos.LessThan(d10) {
		pos = d10
	}
	return Settings{MaxLeverage: 3, MaxTradeNotional: "500", MaxPositionNotional: pos.String(),
		StopLossPct: &sl, OnLeaderExit: "close", SlippageBps: 100}
}

// opt is a JSON field that tells absent (set=false) from null.
type opt[T any] struct {
	set, null bool
	v         T
}

func (o *opt[T]) UnmarshalJSON(b []byte) error {
	o.set = true
	if strings.TrimSpace(string(b)) == "null" {
		o.null = true
		return nil
	}
	return json.Unmarshal(b, &o.v)
}

// SettingsPatch is a partial Settings: absent fields keep their value.
type SettingsPatch struct {
	MaxLeverage         opt[int]    `json:"max_leverage"`
	MaxTradeNotional    opt[string] `json:"max_trade_notional"`
	MaxPositionNotional opt[string] `json:"max_position_notional"`
	StopLossPct         opt[string] `json:"stop_loss_pct"`
	MaxDrawdownPct      opt[string] `json:"max_drawdown_pct"`
	CopyOpenPositions   opt[bool]   `json:"copy_open_positions"`
	OnLeaderExit        opt[string] `json:"on_leader_exit"`
	SlippageBps         opt[int]    `json:"slippage_bps"`
}

func invalid(format string, a ...any) error {
	return &engine.Error{Status: 400, Code: "invalid_request", Message: fmt.Sprintf(format, a...)}
}

func decIn(s, field string, lo, hi decimal.Decimal) (decimal.Decimal, error) {
	v, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return v, invalid("%s must be a decimal string.", field)
	}
	if v.LessThan(lo) || v.GreaterThan(hi) {
		return v, invalid("%s must be %s–%s.", field, lo, hi)
	}
	return v, nil
}

// Apply merges p onto base and validates the result.
func (p SettingsPatch) Apply(base Settings) (Settings, error) {
	s := base
	if p.MaxLeverage.set {
		s.MaxLeverage = p.MaxLeverage.v
	}
	if p.MaxTradeNotional.set && !p.MaxTradeNotional.null {
		s.MaxTradeNotional = p.MaxTradeNotional.v
	}
	if p.MaxPositionNotional.set && !p.MaxPositionNotional.null {
		s.MaxPositionNotional = p.MaxPositionNotional.v
	}
	if p.StopLossPct.set {
		s.StopLossPct = nil
		if !p.StopLossPct.null {
			v := p.StopLossPct.v
			s.StopLossPct = &v
		}
	}
	if p.MaxDrawdownPct.set {
		s.MaxDrawdownPct = nil
		if !p.MaxDrawdownPct.null {
			v := p.MaxDrawdownPct.v
			s.MaxDrawdownPct = &v
		}
	}
	if p.CopyOpenPositions.set {
		s.CopyOpenPositions = p.CopyOpenPositions.v
	}
	if p.OnLeaderExit.set && !p.OnLeaderExit.null {
		s.OnLeaderExit = p.OnLeaderExit.v
	}
	if p.SlippageBps.set {
		s.SlippageBps = p.SlippageBps.v
	}
	return s, s.validate()
}

func (s *Settings) validate() error {
	if s.MaxLeverage < 1 || s.MaxLeverage > 10 {
		return invalid("max_leverage must be 1–10.")
	}
	v, err := decIn(s.MaxTradeNotional, "max_trade_notional", d10, d50k)
	if err != nil {
		return err
	}
	s.MaxTradeNotional = v.String()
	if v, err = decIn(s.MaxPositionNotional, "max_position_notional", d10, d1m); err != nil {
		return err
	}
	s.MaxPositionNotional = v.String()
	for _, f := range []struct {
		p    **string
		name string
	}{{&s.StopLossPct, "stop_loss_pct"}, {&s.MaxDrawdownPct, "max_drawdown_pct"}} {
		if *f.p == nil {
			continue
		}
		v, err := decIn(**f.p, f.name, pctMin, pctMax)
		if err != nil {
			return err
		}
		str := v.String()
		*f.p = &str
	}
	if s.OnLeaderExit != "close" && s.OnLeaderExit != "keep" {
		return invalid(`on_leader_exit must be "close" or "keep".`)
	}
	if s.SlippageBps < 10 || s.SlippageBps > 500 {
		return invalid("slippage_bps must be 10–500.")
	}
	return nil
}

func (s Settings) dec(v string) decimal.Decimal {
	d, _ := decimal.NewFromString(v)
	return d
}

func (s Settings) closeOnExit() bool { return s.OnLeaderExit != "keep" }

func pctOf(p *string) (decimal.Decimal, bool) {
	if p == nil {
		return decimal.Zero, false
	}
	v, err := decimal.NewFromString(*p)
	if err != nil || !v.IsPositive() {
		return decimal.Zero, false
	}
	return v.Div(d100), true
}

// allocationBucket hides a copier's exact allocation from the leader.
func allocationBucket(v decimal.Decimal) string {
	switch {
	case v.LessThan(d100):
		return "lt_100"
	case v.LessThan(decimal.NewFromInt(1000)):
		return "100_1k"
	case v.LessThan(decimal.NewFromInt(10_000)):
		return "1k_10k"
	}
	return "gt_10k"
}
