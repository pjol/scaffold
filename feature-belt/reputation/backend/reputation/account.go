package reputation

import (
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion and export ----

// DeleteAccount removes the person's snapshots, days and score. Integrity
// flags stay as fraud evidence (the users row no longer identifies anyone).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM reputation_snapshots WHERE user_id = $1`,
		`DELETE FROM reputation_days WHERE user_id = $1`,
		`DELETE FROM reputation_scores WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns the person's reputation, day rows and the flags about
// them (kind and outcome; not the other person or the detector's figures).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := map[string]any{"reputation": nil}
	if r, ok, err := s.load(ctx, userID); err != nil {
		return nil, err
	} else if ok {
		out["reputation"] = r
	}
	type day struct {
		Day       string `json:"day"`
		Open      string `json:"open"`
		Close     string `json:"close"`
		Flows     string `json:"flows"`
		Active    bool   `json:"active"`
		Activity  int    `json:"activity"`
		Volume    string `json:"volume"`
		Incidents int    `json:"incidents"`
		Source    string `json:"source"`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), open_value::text, close_value::text, flows::text, active,
		activity, volume::text, incidents, source FROM reputation_days WHERE user_id = $1 ORDER BY day`, userID)
	if err != nil {
		return nil, err
	}
	days, err := pgx.CollectRows(rows, pgx.RowToStructByPos[day])
	if err != nil {
		return nil, err
	}
	if days == nil {
		days = []day{}
	}
	out["days"] = days
	type flag struct {
		Kind      string     `json:"kind"`
		Status    string     `json:"status"`
		CreatedAt time.Time  `json:"created_at"`
		DecidedAt *time.Time `json:"decided_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT kind, status, created_at, decided_at FROM reputation_flags WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	flags, err := pgx.CollectRows(rows, pgx.RowToStructByPos[flag])
	if err != nil {
		return nil, err
	}
	if flags == nil {
		flags = []flag{}
	}
	out["flags"] = flags
	return out, nil
}

// ---- agent tools ----

// Tools exposes reputation to agents (read-only).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name: "reputation_get",
		Description: "Read the user's reputation: score (0–100), tier, status, each component with its weight and inputs, " +
			"the requirements still to meet and 90-day stats. With user_id, another person's score, tier and status only.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"user_id": {"type": "string", "format": "uuid", "description": "Another person's id; omit for the user's own reputation."}},
			"additionalProperties": false
		}`),
		ReadOnly: true,
		Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
			var in struct {
				UserID string `json:"user_id"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, httpx.BadRequest("Invalid input: " + err.Error())
				}
			}
			if in.UserID == "" || in.UserID == caller.UserID.String() {
				r, err := s.full(ctx, caller.UserID)
				if err != nil {
					return nil, err
				}
				r.Stats.Equity = nil // the curve is for screens
				return r, nil
			}
			id, err := uuid.Parse(in.UserID)
			if err != nil {
				return nil, httpx.BadRequest("user_id must be a person's id.")
			}
			ok, err := s.exists(ctx, id)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, httpx.NotFound("No such person.")
			}
			sum, err := Get(ctx, s.d.Pool, id)
			if err != nil {
				return nil, err
			}
			return publicOf(id, sum), nil
		},
	}}
}

// ---- development data ----

type seedPlan struct {
	days     int     // history length, ending yesterday
	mean, sd float64 // daily return of active days
	activeP  float64 // chance a day is active
	seed     uint64
}

// SeedDev gives the dev identities day series that show every state: the
// admin and Sam rated, Jordan unrated (6 days old), Priya under review (an
// open wash-pair flag with Jordan), and a cleared vote-ring flag on Sam.
// Days end yesterday, so a Source's rollup owns today.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	plans := []seedPlan{
		{days: 70, mean: 0.0006, sd: 0.014, activeP: 0.55, seed: 11}, // admin: steady
		{days: 88, mean: 0.0032, sd: 0.009, activeP: 0.7, seed: 23},  // Sam: strong to elite
		{days: 6, mean: 0.002, sd: 0.01, activeP: 1, seed: 37},       // Jordan: too new
		{days: 60, mean: 0.004, sd: 0.006, activeP: 0.8, seed: 41},   // Priya: would rate, under review
	}
	yesterday := dayStart(time.Now()).AddDate(0, 0, -1)
	for i, p := range plans {
		rng := rand.New(rand.NewPCG(p.seed, 0x5eed))
		value := decimal.NewFromInt(1000)
		for k := p.days - 1; k >= 0; k-- {
			d := Day{Day: yesterday.AddDate(0, 0, -k), Open: value}
			if k == p.days-1 || rng.Float64() < 0.05 {
				d.Flows = decimal.NewFromInt(int64(100 * (1 + rng.IntN(5)))) // a deposit
			}
			active := k == p.days-1 || rng.Float64() < p.activeP
			ret := 0.0
			if active {
				ret = p.mean + p.sd*rng.NormFloat64()
				d.Activity = 1 + rng.IntN(4)
				d.Volume = value.Mul(decimal.NewFromFloat(0.15 + 0.3*rng.Float64())).Mul(decimal.NewFromInt(int64(d.Activity))).Round(2)
				if ret < -2.5*p.sd {
					d.Incidents = 1
				}
			}
			base := decimal.Max(value, s.floor())
			d.Close = value.Add(d.Flows).Add(base.Mul(decimal.NewFromFloat(math.Round(ret*1e6) / 1e6))).Round(2)
			d.Active = active
			if err := RecordDay(ctx, s.d.Pool, users[i], d); err != nil {
				return err
			}
			value = d.Close
		}
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	var flagged bool // once seeded, a decision on the flag stands across re-seeds
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reputation_flags WHERE user_id = $1 AND kind = 'wash_pair')`, priya).Scan(&flagged); err != nil {
		return err
	}
	if !flagged {
		if err := Flag(ctx, s.d.Pool, IntegrityFlag{UserID: priya, Kind: FlagWashPair, CounterpartyID: jordan, Metrics: map[string]string{
			"trades_30d": "14", "pair_volume": "4200.00", "share_of_user_pct": "41.2", "share_of_counterparty_pct": "88.5",
			"gain_user": "212.40", "gain_counterparty": "-198.75", "mirrored": "true",
		}}); err != nil {
			return err
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO reputation_flags (user_id, kind, counterparty_id, metrics, status, note, created_at, decided_at, decided_by)
		SELECT $1, 'vote_ring', $2, '{"upvotes_30d":"24","to_counterparty":"13","share_pct":"54.2"}', 'cleared',
		  'Same team, upvoting each other''s guides. Not a ring.', now() - interval '20 days', now() - interval '19 days', $3
		WHERE NOT EXISTS (SELECT 1 FROM reputation_flags WHERE user_id = $1 AND kind = 'vote_ring')`, sam, priya, admin); err != nil {
		return err
	}
	return s.Rescore(ctx, users)
}
