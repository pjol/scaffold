package agents

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminStats is GET /v1/admin/agents/stats.
type AdminStats struct {
	PausedUsers      int            `json:"paused_users"`       // turned off by the user
	StaffPausedUsers int            `json:"staff_paused_users"` // turned off by staff
	PendingNow       int            `json:"pending_now"`
	ActiveAgents7d   int            `json:"active_agents_7d"` // agent × user pairs seen in 7 days
	Suggestions7d    map[string]int `json:"suggestions_7d"`   // by status, created in 7 days
}

func (s *Service) adminStats(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	out := AdminStats{Suggestions7d: map[string]int{StatusPending: 0, StatusAccepted: 0, StatusRejected: 0, StatusFailed: 0, StatusExpired: 0}}
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM agents_settings WHERE paused),
		       (SELECT count(*) FROM agents_settings WHERE staff_paused_by IS NOT NULL),
		       (SELECT count(*) FROM agents_suggestions WHERE status IN ('pending', 'accepting') AND expires_at > now()),
		       (SELECT count(*) FROM agents_clients WHERE last_seen_at > now() - interval '7 days')`).
		Scan(&out.PausedUsers, &out.StaffPausedUsers, &out.PendingNow, &out.ActiveAgents7d); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT CASE WHEN status = 'pending' AND expires_at <= now() THEN 'expired'
		            WHEN status = 'accepting' THEN 'pending' ELSE status END, count(*)
		FROM agents_suggestions WHERE created_at > now() - interval '7 days' GROUP BY 1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return err
		}
		out.Suggestions7d[st] += n
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// AdminUserScope is one person's agent access as staff see it: the switches,
// how many settings, the agents. Not the items or suggestion contents.
type AdminUserScope struct {
	UserID            string      `json:"user_id"`
	Email             string      `json:"email"`
	DisplayName       string      `json:"display_name"`
	DefaultLevel      Level       `json:"default_level"`
	Paused            bool        `json:"paused"`
	StaffPaused       bool        `json:"staff_paused"`
	StaffPausedAt     *time.Time  `json:"staff_paused_at"`
	FeatureOverrides  int         `json:"feature_overrides"`
	ResourceOverrides int         `json:"resource_overrides"`
	Pending           int         `json:"pending"`
	Agents            []AgentInfo `json:"agents"`
}

func (s *Service) adminUserScope(r *http.Request, q db.Querier) (*AdminUserScope, error) {
	userID, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return nil, errNotFound("User")
	}
	ctx := r.Context()
	out := AdminUserScope{UserID: userID.String(), DefaultLevel: LevelRead}
	var def *Level
	var paused, staff *bool
	err = q.QueryRow(ctx, `
		SELECT u.email, u.display_name, st.default_level, st.paused, st.staff_paused_by IS NOT NULL, st.staff_paused_at,
		       (SELECT count(*) FROM agents_feature_scopes WHERE user_id = u.id),
		       (SELECT count(*) FROM agents_resource_scopes WHERE user_id = u.id),
		       (SELECT count(*) FROM agents_suggestions WHERE user_id = u.id AND status IN ('pending', 'accepting') AND expires_at > now())
		FROM users u LEFT JOIN agents_settings st ON st.user_id = u.id
		WHERE u.id = $1 AND u.status <> 'deleted'`, userID).
		Scan(&out.Email, &out.DisplayName, &def, &paused, &staff, &out.StaffPausedAt, &out.FeatureOverrides, &out.ResourceOverrides, &out.Pending)
	if db.IsNoRows(err) {
		return nil, errNotFound("User")
	}
	if err != nil {
		return nil, err
	}
	if def != nil {
		out.DefaultLevel = *def
	}
	out.Paused = paused != nil && *paused
	out.StaffPaused = staff != nil && *staff
	out.Agents, err = s.agentsSeen(ctx, q, userID)
	return &out, err
}

func (s *Service) adminUser(w http.ResponseWriter, r *http.Request) error {
	out, err := s.adminUserScope(r, s.d.Pool)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// adminSetPaused turns a person's agent access off or back on as staff
// (audited). A staff pause stops every agent call at once and the person
// can't lift it; their own switch and settings are left as they were.
func (s *Service) adminSetPaused(w http.ResponseWriter, r *http.Request) error {
	admin := auth.MustFromContext(r.Context())
	var in struct {
		Paused *bool  `json:"paused"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Paused == nil {
		return httpx.BadRequest("Send paused: true or false.")
	}
	reason := truncate(in.Reason, 500)
	ctx := r.Context()
	var out *AdminUserScope
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		before, err := s.adminUserScope(r, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO agents_settings (user_id, staff_paused_by, staff_paused_at)
			VALUES ($1, CASE WHEN $2 THEN $3::uuid END, CASE WHEN $2 THEN now() END)
			ON CONFLICT (user_id) DO UPDATE SET
			  staff_paused_by = CASE WHEN $2 THEN $3::uuid END,
			  staff_paused_at = CASE WHEN $2 THEN now() END, updated_at = now()`,
			before.UserID, *in.Paused, admin.UserID); err != nil {
			return err
		}
		action := "agents.resume"
		if *in.Paused {
			action = "agents.pause"
		}
		if err := audit.Log(ctx, tx, admin.UserID, action, "user", before.UserID,
			map[string]any{"before": before.StaffPaused, "after": *in.Paused, "reason": reason}); err != nil {
			return err
		}
		out, err = s.adminUserScope(r, tx)
		return err
	})
	if err != nil {
		return err
	}
	if uid, err := httpx.URLParamUUID(r, "userID"); err == nil {
		s.scopesChanged(ctx, uid)
	}
	httpx.OK(w, out)
	return nil
}
