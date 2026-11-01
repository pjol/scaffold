package docs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/moderation"
)

// pageTarget makes published pages reportable. Removing one takes its
// public copy down (every link to it stops working; the page itself stays
// for its members); restoring puts it back.
type pageTarget struct{ s *Service }

func (t pageTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	pid, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, fmt.Errorf("%w: bad page id", pgx.ErrNoRows)
	}
	var title string
	var author *uuid.UUID
	if err := t.s.d.Pool.QueryRow(ctx, `SELECT title, created_by FROM docs_pages WHERE id = $1`, pid).Scan(&title, &author); err != nil {
		return moderation.Snapshot{}, err
	}
	var text string
	if err := t.s.d.Pool.QueryRow(ctx, `SELECT COALESCE(string_agg(plain, E'\n' ORDER BY position), '') FROM (
		SELECT plain, position FROM docs_blocks WHERE page_id = $1 AND plain <> '' ORDER BY position LIMIT 200) x`, pid).Scan(&text); err != nil {
		return moderation.Snapshot{}, err
	}
	body := displayTitle(title)
	if text != "" {
		body += "\n\n" + text
	}
	if r := []rune(body); len(r) > 4000 {
		body = string(r[:4000]) + "…"
	}
	return moderation.Snapshot{AuthorID: author, Text: body, MediaKeys: []string{}, Link: "/docs/" + pid.String()}, nil
}

func (t pageTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE docs_pages SET public_blocked_at = now() WHERE id = $1`, id)
	return err
}

func (t pageTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE docs_pages SET public_blocked_at = NULL WHERE id = $1`, id)
	return err
}

// commentTarget makes comments reportable; removing hides one.
type commentTarget struct{ s *Service }

func (t commentTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	cid, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, fmt.Errorf("%w: bad comment id", pgx.ErrNoRows)
	}
	var pageID uuid.UUID
	var thread *uuid.UUID
	var author *uuid.UUID
	var plain string
	err = t.s.d.Pool.QueryRow(ctx, `SELECT page_id, thread_id, author_id, plain FROM docs_comments WHERE id = $1`, cid).Scan(&pageID, &thread, &author, &plain)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	root := cid
	if thread != nil {
		root = *thread
	}
	return moderation.Snapshot{AuthorID: author, Text: strings.TrimSpace(plain), MediaKeys: []string{},
		Link: "/docs/" + pageID.String() + "?comment=" + root.String()}, nil
}

func (t commentTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE docs_comments SET hidden_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

func (t commentTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE docs_comments SET hidden_at = NULL, updated_at = now() WHERE id = $1`, id)
	return err
}
