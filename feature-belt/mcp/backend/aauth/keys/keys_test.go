package keys

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/aauth"
	"scaffoldapp/backend/internal/db"
)

// testPool gives a test its own migrated database on TEST_DATABASE_URL (a role
// that may CREATE DATABASE), dropped on cleanup; unset, the test is skipped.
func testPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := fmt.Sprintf("aauth_keys_test_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	db.UseUTC(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

var testSecret = []byte("0123456789abcdef0123456789abcdef")

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestNewRefusesShortSecret(t *testing.T) {
	pool := testPool(t)
	if _, err := New(pool, []byte("short")); err == nil {
		t.Fatal("New accepted a short secret")
	}
	if _, err := New(pool, nil); err == nil {
		t.Fatal("New accepted no secret")
	}
}

func TestUnknownRole(t *testing.T) {
	store, err := New(testPool(t), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Role("as").Signer(context.Background()); err == nil {
		t.Fatal("Signer accepted an unknown role")
	}
	if _, err := store.Role("as").PublicKeys(context.Background()); err == nil {
		t.Fatal("PublicKeys accepted an unknown role")
	}
}

func TestGeneratedOnFirstNeedPerRole(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := store.Role(RolePS).Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := store.Role(RoleSS).Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ps.PublicJWK().Kid == ss.PublicJWK().Kid {
		t.Fatal("roles share a key")
	}
	if ps.PublicJWK().Alg != string(aauth.AlgEd25519) {
		t.Fatalf("alg = %q, want Ed25519", ps.PublicJWK().Alg)
	}
	// A second store on the same database reads the same key: restart safe.
	again, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	same, err := again.Role(RolePS).Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if same.PublicJWK().Kid != ps.PublicJWK().Kid {
		t.Fatal("another instance generated a second key")
	}
	msg := []byte("hello")
	sig, err := same.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ps.PublicJWK().Verify(msg, sig); err != nil {
		t.Fatalf("signature from the stored key did not verify: %v", err)
	}
	public, err := again.Role(RolePS).PublicKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].Kid != ps.PublicJWK().Kid || public[0].Alg == "" {
		t.Fatalf("public keys = %+v", public)
	}
}

func TestFirstNeedRaceYieldsOneKey(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	var wg sync.WaitGroup
	kids := make([]string, 8)
	errs := make([]error, 8)
	for i := range kids {
		wg.Go(func() {
			store, err := New(pool, testSecret)
			if err != nil {
				errs[i] = err
				return
			}
			s, err := store.Role(RolePS).Signer(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			kids[i] = s.PublicJWK().Kid
		})
	}
	wg.Wait()
	for i := range kids {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if kids[i] != kids[0] {
			t.Fatalf("instances disagree: %q vs %q", kids[i], kids[0])
		}
	}
}

func TestPrivateKeyEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := store.Role(RolePS).Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := signer.(*aauth.PrivateKey)
	plain, err := key.MarshalPrivateJWK()
	if err != nil {
		t.Fatal(err)
	}
	var sealed, public []byte
	if err := pool.QueryRow(ctx, `SELECT private_enc, public_jwk::text::bytea FROM aauth_signing_keys WHERE kid = $1`,
		key.PublicJWK().Kid).Scan(&sealed, &public); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) || bytes.Contains(sealed, []byte(`"d"`)) {
		t.Fatal("private key is readable in the row")
	}
	if bytes.Contains(public, []byte(`"d"`)) {
		t.Fatal("the public column holds the private part")
	}
	// The wrong secret can't open it.
	other, err := New(pool, []byte("another-secret-another-secret-another"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Role(RolePS).Signer(ctx); err == nil {
		t.Fatal("wrong secret opened the key")
	}
	// A row moved to another role fails, because role and kid are additional data.
	if _, err := pool.Exec(ctx, `UPDATE aauth_signing_keys SET role = 'ss' WHERE kid = $1`, key.PublicJWK().Kid); err != nil {
		t.Fatal(err)
	}
	fresh, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Role(RoleSS).Signer(ctx); err == nil {
		t.Fatal("a key moved to another role still opened")
	}
}

func TestRotationWithOverlap(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	clock := &fakeClock{t: time.Now().UTC()}
	store, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	store.SetClock(clock.now)
	role := store.Role(RolePS)

	first, err := role.Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstKid := first.PublicJWK().Kid
	newKid, err := role.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if newKid == firstKid {
		t.Fatal("rotation kept the kid")
	}

	public := func() map[string]bool {
		t.Helper()
		store.SetClock(clock.now) // drop the role's cache
		keys, err := role.PublicKeys(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, k := range keys {
			out[k.Kid] = true
		}
		return out
	}
	signing := func() string {
		t.Helper()
		store.SetClock(clock.now)
		s, err := role.Signer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s.PublicJWK().Kid
	}

	// Published before it signs, so verifiers see it in time.
	if got := public(); !got[firstKid] || !got[newKid] {
		t.Fatalf("during pre-publication: %v", got)
	}
	if got := signing(); got != firstKid {
		t.Fatalf("signing with %s before pre-publication ended", got)
	}
	clock.advance(PrePublish + time.Second)
	if got := signing(); got != newKid {
		t.Fatalf("still signing with %s after pre-publication", got)
	}
	// The old key stays published through the overlap, then leaves.
	if got := public(); !got[firstKid] || !got[newKid] {
		t.Fatalf("during overlap: %v", got)
	}
	clock.advance(Overlap)
	if got := public(); got[firstKid] || !got[newKid] {
		t.Fatalf("after overlap: %v", got)
	}
	if err := role.Maintain(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var wiped bool
	if err := pool.QueryRow(ctx, `SELECT private_enc IS NULL FROM aauth_signing_keys WHERE kid = $1`, firstKid).Scan(&wiped); err != nil {
		t.Fatal(err)
	}
	if !wiped {
		t.Fatal("retired key kept its private part")
	}
}

func TestMaintainRotatesByAge(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	clock := &fakeClock{t: time.Now().UTC()}
	store, err := New(pool, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	store.SetClock(clock.now)
	role := store.Role(RoleSS)
	first, err := role.Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := role.Maintain(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aauth_signing_keys WHERE role = 'ss'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rotated a fresh key: %d rows", n)
	}
	clock.advance(time.Hour + time.Minute)
	if err := role.Maintain(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aauth_signing_keys WHERE role = 'ss'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("did not rotate an old key: %d rows", n)
	}
	store.SetClock(clock.now)
	s, err := role.Signer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.PublicJWK().Kid != first.PublicJWK().Kid {
		t.Fatal("rotated-in key signed during pre-publication")
	}
}

func TestSecretFromEnvRefusesFallbackInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv(SecretEnvVar, "")
	if got := SecretFromEnv(); got != nil {
		t.Fatal("production yielded a secret with none set")
	}
	t.Setenv(SecretEnvVar, devSecret)
	if got := SecretFromEnv(); got != nil {
		t.Fatal("production accepted the development fallback")
	}
	t.Setenv(SecretEnvVar, string(testSecret))
	if got := SecretFromEnv(); !bytes.Equal(got, testSecret) {
		t.Fatal("production refused a real secret")
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv(SecretEnvVar, "")
	if got := SecretFromEnv(); len(got) < MinSecretBytes {
		t.Fatal("development has no fallback")
	}
}
