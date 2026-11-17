package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// PublicView is what anyone signed in sees of a person: score, tier, status.
type PublicView struct {
	UserID string  `json:"user_id"`
	Score  *int    `json:"score"`
	Status string  `json:"status"`
	Tier   *string `json:"tier"`
}

func publicOf(id uuid.UUID, s Summary) PublicView {
	v := PublicView{UserID: id.String(), Score: s.Score, Status: s.Status}
	if s.Tier != "" {
		t := s.Tier
		v.Tier = &t
	}
	return v
}

// Person is a user as the admin flag queue shows them.
type Person struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
	Status      string  `json:"status"`
}

// FlagView is one integrity flag in Admin → Reputation.
type FlagView struct {
	ID            string            `json:"id"`
	User          Person            `json:"user"`
	Kind          string            `json:"kind"`
	Counterparty  *Person           `json:"counterparty"`
	Metrics       map[string]string `json:"metrics"`
	Status        string            `json:"status"`
	Note          string            `json:"note"`
	CreatedAt     time.Time         `json:"created_at"`
	DecidedAt     *time.Time        `json:"decided_at"`
	DecidedByName *string           `json:"decided_by_name"`
}

func (s *Service) meHandler(w http.ResponseWriter, r *http.Request) error {
	rep, err := s.full(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if errors.Is(err, errNoPerson) {
		return httpx.NotFound("No reputation for this account.")
	}
	if err != nil {
		return err
	}
	httpx.OK(w, rep)
	return nil
}

func (s *Service) exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND status <> 'deleted')`, id).Scan(&ok)
	return ok, err
}

func (s *Service) personHandler(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	ok, err := s.exists(r.Context(), id)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.NotFound("No such person.")
	}
	sum, err := Get(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, publicOf(id, sum))
	return nil
}

// peopleHandler: GET /reputation/people?ids=a,b,… (≤ 100), for lists.
// Unknown and deleted people are left out.
func (s *Service) peopleHandler(w http.ResponseWriter, r *http.Request) error {
	var ids []uuid.UUID
	for _, raw := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.BadRequest("ids must be comma-separated person ids.")
		}
		ids = append(ids, id)
	}
	if len(ids) > 100 {
		return httpx.BadRequest("Ask for 100 people at most.")
	}
	items := []PublicView{}
	if len(ids) > 0 {
		rows, err := s.d.Pool.Query(r.Context(), `SELECT u.id, COALESCE(s.status, 'unrated'), s.score, s.tier
			FROM users u LEFT JOIN reputation_scores s ON s.user_id = u.id
			WHERE u.id = ANY($1) AND u.status <> 'deleted'`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var sum Summary
			var tier *string
			if err := rows.Scan(&id, &sum.Status, &sum.Score, &tier); err != nil {
				return err
			}
			if sum.Status != StatusRated {
				sum.Score, tier = nil, nil
			}
			if tier != nil {
				sum.Tier = *tier
			}
			items = append(items, publicOf(id, sum))
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

func (s *Service) adminPersonHandler(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	ok, err := s.exists(r.Context(), id)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.NotFound("No such person.")
	}
	rep, err := s.full(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.OK(w, rep)
	return nil
}

type flagCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

const flagColumns = `f.id, f.user_id, u.display_name, u.email, u.avatar_url, u.status, f.kind,
	f.counterparty_id, c.display_name, c.email, c.avatar_url, c.status, f.metrics, f.status, f.note,
	f.created_at, f.decided_at, NULLIF(d.display_name, '')`

const flagFrom = ` FROM reputation_flags f JOIN users u ON u.id = f.user_id
	LEFT JOIN users c ON c.id = f.counterparty_id LEFT JOIN users d ON d.id = f.decided_by`

func scanFlag(row pgx.Row) (FlagView, error) {
	var v FlagView
	var id, uid uuid.UUID
	var cid *uuid.UUID
	var cName, cEmail, cStatus *string
	var cAvatar *string
	var metrics []byte
	if err := row.Scan(&id, &uid, &v.User.DisplayName, &v.User.Email, &v.User.AvatarURL, &v.User.Status, &v.Kind,
		&cid, &cName, &cEmail, &cAvatar, &cStatus, &metrics, &v.Status, &v.Note, &v.CreatedAt, &v.DecidedAt, &v.DecidedByName); err != nil {
		return v, err
	}
	v.ID, v.User.ID = id.String(), uid.String()
	if cid != nil {
		p := Person{ID: cid.String(), AvatarURL: cAvatar}
		if cName != nil {
			p.DisplayName = *cName
		}
		if cEmail != nil {
			p.Email = *cEmail
		}
		if cStatus != nil {
			p.Status = *cStatus
		}
		v.Counterparty = &p
	}
	v.Metrics = map[string]string{}
	_ = json.Unmarshal(metrics, &v.Metrics)
	return v, nil
}

// flagsHandler: GET /admin/reputation/flags?status=open|cleared|confirmed.
// Open flags oldest first (work from the top); decided ones newest decision first.
func (s *Service) flagsHandler(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = FlagOpen
	}
	if status != FlagOpen && status != FlagCleared && status != FlagConfirmed {
		return httpx.BadRequest("status must be open, cleared or confirmed.")
	}
	pp := httpx.ParsePage(r)
	var c flagCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	var q string
	if status == FlagOpen {
		q = `SELECT ` + flagColumns + flagFrom + ` WHERE f.status = $1
			AND ($2::timestamptz IS NULL OR (f.created_at, f.id) > ($2, $3)) ORDER BY f.created_at, f.id LIMIT $4`
	} else {
		q = `SELECT ` + flagColumns + flagFrom + ` WHERE f.status = $1
			AND ($2::timestamptz IS NULL OR (f.decided_at, f.id) < ($2, $3)) ORDER BY f.decided_at DESC, f.id DESC LIMIT $4`
	}
	var after *time.Time
	if pp.Cursor != "" {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(r.Context(), q, status, after, c.ID, pp.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []FlagView
	for rows.Next() {
		v, err := scanFlag(rows)
		if err != nil {
			return err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, pp.Limit, func(v FlagView) any {
		t := v.CreatedAt
		if status != FlagOpen && v.DecidedAt != nil {
			t = *v.DecidedAt
		}
		id, _ := uuid.Parse(v.ID)
		return flagCursor{T: t, ID: id}
	}))
	return nil
}

func (s *Service) flag(ctx context.Context, q db.Querier, id uuid.UUID) (FlagView, error) {
	return scanFlag(q.QueryRow(ctx, `SELECT `+flagColumns+flagFrom+` WHERE f.id = $1`, id))
}

// decideHandler: POST /admin/reputation/flags/{id}/decision {decision, note}.
// clear lifts the review (unless another flag is open); confirm keeps the
// person under review for 90 days. Audited in the same transaction.
func (s *Service) decideHandler(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	status := map[string]string{"clear": FlagCleared, "confirm": FlagConfirmed}[body.Decision]
	if status == "" {
		return httpx.BadRequest("decision must be clear or confirm.")
	}
	note := strings.TrimSpace(body.Note)
	if len([]rune(note)) > 1000 {
		return httpx.BadRequest("Keep the note to 1,000 characters.")
	}
	staff := auth.MustFromContext(r.Context())
	var userID uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var kind string
		err := tx.QueryRow(r.Context(), `UPDATE reputation_flags SET status = $2, note = $3, decided_at = now(), decided_by = $4
			WHERE id = $1 AND status = 'open' RETURNING user_id, kind`, id, status, note, staff.UserID).Scan(&userID, &kind)
		if db.IsNoRows(err) {
			var exists bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM reputation_flags WHERE id = $1)`, id).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return httpx.NotFound("No such flag.")
			}
			return httpx.NewError(http.StatusConflict, "flag_decided", "This flag was already decided. Reload to see the decision.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "reputation.flag_"+status, "reputation_flag", id.String(),
			map[string]any{"user_id": userID, "kind": kind, "note": note})
	})
	if err != nil {
		return err
	}
	// The status follows the decision now; a failure here is caught by the next tick.
	if err := s.Rescore(r.Context(), []uuid.UUID{userID}); err != nil {
		s.log.WarnContext(r.Context(), "reputation: rescore after decision failed", "user", userID, "err", err)
	}
	v, err := s.flag(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}
