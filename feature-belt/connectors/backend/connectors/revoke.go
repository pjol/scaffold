package connectors

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
)

// Revocation (RFC 7009) is queued, not attempted inline: the rows holding
// the tokens are deleted in the caller's transaction (disconnect, account
// deletion, a deleted connector), and the token moves to
// connector_revocations, sealed, until the provider confirms. Callers then
// try the queue right away; the worker retries with backoff and gives up
// after maxRevokeAttempts, deleting the row either way. A provider without a
// revocation endpoint gets nothing queued: our copy is deleted and that is
// all we can do.

const maxRevokeAttempts = 6

// enqueueRevocation queues one token. Empty tokens or endpoints are skipped.
func (s *Service) enqueueRevocation(ctx context.Context, q db.Querier, userID *uuid.UUID, endpoint string, client oauthClient, token, hint string) error {
	_, err := s.enqueueRevocationID(ctx, q, userID, endpoint, client, token, hint)
	return err
}

// enqueueRevocationID is enqueueRevocation returning the queued row (uuid.Nil
// when nothing was queued).
func (s *Service) enqueueRevocationID(ctx context.Context, q db.Querier, userID *uuid.UUID, endpoint string, client oauthClient, token, hint string) (uuid.UUID, error) {
	if endpoint == "" || token == "" {
		return uuid.Nil, nil
	}
	id := uuid.New()
	_, err := q.Exec(ctx, `
		INSERT INTO connector_revocations (id, user_id, revocation_endpoint, client_id, client_secret_enc, token_auth_method,
		  token_enc, token_type_hint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, userID, endpoint, client.ID, s.seal.seal(client.Secret, aadRevocation(id, "client_secret")), client.AuthMethod,
		s.seal.seal(token, aadRevocation(id, "token")), hint)
	return id, err
}

// enqueueConnectionRevocations queues a connection's refresh and access
// tokens (refresh first: revoking it usually ends the grant).
func (s *Service) enqueueConnectionRevocations(ctx context.Context, q db.Querier, cc *connectionRow) error {
	_, err := s.queueConnection(ctx, q, cc)
	return err
}

// queueConnection is enqueueConnectionRevocations returning the queued rows.
func (s *Service) queueConnection(ctx context.Context, q db.Querier, cc *connectionRow) ([]uuid.UUID, error) {
	if cc.RevocationEndpoint == "" {
		return nil, nil
	}
	client, err := cc.client(s)
	if err != nil {
		slog.WarnContext(ctx, "connectors: client secret unreadable; skipping revocation", "connection_id", cc.ID)
		return nil, nil
	}
	refresh, _ := s.seal.open(cc.RefreshTokenEnc, aadConnection(cc.ID, "refresh"))
	access, _ := s.seal.open(cc.AccessTokenEnc, aadConnection(cc.ID, "access"))
	uid := cc.UserID
	var ids []uuid.UUID
	for _, t := range []struct{ tok, hint string }{{refresh, "refresh_token"}, {access, "access_token"}} {
		id, err := s.enqueueRevocationID(ctx, q, &uid, cc.RevocationEndpoint, client, t.tok, t.hint)
		if err != nil {
			return nil, err
		}
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// revokeNow tries the given queued rows right away and returns how many
// are still queued (the worker retries those).
func (s *Service) revokeNow(ctx context.Context, ids []uuid.UUID) int {
	if len(ids) == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for range ids {
		if tried, _, err := s.revokeOne(ctx, nil, ids); err != nil || !tried {
			break
		}
	}
	var left int
	_ = s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM connector_revocations WHERE id = ANY($1)`, ids).Scan(&left)
	return left
}

// processRevocations is the worker: due rows, oldest first.
func (s *Service) processRevocations(ctx context.Context) error {
	_, err := s.revokeDue(ctx, nil)
	return err
}

// kickRevocations tries every due revocation now, in the background.
func (s *Service) kickRevocations(ctx context.Context) {
	bg.Go(ctx, "connectors.revoke", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := s.revokeDue(ctx, nil); err != nil {
			slog.WarnContext(ctx, "connectors: revoking now failed", "err", err)
		}
		return nil
	})
}

// kickUserRevocations runs processUserRevocations in the background.
func (s *Service) kickUserRevocations(ctx context.Context, userID uuid.UUID) {
	bg.Go(ctx, "connectors.revoke_user", func(ctx context.Context) error {
		s.processUserRevocations(ctx, userID)
		return nil
	})
}

// processUserRevocations tries a user's queued revocations now (after a
// disconnect or account deletion). Best effort; the worker retries.
func (s *Service) processUserRevocations(ctx context.Context, userID uuid.UUID) (done, pending int) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done, err := s.revokeDue(ctx, &userID)
	if err != nil {
		slog.WarnContext(ctx, "connectors: revoking now failed", "user_id", userID, "err", err)
	}
	_ = s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM connector_revocations WHERE user_id = $1`, userID).Scan(&pending)
	return done, pending
}

// revokeDue attempts up to 50 due revocations (one user's when userID is
// set). Each row is locked while it is tried, so instances never double up.
func (s *Service) revokeDue(ctx context.Context, userID *uuid.UUID) (int, error) {
	done := 0
	for range 50 {
		tried, ok, err := s.revokeOne(ctx, userID, nil)
		if err != nil {
			return done, err
		}
		if !tried {
			return done, nil
		}
		if ok {
			done++
		}
	}
	return done, nil
}

func (s *Service) revokeOne(ctx context.Context, userID *uuid.UUID, only []uuid.UUID) (tried, ok bool, err error) {
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		var endpoint, clientID, method, hint string
		var secretEnc, tokenEnc []byte
		var attempts int
		e := tx.QueryRow(ctx, `
			SELECT id, revocation_endpoint, client_id, client_secret_enc, token_auth_method, token_enc, token_type_hint, attempts
			FROM connector_revocations
			WHERE next_attempt_at <= now() AND ($1::uuid IS NULL OR user_id = $1) AND ($2::uuid[] IS NULL OR id = ANY($2))
			ORDER BY next_attempt_at LIMIT 1 FOR UPDATE SKIP LOCKED`, userID, only,
		).Scan(&id, &endpoint, &clientID, &secretEnc, &method, &tokenEnc, &hint, &attempts)
		if isNoRows(e) {
			return nil
		}
		if e != nil {
			return e
		}
		tried = true
		secret, e1 := s.seal.open(secretEnc, aadRevocation(id, "client_secret"))
		token, e2 := s.seal.open(tokenEnc, aadRevocation(id, "token"))
		if e1 != nil || e2 != nil {
			slog.WarnContext(ctx, "connectors: queued revocation unreadable; dropped", "revocation_id", id)
			_, e := tx.Exec(ctx, `DELETE FROM connector_revocations WHERE id = $1`, id)
			return e
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		rerr := s.revokeToken(rctx, endpoint, oauthClient{ID: clientID, Secret: secret, AuthMethod: method}, token, hint)
		cancel()
		if rerr == nil || attempts+1 >= maxRevokeAttempts {
			if rerr != nil {
				slog.WarnContext(ctx, "connectors: revocation gave up", "revocation_id", id, "attempts", attempts+1, "err", rerr)
			}
			ok = rerr == nil
			_, e := tx.Exec(ctx, `DELETE FROM connector_revocations WHERE id = $1`, id)
			return e
		}
		backoff := time.Duration(1<<min(attempts, 8)) * time.Minute
		_, e = tx.Exec(ctx, `UPDATE connector_revocations SET attempts = attempts + 1, next_attempt_at = now() + make_interval(secs => $2)
			WHERE id = $1`, id, backoff.Seconds())
		return e
	})
	return tried, ok, err
}
