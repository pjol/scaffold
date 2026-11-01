package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
)

// Reasons are the report reasons clients offer, in display order.
var Reasons = []string{"spam", "harassment", "hate", "sexual", "violence", "self_harm", "illegal", "other"}

func validReason(r string) bool {
	for _, x := range Reasons {
		if x == r {
			return true
		}
	}
	return false
}

// MyReport is a report as its reporter sees it. Outcome is generic on
// purpose: open | actioned | closed.
type MyReport struct {
	ID         uuid.UUID `json:"id"`
	TargetKind string    `json:"target_kind"`
	TargetID   string    `json:"target_id"`
	Reason     string    `json:"reason"`
	Note       string    `json:"note"`
	Outcome    string    `json:"outcome"`
	CreatedAt  time.Time `json:"created_at"`
}

type createReportReq struct {
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	Note       string `json:"note"`
}

var errReportLimit = httpx.NewError(http.StatusTooManyRequests, "rate_limited",
	fmt.Sprintf("You've sent %d reports in the last hour. Try again later or contact support.", reportsPerHour))

// createReport files a report. Reporting the same target again while its
// case is open updates the earlier report (200) instead of adding one (201).
func (s *Service) createReport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req createReportReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.TargetKind = strings.TrimSpace(req.TargetKind)
	req.TargetID = strings.TrimSpace(req.TargetID)
	req.Note = strings.TrimSpace(req.Note)
	if req.TargetKind == "" || req.TargetID == "" {
		return httpx.BadRequest("target_kind and target_id are required.")
	}
	if !validReason(req.Reason) {
		return httpx.BadRequest("Choose a reason: " + strings.Join(Reasons, ", ") + ".")
	}
	if utf8.RuneCountInString(req.Note) > maxNoteChars {
		return httpx.BadRequest(fmt.Sprintf("Keep the note under %d characters.", maxNoteChars))
	}

	snap, err := s.snapshot(ctx, req.TargetKind, req.TargetID)
	if err != nil {
		return err
	}
	if snap.AuthorID != nil && *snap.AuthorID == me {
		if req.TargetKind == TargetUser {
			return httpx.BadRequest("You can't report yourself.")
		}
		return httpx.BadRequest("You can't report your own content.")
	}
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	var (
		out       MyReport
		inserted  bool
		caseID    uuid.UUID
		newCase   bool
		autoHid   bool
		reporters int
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// Counted in the database so the limit holds across instances.
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM moderation_reports
			WHERE reporter_id = $1 AND created_at > now() - interval '1 hour'`, me).Scan(&recent); err != nil {
			return err
		}
		var hidden bool
		if err := tx.QueryRow(ctx, `
			INSERT INTO moderation_cases (target_kind, target_id, author_id, snapshot)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (target_kind, target_id) WHERE status = 'open'
			DO UPDATE SET last_reported_at = now(), updated_at = now()
			RETURNING id, xmax = 0, hidden_at IS NOT NULL`,
			req.TargetKind, req.TargetID, snap.AuthorID, snapJSON).Scan(&caseID, &newCase, &hidden); err != nil {
			return err
		}
		var existing bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM moderation_reports WHERE case_id = $1 AND reporter_id = $2)`,
			caseID, me).Scan(&existing); err != nil {
			return err
		}
		if !existing && recent >= reportsPerHour {
			return errReportLimit
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO moderation_reports (case_id, reporter_id, target_kind, target_id, reason, note, snapshot)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (case_id, reporter_id) DO UPDATE SET
			  reason = EXCLUDED.reason,
			  note = CASE WHEN EXCLUDED.note <> '' THEN EXCLUDED.note ELSE moderation_reports.note END,
			  snapshot = EXCLUDED.snapshot, updated_at = now()
			RETURNING id, reason, note, created_at, xmax = 0`,
			caseID, me, req.TargetKind, req.TargetID, req.Reason, req.Note, snapJSON,
		).Scan(&out.ID, &out.Reason, &out.Note, &out.CreatedAt, &inserted); err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		if err := tx.QueryRow(ctx, `UPDATE moderation_cases SET report_count = report_count + 1 WHERE id = $1
			RETURNING report_count`, caseID).Scan(&reporters); err != nil {
			return err
		}
		if s.cfg.autoHideReports > 0 && reporters >= s.cfg.autoHideReports && !hidden && req.TargetKind != TargetUser {
			autoHid = s.autoHide(ctx, tx, caseID, req.TargetKind, req.TargetID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	out.TargetKind, out.TargetID, out.Outcome = req.TargetKind, req.TargetID, "open"
	if newCase {
		s.publish(ctx, realtime.AdminLiveTopic, EventCaseOpened, map[string]any{
			"case_id": caseID, "target_kind": req.TargetKind, "reason": req.Reason,
		})
	}
	if autoHid {
		slog.InfoContext(ctx, "moderation: content auto-hidden", "case_id", caseID, "target_kind", req.TargetKind, "reporters", reporters)
	}
	status := http.StatusOK
	if inserted {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, out)
	return nil
}

// autoHide removes the target inside a savepoint: a failing Remove is
// logged and the report is still filed.
func (s *Service) autoHide(ctx context.Context, tx pgx.Tx, caseID uuid.UUID, kind, id string) bool {
	t, ok := target(kind)
	if !ok {
		return false
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		slog.WarnContext(ctx, "moderation: auto-hide savepoint", "case_id", caseID, "err", err)
		return false
	}
	if err := t.Remove(ctx, sp, id); err != nil {
		_ = sp.Rollback(ctx)
		slog.WarnContext(ctx, "moderation: auto-hide failed", "case_id", caseID, "target_kind", kind, "err", err)
		return false
	}
	if _, err := sp.Exec(ctx, `UPDATE moderation_cases SET hidden_at = now(), auto_hidden = true, updated_at = now()
		WHERE id = $1`, caseID); err != nil {
		_ = sp.Rollback(ctx)
		slog.WarnContext(ctx, "moderation: auto-hide mark", "case_id", caseID, "err", err)
		return false
	}
	if err := sp.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "moderation: auto-hide commit", "case_id", caseID, "err", err)
		return false
	}
	return true
}

// snapshot captures the target now: the owning feature's Target for content,
// the users row for kind "user".
func (s *Service) snapshot(ctx context.Context, kind, id string) (Snapshot, error) {
	if kind == TargetUser {
		return s.userSnapshot(ctx, id)
	}
	t, ok := target(kind)
	if !ok {
		return Snapshot{}, httpx.BadRequest("That can't be reported.")
	}
	snap, err := t.Snapshot(ctx, id)
	if err != nil {
		var apiErr *httpx.Error
		switch {
		case db.IsNoRows(err), errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
			return Snapshot{}, httpx.NotFound("That content no longer exists.")
		case errors.As(err, &apiErr):
			return Snapshot{}, apiErr
		}
		return Snapshot{}, fmt.Errorf("moderation: snapshot %s: %w", kind, err)
	}
	if snap.MediaKeys == nil {
		snap.MediaKeys = []string{}
	}
	return snap, nil
}

// userSnapshot records a person's public profile. Their avatar URL goes in
// MediaKeys so staff see the picture that was reported.
func (s *Service) userSnapshot(ctx context.Context, id string) (Snapshot, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return Snapshot{}, httpx.BadRequest("Invalid target_id.")
	}
	var (
		name   string
		avatar *string
	)
	err = s.d.Pool.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1 AND status <> 'deleted'`, uid).
		Scan(&name, &avatar)
	if db.IsNoRows(err) {
		return Snapshot{}, httpx.NotFound("That person no longer has an account.")
	}
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{AuthorID: &uid, Text: name, MediaKeys: []string{}, Link: "/users/" + uid.String()}
	if avatar != nil && *avatar != "" {
		snap.MediaKeys = append(snap.MediaKeys, *avatar)
	}
	return snap, nil
}

// listMyReports: GET /v1/moderation/reports, newest first.
func (s *Service) listMyReports(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := myReports(r.Context(), s.d.Pool, me, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m MyReport) any { return cursor{m.CreatedAt, m.ID} }))
	return nil
}

func myReports(ctx context.Context, q db.Querier, me uuid.UUID, before *time.Time, beforeID uuid.UUID, limit int) ([]MyReport, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.target_kind, r.target_id, r.reason, r.note,
		       CASE WHEN c.status = 'open' THEN 'open'
		            WHEN c.action IN ('remove', 'warn', 'suspend', 'ban') THEN 'actioned'
		            ELSE 'closed' END,
		       r.created_at
		FROM moderation_reports r JOIN moderation_cases c ON c.id = r.case_id
		WHERE r.reporter_id = $1 AND ($2::timestamptz IS NULL OR (r.created_at, r.id) < ($2, $3))
		ORDER BY r.created_at DESC, r.id DESC LIMIT $4`, me, before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MyReport])
}
