package docs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
)

// NotifyReminder is the notification kind for date reminders in pages.
const NotifyReminder = "docs.reminder"

// reminderSpec is one reminder a block's text holds.
type reminderSpec struct {
	at    time.Time
	label string
}

// remindersIn finds date mentions with remind_at in a block's rich text
// (text, caption, table cells).
func remindersIn(b Block) map[time.Time]reminderSpec {
	out := map[time.Time]reminderSpec{}
	add := func(spans []Span) {
		for _, s := range spans {
			if s.Mention == nil || s.Mention.Type != "date" || s.Mention.RemindAt == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, s.Mention.RemindAt)
			if err != nil {
				continue
			}
			label := s.Mention.Date
			if s.Mention.Time != "" {
				label += " " + s.Mention.Time
			}
			out[t.UTC()] = reminderSpec{at: t.UTC(), label: label}
		}
	}
	add(spansOf(b.Props, "text"))
	add(spansOf(b.Props, "caption"))
	if b.Type == "table" {
		raw, _ := json.Marshal(b.Props["rows"])
		var rows [][][]Span
		_ = json.Unmarshal(raw, &rows)
		for _, r := range rows {
			for _, c := range r {
				add(c)
			}
		}
	}
	return out
}

// syncReminders makes docs_reminders match the reminders in the blocks a
// batch touched: new ones belong to whoever wrote them (me); ones still there
// keep their owner; removed ones go. Deleted blocks take theirs along (FK).
func syncReminders(ctx context.Context, tx pgx.Tx, p *Page, blocks []Block, _ []uuid.UUID, me uuid.UUID) error {
	for _, b := range blocks {
		want := remindersIn(b)
		rows, err := tx.Query(ctx, `SELECT remind_at FROM docs_reminders WHERE block_id = $1`, b.ID)
		if err != nil {
			return err
		}
		have, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
		if err != nil {
			return err
		}
		if len(want) == 0 && len(have) == 0 {
			continue
		}
		var gone []time.Time
		seen := map[time.Time]bool{}
		for _, t := range have {
			t = t.UTC()
			seen[t] = true
			if _, ok := want[t]; !ok {
				gone = append(gone, t)
			}
		}
		if len(gone) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM docs_reminders WHERE block_id = $1 AND remind_at = ANY ($2)`, b.ID, gone); err != nil {
				return err
			}
		}
		for t, r := range want {
			if seen[t] || r.at.Before(time.Now().Add(-time.Minute)) {
				continue // kept, or written for a time already past
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_reminders (page_id, block_id, user_id, remind_at, date_label) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (block_id, remind_at) DO NOTHING`, p.ID, b.ID, me, r.at, r.label); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendReminders notifies people whose reminders are due (once each). A
// reminder more than a day late (the server was down) is dropped quietly; one
// on a page its owner can no longer open, or in the trash, is dropped too.
func (s *Service) sendReminders(ctx context.Context) error {
	type due struct {
		ID, PageID, BlockID, UserID uuid.UUID
		Label                       string
		Stale                       bool
	}
	rows, err := s.d.Pool.Query(ctx, `
		UPDATE docs_reminders r SET sent_at = now()
		WHERE r.id IN (SELECT id FROM docs_reminders WHERE sent_at IS NULL AND remind_at <= now() ORDER BY remind_at LIMIT 200 FOR UPDATE SKIP LOCKED)
		RETURNING r.id, r.page_id, r.block_id, r.user_id, r.date_label, r.remind_at < now() - interval '1 day'`)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[due])
	if err != nil {
		return err
	}
	for _, d := range list {
		if d.Stale {
			continue
		}
		var title, plain string
		var trashed bool
		err := s.d.Pool.QueryRow(ctx, `
			SELECT p.title, p.deleted_at IS NOT NULL, b.plain FROM docs_pages p JOIN docs_blocks b ON b.page_id = p.id
			WHERE p.id = $1 AND b.id = $2`, d.PageID, d.BlockID).Scan(&title, &trashed, &plain)
		if db.IsNoRows(err) || trashed || !s.canView(ctx, d.UserID, d.PageID) {
			continue
		}
		if err != nil {
			return err
		}
		s.send(ctx, notify.Notification{
			UserID: d.UserID, Kind: NotifyReminder, Title: "Reminder in " + displayTitle(title), Body: snippet(plain, 140),
			Link: "/docs/" + d.PageID.String() + "#block-" + d.BlockID.String(), Data: map[string]any{"page_id": d.PageID, "block_id": d.BlockID},
		})
	}
	// Sent reminders are kept a month (so an edit doesn't re-create them), then go.
	_, err = s.d.Pool.Exec(ctx, `DELETE FROM docs_reminders WHERE sent_at < now() - interval '30 days'`)
	return err
}
