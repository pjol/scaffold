package calendar

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
)

// testPool creates a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE), applies every migration, and drops it afterwards.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("calendar_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

// recordingMailer records sends and fails for recipients in fail. While a
// send runs it reports whether a connection is checked out of the pool.
type recordingMailer struct {
	mu       sync.Mutex
	pool     *pgxpool.Pool
	sent     []string
	fail     map[string]bool
	heldConn bool
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pool.Stat().AcquiredConns() > 0 {
		m.heldConn = true
	}
	if m.fail[msg.To] {
		return errors.New("provider down")
	}
	m.sent = append(m.sent, msg.To)
	return nil
}

// The outbox sends with no transaction or connection held, records each
// result once by claim, retries failures with a backoff, and leaves rows a
// crashed sender claimed until their lease ends.
func TestOutboxClaimsSendOutsideTransaction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mailer := &recordingMailer{pool: pool, fail: map[string]bool{"down@example.test": true}}
	s := &Service{d: &app.Deps{Pool: pool, Mail: mailer}}

	for _, to := range []string{"a@example.test", "down@example.test", "b@example.test"} {
		if err := enqueueMail(ctx, pool, mail.Message{To: to, Subject: "Hi", Text: "Hi"}); err != nil {
			t.Fatal(err)
		}
	}
	// A row another sender claimed (and crashed holding): not due until its lease ends.
	var stuck int64
	if err := pool.QueryRow(ctx, `INSERT INTO calendar_outbox (kind, payload, claim, next_attempt_at)
		VALUES ('mail', '{"To":"stuck@example.test","Subject":"x","Text":"x"}', $1, now() + interval '5 minutes') RETURNING id`, uuid.New()).Scan(&stuck); err != nil {
		t.Fatal(err)
	}

	if err := s.drainOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(mailer.sent); got != "[a@example.test b@example.test]" {
		t.Errorf("sent %s, want a and b in order (the failing one retried later, the claimed one left alone)", got)
	}
	if mailer.heldConn {
		t.Error("a database connection was held while sending")
	}
	rows, _ := pool.Query(ctx, `SELECT payload->>'To' || ':' || attempts || ':' || (claim IS NULL) || ':' || (next_attempt_at > now() + interval '30 seconds')
		FROM calendar_outbox ORDER BY id`)
	left, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	// down: one attempt, released, backing off; stuck: untouched.
	if got := fmt.Sprint(left); got != "[down@example.test:1:true:true stuck@example.test:0:false:true]" {
		t.Errorf("rows left = %s", got)
	}

	// A sender whose claim was taken over records nothing.
	if err := s.recordOutbox(ctx, outboxItem{id: stuck, kind: "mail"}, uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM calendar_outbox WHERE id = $1`, stuck).Scan(&n); err != nil || n != 1 {
		t.Errorf("a stale claim deleted the row: %d %v", n, err)
	}

	// Once the lease ends the row is due again and sent once.
	if _, err := pool.Exec(ctx, `UPDATE calendar_outbox SET next_attempt_at = now() WHERE id = $1`, stuck); err != nil {
		t.Fatal(err)
	}
	if err := s.drainOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(mailer.sent); got != "[a@example.test b@example.test stuck@example.test]" {
		t.Errorf("after the lease: sent %s", got)
	}
}
