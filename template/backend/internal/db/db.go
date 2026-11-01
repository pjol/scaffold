// Package db owns the Postgres pool, embedded migrations, and transaction helpers.
package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/migrations"
)

const (
	connectRetryWindow = 60 * time.Second
	connectPingTimeout = 5 * time.Second
	migrationLockKey   = 7_461_002_001 // arbitrary, stable
)

// Connect opens a pgxpool and retries the first ping for up to a minute, so a
// database that starts alongside the app doesn't fail the deploy.
func Connect(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	UseUTC(cfg)
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = 45 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	deadline := time.Now().Add(connectRetryWindow)
	backoff := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		pctx, cancel := context.WithTimeout(ctx, connectPingTimeout)
		err = pool.Ping(pctx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) {
			break
		}
		slog.Warn("database unreachable, retrying", "attempt", attempt, "retry_in", backoff.String(), "err", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
	pool.Close()
	return nil, fmt.Errorf("ping db (gave up after %s): %w", connectRetryWindow, err)
}

// UseUTC makes every pooled session run in UTC, so SQL date math
// (current_date, ::date casts) agrees with Go's UTC dates, and scans
// timestamptz values as UTC so the API emits "…Z" times regardless of the
// host's zone. Connect applies it.
func UseUTC(cfg *pgxpool.Config) {
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	prev := cfg.AfterConnect
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		if prev != nil {
			return prev(ctx, conn)
		}
		return nil
	}
}

// Migrate applies embedded *.sql files in lexical order (the numbering
// ledger in concrete/database.md makes lexical order dependency order), each
// in its own transaction that first takes a transaction-scoped advisory lock,
// so concurrently booting instances never race and the lock is safe behind
// PgBouncer transaction pooling. Every applied file's sha256 is recorded; a
// changed checksum means someone edited an applied migration, which fails the
// boot when strict (staging, production) and warns otherwise.
func Migrate(ctx context.Context, pool *pgxpool.Pool, strict bool) error {
	lock := func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
			return fmt.Errorf("migration lock: %w", err)
		}
		return nil
	}
	if err := WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := lock(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version    text PRIMARY KEY,
				checksum   text,
				applied_at timestamptz NOT NULL DEFAULT now()
			)`)
		return err
	}); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		applied := false
		err = WithTx(ctx, pool, func(tx pgx.Tx) error {
			if err := lock(tx); err != nil {
				return err
			}
			var recorded *string
			err := tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version = $1`, name).Scan(&recorded)
			switch {
			case err == nil:
				if recorded == nil {
					_, err = tx.Exec(ctx, `UPDATE schema_migrations SET checksum = $2 WHERE version = $1`, name, checksum)
					return err
				}
				if *recorded != checksum {
					msg := fmt.Sprintf("migration %s changed after it was applied; never edit an applied migration, add the next number instead", name)
					if strict {
						return errors.New(msg)
					}
					slog.Warn(msg)
				}
				return nil
			case !IsNoRows(err):
				return err
			}
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`, name, checksum); err != nil {
				return err
			}
			applied = true
			return nil
		})
		if err != nil {
			return err
		}
		if applied {
			slog.Info("migrated", "version", name)
		}
	}
	return nil
}

// Querier is satisfied by *pgxpool.Pool, pgx.Tx and *pgxpool.Conn so query
// helpers can run inside or outside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTx runs fn in a transaction, committing on nil error and rolling back
// otherwise (including on panic, which is re-raised).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsNoRows reports whether err is pgx.ErrNoRows.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports whether err is a unique-constraint violation,
// optionally on the named constraint/index.
func IsUniqueViolation(err error, constraint ...string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return false
	}
	if len(constraint) == 0 {
		return true
	}
	for _, c := range constraint {
		if pg.ConstraintName == c {
			return true
		}
	}
	return false
}

// TryAdvisoryLock runs fn only if this instance wins the advisory lock for
// key — the pattern for singleton background jobs across N instances.
// Returns false when another instance holds the lock.
//
// The lock is transaction-scoped (pg_try_advisory_xact_lock) on a transaction
// held open while fn runs, then ended — so it is released with that
// transaction and never outlives or strays from its server connection, which
// keeps it correct behind PgBouncer transaction pooling. fn does
// its own work on the pool; the lock transaction stays idle meanwhile, so the
// database's idle_in_transaction_session_timeout (if set) must exceed the
// longest worker run.
func TryAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, key int64, fn func(ctx context.Context) error) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // releases the lock; no-op after commit
	var got bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	return true, fn(ctx)
}
