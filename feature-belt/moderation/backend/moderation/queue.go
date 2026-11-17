package moderation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// SLA is the response target for an open case (Apple 1.2 expects reports
// to be acted on within 24 hours).
const SLA = 24 * time.Hour

// EventCaseClosed is published on admin:live when staff close a case.
const EventCaseClosed = "moderation.case_closed"

// CaseUser is a person on a case as staff see them, read live.
type CaseUser struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Status      string    `json:"status"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Case is one reported target in the staff queue.
type Case struct {
	ID             uuid.UUID      `json:"id"`
	TargetKind     string         `json:"target_kind"`
	TargetID       string         `json:"target_id"`
	Status         string         `json:"status"` // open | resolved | dismissed
	Source         string         `json:"source"` // report | auto (opened by screening)
	Categories     []string       `json:"categories"`
	ReportCount    int            `json:"report_count"`
	Reasons        map[string]int `json:"reasons"`
	Snapshot       Snapshot       `json:"snapshot"`
	MediaURLs      []string       `json:"media_urls"`
	Author         *CaseUser      `json:"author"`
	Hidden         bool           `json:"hidden"`
	AutoHidden     bool           `json:"auto_hidden"`
	Action         *string        `json:"action"`
	ResolutionNote string         `json:"resolution_note"`
	ResolvedBy     *uuid.UUID     `json:"resolved_by"`
	ResolvedAt     *time.Time     `json:"resolved_at"`
	AgeSeconds     int64          `json:"age_seconds"` // open: time waiting; closed: time to close
	OverSLA        bool           `json:"over_sla"`
	LastReportedAt time.Time      `json:"last_reported_at"`
	CreatedAt      time.Time      `json:"created_at"`
}

// CaseReport is one report inside a case.
type CaseReport struct {
	ID        uuid.UUID `json:"id"`
	Reporter  *CaseUser `json:"reporter"` // nil once the reporter deleted their account
	Reason    string    `json:"reason"`
	Note      string    `json:"note"`
	Snapshot  Snapshot  `json:"snapshot"`
	MediaURLs []string  `json:"media_urls"`
	CreatedAt time.Time `json:"created_at"`
}

// CaseDetail is GET /v1/admin/moderation/cases/{id}.
type CaseDetail struct {
	Case
	Reports       []CaseReport   `json:"reports"`
	Decisions     []DecisionView `json:"decisions"` // screening decisions on the content, newest first
	AuthorHistory AuthorHistory  `json:"author_history"`
}

// AuthorHistory counts the author's other cases and active strikes.
type AuthorHistory struct {
	Cases    int `json:"cases"`
	Actioned int `json:"actioned"`
	Strikes  int `json:"strikes"`
}

const caseCols = `
	c.id, c.target_kind, c.target_id, c.status, c.source, c.categories, c.report_count,
	COALESCE((SELECT jsonb_object_agg(reason, n) FROM (
	  SELECT reason, count(*) AS n FROM moderation_reports WHERE case_id = c.id GROUP BY reason) x), '{}'::jsonb),
	c.snapshot, c.hidden_at IS NOT NULL, c.auto_hidden, c.action, c.resolution_note, c.resolved_by, c.resolved_at,
	c.last_reported_at, c.created_at,
	u.id, u.display_name, u.email, u.status, u.avatar_url`

func (s *Service) scanCases(ctx context.Context, rows pgx.Rows) ([]Case, error) {
	defer rows.Close()
	out := []Case{}
	now := time.Now()
	for rows.Next() {
		var (
			c                    Case
			reasons, snap        []byte
			uid                  *uuid.UUID
			uname, uemail, ustat *string
			uavatar              *string
		)
		if err := rows.Scan(&c.ID, &c.TargetKind, &c.TargetID, &c.Status, &c.Source, &c.Categories, &c.ReportCount, &reasons, &snap,
			&c.Hidden, &c.AutoHidden, &c.Action, &c.ResolutionNote, &c.ResolvedBy, &c.ResolvedAt,
			&c.LastReportedAt, &c.CreatedAt, &uid, &uname, &uemail, &ustat, &uavatar); err != nil {
			return nil, err
		}
		if c.Categories == nil {
			c.Categories = []string{}
		}
		if err := json.Unmarshal(reasons, &c.Reasons); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(snap, &c.Snapshot); err != nil {
			return nil, err
		}
		if uid != nil {
			c.Author = &CaseUser{ID: *uid, DisplayName: *uname, Email: *uemail, Status: *ustat, AvatarURL: uavatar}
		}
		end := now
		if c.ResolvedAt != nil {
			end = *c.ResolvedAt
		}
		age := end.Sub(c.CreatedAt)
		c.AgeSeconds = int64(age.Seconds())
		c.OverSLA = age > SLA
		c.MediaURLs = s.mediaURLs(ctx, c.Snapshot.MediaKeys)
		out = append(out, c)
	}
	return out, rows.Err()
}

// mediaURLs turns snapshot media into URLs staff can open: public objects
// directly, private ones as 15-minute presigned GETs. Entries that are
// already URLs (a reported person's avatar) pass through.
func (s *Service) mediaURLs(ctx context.Context, keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, "https://") || strings.HasPrefix(k, "http://"):
			out = append(out, k)
		case storage.IsPublic(strings.SplitN(k, "/", 2)[0]):
			out = append(out, s.d.Storage.PublicURL(k))
		default:
			u, err := s.d.Storage.PresignGet(ctx, k, 15*time.Minute)
			if err != nil {
				slog.WarnContext(ctx, "moderation: presign evidence", "err", err)
				continue
			}
			out = append(out, u)
		}
	}
	return out
}

// listCases: GET /v1/admin/moderation/cases?status=open|resolved|dismissed|closed&kind=&author_id=
// Open cases come oldest first (work the queue from the top); closed ones
// newest first.
func (s *Service) listCases(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	status := q.Get("status")
	if status == "" {
		status = "open"
	}
	var statusSQL string
	switch status {
	case "open":
		statusSQL = `c.status = 'open'`
	case "resolved", "dismissed":
		statusSQL = `c.status = '` + status + `'`
	case "closed":
		statusSQL = `c.status <> 'open'`
	default:
		return httpx.BadRequest("status must be open, resolved, dismissed or closed.")
	}
	var authorID *uuid.UUID
	if a := q.Get("author_id"); a != "" {
		id, err := uuid.Parse(a)
		if err != nil {
			return httpx.BadRequest("Invalid author_id.")
		}
		authorID = &id
	}
	order, cmp := "ASC", ">"
	if status != "open" {
		order, cmp = "DESC", "<"
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+caseCols+`
		FROM moderation_cases c LEFT JOIN users u ON u.id = c.author_id
		WHERE `+statusSQL+`
		  AND ($1 = '' OR c.target_kind = $1)
		  AND ($2::uuid IS NULL OR c.author_id = $2)
		  AND ($3::timestamptz IS NULL OR (c.created_at, c.id) `+cmp+` ($3, $4))
		ORDER BY c.created_at `+order+`, c.id `+order+` LIMIT $5`,
		q.Get("kind"), authorID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	cases, err := s.scanCases(r.Context(), rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(cases, p.Limit, func(x Case) any { return cursor{x.CreatedAt, x.ID} }))
	return nil
}

func (s *Service) loadCase(ctx context.Context, id uuid.UUID) (*Case, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+caseCols+`
		FROM moderation_cases c LEFT JOIN users u ON u.id = c.author_id WHERE c.id = $1`, id)
	if err != nil {
		return nil, err
	}
	cases, err := s.scanCases(ctx, rows)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, httpx.NotFound("Case not found.")
	}
	return &cases[0], nil
}

// getCase: GET /v1/admin/moderation/cases/{id} with every report.
func (s *Service) getCase(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	return s.respondCase(w, r, id)
}

func (s *Service) respondCase(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	ctx := r.Context()
	c, err := s.loadCase(ctx, id)
	if err != nil {
		return err
	}
	d := CaseDetail{Case: *c, Reports: []CaseReport{}, Decisions: []DecisionView{}}
	if c.TargetKind != TargetUser {
		if d.Decisions, err = loadDecisions(ctx, s.d.Pool, c.TargetKind, c.TargetID, 10); err != nil {
			return err
		}
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.id, r.reason, r.note, r.snapshot, r.created_at,
		       u.id, u.display_name, u.email, u.status, u.avatar_url
		FROM moderation_reports r LEFT JOIN users u ON u.id = r.reporter_id
		WHERE r.case_id = $1 ORDER BY r.created_at, r.id LIMIT 500`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rep                  CaseReport
			snap                 []byte
			uid                  *uuid.UUID
			uname, uemail, ustat *string
			uavatar              *string
		)
		if err := rows.Scan(&rep.ID, &rep.Reason, &rep.Note, &snap, &rep.CreatedAt,
			&uid, &uname, &uemail, &ustat, &uavatar); err != nil {
			return err
		}
		if err := json.Unmarshal(snap, &rep.Snapshot); err != nil {
			return err
		}
		if uid != nil {
			rep.Reporter = &CaseUser{ID: *uid, DisplayName: *uname, Email: *uemail, Status: *ustat, AvatarURL: uavatar}
		}
		rep.MediaURLs = s.mediaURLs(ctx, rep.Snapshot.MediaKeys)
		d.Reports = append(d.Reports, rep)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if c.Author != nil {
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT count(*), count(*) FILTER (WHERE action IN ('remove', 'warn', 'suspend', 'ban'))
			FROM moderation_cases WHERE author_id = $1 AND id <> $2`, c.Author.ID, id,
		).Scan(&d.AuthorHistory.Cases, &d.AuthorHistory.Actioned); err != nil {
			return err
		}
		if d.AuthorHistory.Strikes, err = Strikes(ctx, s.d.Pool, c.Author.ID, time.Now().Add(-StrikeWindow)); err != nil {
			return err
		}
	}
	httpx.OK(w, d)
	return nil
}

// Summary is GET /v1/admin/moderation/summary: queue health against the SLA.
type Summary struct {
	Open                 int    `json:"open"`
	OverSLA              int    `json:"over_sla"`
	OldestOpenAgeSeconds *int64 `json:"oldest_open_age_seconds"`
	Closed7d             int    `json:"closed_7d"`
	MedianCloseSeconds7d *int64 `json:"median_close_seconds_7d"`
	SLASeconds           int64  `json:"sla_seconds"`
	HeldOpen             int    `json:"held_open"` // open cases screening opened

	DecisionModel DecisionModelHealth `json:"decision_model"`
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request) error {
	out, err := s.loadSummary(r.Context())
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) loadSummary(ctx context.Context) (Summary, error) {
	out := Summary{SLASeconds: int64(SLA.Seconds()), DecisionModel: decisionModelHealth()}
	var oldest, median *float64
	err := s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'open'),
		       count(*) FILTER (WHERE status = 'open' AND created_at < now() - make_interval(secs => $1)),
		       extract(epoch FROM now() - min(created_at) FILTER (WHERE status = 'open'))::float8,
		       count(*) FILTER (WHERE resolved_at > now() - interval '7 days'),
		       (percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM resolved_at - created_at))
		          FILTER (WHERE resolved_at > now() - interval '7 days'))::float8,
		       count(*) FILTER (WHERE status = 'open' AND source = 'auto')
		FROM moderation_cases
		WHERE status = 'open' OR resolved_at > now() - interval '7 days'`, SLA.Seconds(),
	).Scan(&out.Open, &out.OverSLA, &oldest, &out.Closed7d, &median, &out.HeldOpen)
	if oldest != nil {
		v := int64(*oldest)
		out.OldestOpenAgeSeconds = &v
	}
	if median != nil {
		v := int64(*median)
		out.MedianCloseSeconds7d = &v
	}
	return out, err
}

// ---- staff actions -----------------------------------------------------------

// Actions staff take on a case. Every action closes it.
const (
	ActionDismiss = "dismiss" // no violation; auto-hidden content is restored when the Target can
	ActionRemove  = "remove"  // take the content down (Target.Remove)
	ActionWarn    = "warn"    // notify the author
	ActionSuspend = "suspend" // users.status = suspended (read-only account)
	ActionBan     = "ban"     // users.status = banned (can't sign in)
	ActionResolve = "resolve" // close with a note, nothing else
)

type actionReq struct {
	Action          string `json:"action"`
	Note            string `json:"note"`
	RemoveContent   bool   `json:"remove_content"`   // with warn, suspend or ban on content
	NotifyReporters *bool  `json:"notify_reporters"` // default true (and MODERATION_NOTIFY_REPORTER)
}

type lockedCase struct {
	status, kind, targetID, source string
	categories                     []string
	authorID                       *uuid.UUID
	hidden, autoHidden             bool
}

// actOnCase: POST /v1/admin/moderation/cases/{id}/actions.
func (s *Service) actOnCase(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req actionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Note = strings.TrimSpace(req.Note)
	switch req.Action {
	case ActionDismiss, ActionRemove, ActionWarn, ActionSuspend, ActionBan, ActionResolve:
	default:
		return httpx.BadRequest("action must be dismiss, remove, warn, suspend, ban or resolve.")
	}
	if utf8.RuneCountInString(req.Note) > 2000 {
		return httpx.BadRequest("Keep the note under 2000 characters.")
	}
	if req.RemoveContent && req.Action != ActionWarn && req.Action != ActionSuspend && req.Action != ActionBan {
		return httpx.BadRequest("remove_content goes with warn, suspend or ban.")
	}

	var (
		lc        lockedCase
		removed   bool
		restored  bool
		struck    bool
		reporters []uuid.UUID
	)
	notifyReporters := s.cfg.notifyReporter && (req.NotifyReporters == nil || *req.NotifyReporters)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT status, target_kind, target_id, source, categories, author_id, hidden_at IS NOT NULL, auto_hidden
			FROM moderation_cases WHERE id = $1 FOR UPDATE`, id).
			Scan(&lc.status, &lc.kind, &lc.targetID, &lc.source, &lc.categories, &lc.authorID, &lc.hidden, &lc.autoHidden)
		if db.IsNoRows(err) {
			return httpx.NotFound("Case not found.")
		}
		if err != nil {
			return err
		}
		if lc.status != "open" {
			return httpx.Conflict("This case is already closed.")
		}
		isUser := lc.kind == TargetUser

		// Content screening held or removed waits for a decision on the
		// content itself: approve it (dismiss) or take it down.
		held := lc.source == "auto" && lc.autoHidden
		if held && (req.Action == ActionResolve || ((req.Action == ActionWarn || req.Action == ActionSuspend || req.Action == ActionBan) && !req.RemoveContent)) {
			return httpx.BadRequest("This content is held: approve it, or remove it (alone or with the author action).")
		}

		// Content removal. Held content is taken down by its Target too
		// (Remove is idempotent), so it leaves the held state.
		if req.Action == ActionRemove || req.RemoveContent {
			if isUser {
				return httpx.BadRequest("A person can't be removed. Suspend or ban them instead.")
			}
			if !lc.hidden || held {
				t, ok := target(lc.kind)
				if !ok {
					return httpx.Conflict("That content type is no longer installed, so it can't be removed here.")
				}
				if err := t.Remove(ctx, tx, lc.targetID); err != nil {
					return fmt.Errorf("moderation: remove %s: %w", lc.kind, err)
				}
			}
			removed = true
			if _, err := tx.Exec(ctx, `UPDATE moderation_cases SET hidden_at = COALESCE(hidden_at, now()), auto_hidden = false
				WHERE id = $1`, id); err != nil {
				return err
			}
		}

		// Dismissal restores what reports hid automatically.
		if req.Action == ActionDismiss && lc.autoHidden {
			if t, ok := target(lc.kind); ok {
				if rs, ok := t.(Restorer); ok {
					if err := rs.Restore(ctx, tx, lc.targetID); err != nil {
						return fmt.Errorf("moderation: restore %s: %w", lc.kind, err)
					}
					if _, err := tx.Exec(ctx, `UPDATE moderation_cases SET hidden_at = NULL, auto_hidden = false WHERE id = $1`, id); err != nil {
						return err
					}
					restored = true
				}
			}
		}

		// Author actions.
		switch req.Action {
		case ActionWarn, ActionSuspend, ActionBan:
			if lc.authorID == nil {
				return httpx.BadRequest("This content has no author to act on.")
			}
		}
		if req.Action == ActionSuspend || req.Action == ActionBan {
			if err := applySuspension(ctx, tx, staff, *lc.authorID, req.Action == ActionBan, id, req.Note); err != nil {
				return err
			}
		}

		// A removal or a ban is a strike against the author.
		if lc.authorID != nil && (removed || req.Action == ActionBan) {
			kind := ActionRemove
			if req.Action == ActionBan {
				kind = ActionBan
			}
			category := ""
			if len(lc.categories) > 0 {
				category = lc.categories[0]
			}
			if err := addStrike(ctx, tx, *lc.authorID, id, kind, category, staff.UserID); err != nil {
				return err
			}
			struck = true
		}

		status := "resolved"
		if req.Action == ActionDismiss {
			status = "dismissed"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE moderation_cases SET status = $2, action = $3, resolution_note = $4, resolved_by = $5,
			  resolved_at = now(), updated_at = now()
			WHERE id = $1`, id, status, req.Action, req.Note, staff.UserID); err != nil {
			return err
		}
		if notifyReporters {
			rows, err := tx.Query(ctx, `
				UPDATE moderation_reports SET outcome_notified_at = now()
				WHERE case_id = $1 AND reporter_id IS NOT NULL AND outcome_notified_at IS NULL
				RETURNING reporter_id`, id)
			if err != nil {
				return err
			}
			if reporters, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, staff.UserID, "moderation.case."+req.Action, "moderation_case", id.String(), map[string]any{
			"target_kind": lc.kind, "target_id": lc.targetID, "author_id": lc.authorID, "source": lc.source,
			"removed": removed, "restored": restored, "strike": struck, "note": req.Note,
		})
	})
	if err != nil {
		return err
	}
	s.afterAction(context.WithoutCancel(ctx), id, req.Action, lc, removed, restored, struck, reporters)
	return s.respondCase(w, r, id)
}

// applySuspension sets the author's status. Nobody suspends themselves and
// only admins suspend staff accounts.
func applySuspension(ctx context.Context, tx pgx.Tx, staff *auth.Identity, userID uuid.UUID, ban bool, caseID uuid.UUID, note string) error {
	if userID == staff.UserID {
		return httpx.Forbidden("You can't suspend your own account.")
	}
	var (
		role     *string
		previous string
	)
	err := tx.QueryRow(ctx, `SELECT admin_role, status FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID).Scan(&role, &previous)
	if db.IsNoRows(err) {
		return httpx.NotFound("User not found.")
	}
	if err != nil {
		return err
	}
	if previous == "deleted" {
		return httpx.Conflict("That account was deleted.")
	}
	if role != nil && !staff.IsAdmin() {
		return httpx.Forbidden("Only admins can suspend staff accounts.")
	}
	status, action := "suspended", "user.suspend"
	if ban {
		status, action = "banned", "user.ban"
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, userID, status); err != nil {
		return err
	}
	return audit.Log(ctx, tx, staff.UserID, action, "user", userID.String(), map[string]any{
		"previous_status": previous, "case_id": caseID, "note": note,
	})
}

// afterAction sends the best-effort side effects once the action committed.
func (s *Service) afterAction(ctx context.Context, id uuid.UUID, action string, lc lockedCase, removed, restored, struck bool, reporters []uuid.UUID) {
	if (removed || restored) && lc.kind != TargetUser {
		if t, ok := target(lc.kind); ok {
			if ch, ok := t.(Changer); ok {
				ch.Changed(ctx, lc.targetID, restored)
			}
		}
	}
	// Reporters learn only whether something was done, never what.
	outcome, body := "closed", "Thanks for your report. We reviewed it and closed it."
	if action == ActionRemove || action == ActionWarn || action == ActionSuspend || action == ActionBan {
		outcome, body = "actioned", "Thanks for your report. We reviewed it and took action."
	}
	for _, uid := range reporters {
		s.send(ctx, uid, KindOutcome, "Your report was reviewed", body, "/settings/safety", map[string]any{"outcome": outcome})
	}
	if lc.authorID != nil {
		support := s.d.Cfg.SupportEmail
		switch action {
		case ActionSuspend:
			s.send(ctx, *lc.authorID, KindNotice, "Your account is suspended",
				"Contact "+support+" if you think this is a mistake.", "", nil)
		case ActionBan:
			s.send(ctx, *lc.authorID, KindNotice, "Your account has been closed",
				"Contact "+support+" if you think this is a mistake.", "", nil)
		case ActionWarn:
			body := "We reviewed a report about your activity. Repeated reports can lead to suspension."
			if removed {
				body = "We removed something you posted because it broke the community rules. Repeated reports can lead to suspension."
			}
			s.send(ctx, *lc.authorID, KindNotice, "A warning about your account", body, "", nil)
		case ActionRemove:
			s.send(ctx, *lc.authorID, KindNotice, "Your "+kindNoun(lc.kind)+" was removed",
				"It broke the community rules. Repeated removals stop you posting for a while.", "/settings/safety", nil)
		case ActionDismiss:
			if lc.source == "auto" && restored {
				s.send(ctx, *lc.authorID, KindNotice, "Your "+kindNoun(lc.kind)+" was approved",
					"A moderator reviewed it and it's now visible to everyone.", s.snapshotLink(ctx, id), nil)
			}
		}
		if struck && action != ActionBan {
			if until, err := RestrictedUntil(ctx, s.d.Pool, *lc.authorID); err == nil && !until.IsZero() {
				s.send(ctx, *lc.authorID, KindNotice, "You can't post for a while",
					"After repeated removals you can post again on "+until.UTC().Format("Jan 2, 15:04")+" UTC.", "/settings/safety", nil)
			}
		}
	}
	s.publish(ctx, realtime.AdminLiveTopic, EventCaseClosed, map[string]any{"case_id": id, "action": action})
}

// snapshotLink is the app path stored with a case's content.
func (s *Service) snapshotLink(ctx context.Context, id uuid.UUID) string {
	var link string
	_ = s.d.Pool.QueryRow(ctx, `SELECT COALESCE(snapshot->>'link', '') FROM moderation_cases WHERE id = $1`, id).Scan(&link)
	return link
}
