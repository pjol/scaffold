package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/webhooks"
)

// ---- deletion --------------------------------------------------------------

func (s *Service) deleteMe(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if err := s.DeleteAccount(context.WithoutCancel(r.Context()), id.UserID, "user"); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// DeleteAccount deletes a user's account: every module's AccountDeleter runs
// and the users row is anonymised in one transaction, then the identity
// provider user and stored objects are removed (retried by a worker until
// done). Source is user, admin or provider. Irreversible.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID, source string) error {
	var subject string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT auth_subject FROM users WHERE id = $1 AND status <> 'deleted' FOR UPDATE`, userID).Scan(&subject); err != nil {
			if db.IsNoRows(err) {
				return httpx.ErrNotFound
			}
			return err
		}
		for _, m := range s.d.Modules {
			if del, ok := m.(app.AccountDeleter); ok {
				if err := del.DeleteAccount(ctx, tx, userID); err != nil {
					return fmt.Errorf("delete account: module %s: %w", m.Name(), err)
				}
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE users SET
			  auth_subject = 'deleted:' || id, email = 'deleted+' || id || '@deleted.invalid',
			  display_name = '', avatar_url = NULL, admin_role = NULL,
			  status = 'deleted', deleted_at = now(), updated_at = now()
			WHERE id = $1`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO account_deletions (user_id, auth_subject, source, provider_deleted)
			VALUES ($1, $2, $3, $4)`, userID, subject, source, source == "provider" || strings.HasPrefix(subject, "dev_"))
		return err
	})
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "account deleted", "user_id", userID, "source", source)
	for _, m := range s.d.Modules {
		if h, ok := m.(app.AccountDeletedHook); ok {
			if err := h.AccountDeleted(ctx, userID); err != nil {
				slog.WarnContext(ctx, "account deleted hook failed", "module", m.Name(), "user_id", userID, "err", err)
			}
		}
	}
	if err := s.finishDeletions(ctx); err != nil {
		slog.WarnContext(ctx, "account deletion: external cleanup will retry", "user_id", userID, "err", err)
	}
	return nil
}

// finishDeletions removes provider users and stored objects for pending
// deletions. Runs inline after a deletion and every 10 minutes.
func (s *Service) finishDeletions(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, user_id, COALESCE(auth_subject, ''), provider_deleted, storage_deleted
		FROM account_deletions WHERE completed_at IS NULL AND attempts < 20
		ORDER BY requested_at LIMIT 50`)
	if err != nil {
		return err
	}
	type pending struct {
		id               int64
		userID           uuid.UUID
		subject          string
		provider, stored bool
	}
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.userID, &p.subject, &p.provider, &p.stored); err != nil {
			rows.Close()
			return err
		}
		list = append(list, p)
	}
	rows.Close()
	var lastErr error
	for _, p := range list {
		var errs []string
		if !p.provider && s.d.Auth.Provider() != nil {
			if err := s.d.Auth.Provider().DeleteUser(ctx, p.subject); err != nil {
				errs = append(errs, "provider: "+err.Error())
			} else {
				p.provider = true
			}
		}
		if !p.stored {
			if err := s.d.Storage.DeleteOwner(ctx, p.userID); err != nil {
				errs = append(errs, "storage: "+err.Error())
			} else {
				p.stored = true
			}
		}
		done := p.provider && p.stored
		_, err := s.d.Pool.Exec(ctx, `
			UPDATE account_deletions SET provider_deleted = $2, storage_deleted = $3, attempts = attempts + 1,
			  last_error = $4, completed_at = CASE WHEN $5 THEN now() END,
			  auth_subject = CASE WHEN $5 THEN NULL ELSE auth_subject END
			WHERE id = $1`, p.id, p.provider, p.stored, strings.Join(errs, "; "), done)
		if err != nil {
			lastErr = err
		} else if len(errs) > 0 {
			lastErr = fmt.Errorf("%s", strings.Join(errs, "; "))
		}
	}
	return lastErr
}

// ---- export ----------------------------------------------------------------

func (s *Service) export(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	me, err := s.loadMe(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	type acceptance struct {
		Document   string    `json:"document"`
		Version    string    `json:"version"`
		Platform   string    `json:"platform"`
		AcceptedAt time.Time `json:"accepted_at"`
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT document, version, platform, accepted_at FROM legal_acceptances
		WHERE user_id = $1 ORDER BY accepted_at`, id.UserID)
	if err != nil {
		return err
	}
	acceptances, err := pgx.CollectRows(rows, pgx.RowToStructByPos[acceptance])
	if err != nil {
		return err
	}
	out := map[string]any{
		"exported_at":       time.Now().UTC(),
		"account":           me,
		"legal_acceptances": acceptances,
	}
	for _, m := range s.d.Modules {
		if ex, ok := m.(app.AccountExporter); ok {
			v, err := ex.ExportAccount(r.Context(), id.UserID)
			if err != nil {
				return fmt.Errorf("export: module %s: %w", m.Name(), err)
			}
			out[m.Name()] = v
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="account-export.json"`)
	httpx.OK(w, out)
	return nil
}

// ---- Clerk webhook ---------------------------------------------------------

// clerkWebhook keeps users in step with Clerk (Svix-signed): user.updated
// syncs email, name and avatar; user.deleted deletes the account.
func (s *Service) clerkWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unreadable body."))
		return
	}
	if !webhooks.VerifySvix(s.d.Cfg.ClerkWebhookSecret, r.Header, body, time.Now()) {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "bad_signature", "Signature check failed."))
		return
	}
	var ev struct {
		Type string `json:"type"`
		Data struct {
			ID             string  `json:"id"`
			FirstName      *string `json:"first_name"`
			LastName       *string `json:"last_name"`
			ImageURL       *string `json:"image_url"`
			PrimaryEmailID *string `json:"primary_email_address_id"`
			EmailAddresses []struct {
				ID           string `json:"id"`
				EmailAddress string `json:"email_address"`
				Verification *struct {
					Status string `json:"status"`
				} `json:"verification"`
			} `json:"email_addresses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.Data.ID == "" {
		httpx.Fail(w, r, httpx.BadRequest("Unexpected payload."))
		return
	}
	ctx := context.WithoutCancel(r.Context())
	switch ev.Type {
	case "user.updated":
		email := ""
		for _, e := range ev.Data.EmailAddresses {
			if e.Verification != nil && e.Verification.Status == "verified" &&
				(email == "" || (ev.Data.PrimaryEmailID != nil && e.ID == *ev.Data.PrimaryEmailID)) {
				email = strings.ToLower(e.EmailAddress)
			}
		}
		name := strings.TrimSpace(deref(ev.Data.FirstName) + " " + deref(ev.Data.LastName))
		_, err = s.d.Pool.Exec(ctx, `
			UPDATE users SET email = COALESCE(NULLIF($2, ''), email),
			  display_name = CASE WHEN display_name = '' THEN $3 ELSE display_name END,
			  updated_at = now()
			WHERE auth_subject = $1 AND status <> 'deleted'`, ev.Data.ID, email, name)
	case "user.deleted":
		var userID uuid.UUID
		if err = s.d.Pool.QueryRow(ctx, `SELECT id FROM users WHERE auth_subject = $1 AND status <> 'deleted'`, ev.Data.ID).Scan(&userID); err == nil {
			err = s.DeleteAccount(ctx, userID, "provider")
		} else if db.IsNoRows(err) {
			err = nil
		}
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
