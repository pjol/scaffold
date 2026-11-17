// Package keys holds AAuth signing keys per role (a resource's, a PS's, an SS's): generated on first need
// (Ed25519), encrypted at rest with AES-256-GCM under AAUTH_KEY_ENCRYPTION_SECRET,
// rotated with overlap, and published as a JWK set. Every instance reads the same rows,
// so a rotation on one reaches the others within the cache lifetime.
//
//	store, err := keys.New(pool, keys.SecretFromEnv())
//	signer, err := store.Role(keys.RoleResource).Signer(ctx)
//	jwks, err := store.Role(keys.RoleResource).PublicKeys(ctx)
//
// Copied from Sayso's backend/internal/aauth/keys with RoleResource added and the
// app's own development secret and HKDF label; keep the two in step.
package keys

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/aauth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/jobs"
)

// The roles a key can serve.
const (
	RolePS       = "ps"
	RoleSS       = "ss"
	RoleResource = "resource"
)

const (
	// SecretEnvVar names the encryption secret's variable.
	SecretEnvVar = "AAUTH_KEY_ENCRYPTION_SECRET"
	// MinSecretBytes is the shortest secret New accepts.
	MinSecretBytes = 32

	// devSecret is the development fallback. SecretFromEnv refuses it everywhere else.
	devSecret = "scaffoldapp-development-only-key-encryption-secret"

	sealVersion = 1
	hkdfInfo    = "scaffoldapp aauth signing key v1"

	// PrePublish is how long a rotated-in key is published before it signs. It beats
	// the verifiers' once-per-minute JWKS floor, so none of them sees an unknown kid
	// it can't refresh for.
	PrePublish = 2 * time.Minute
	// Overlap is how long a rotated-out key stays published. Person and auth tokens
	// live at most an hour; the rest is margin for clock differences and a slow poll.
	Overlap = 2 * time.Hour

	cacheTTL = 30 * time.Second

	// firstNeedSkew is how far ahead of this instance's clock a key made a moment ago may be dated.

	firstNeedSkew = time.Minute
)

// ErrUnknownRole is returned for a role other than RolePS, RoleSS and RoleResource.
var ErrUnknownRole = errors.New("keys: unknown role")

// SecretFromEnv reads AAUTH_KEY_ENCRYPTION_SECRET. Outside development the variable is
// required and the development fallback is refused; either failure is recorded with
// config.Problem, so the boot fails at config.Check. In development an unset variable
// yields the fallback, so the stack runs with no setup.
func SecretFromEnv() []byte {
	v := config.RequiredOutsideDev(SecretEnvVar, devSecret)
	if config.String("APP_ENV", "development") != "development" {
		if v == devSecret {
			config.Problem("%s must not be the development fallback outside development", SecretEnvVar)
			return nil
		}
		if v == "" {
			return nil
		}
	}
	if len(v) < MinSecretBytes {
		config.Problem("%s must be at least %d bytes", SecretEnvVar, MinSecretBytes)
		return nil
	}
	return []byte(v)
}

// Store is the keys of every role. It is safe for concurrent use.
type Store struct {
	pool  *pgxpool.Pool
	aead  cipher.AEAD
	clock func() time.Time

	mu    sync.Mutex
	roles map[string]*RoleKeys
}

// New returns a Store. The secret must be at least MinSecretBytes long.
func New(pool *pgxpool.Pool, encryptionSecret []byte) (*Store, error) {
	if pool == nil {
		return nil, errors.New("keys: nil pool")
	}
	if len(encryptionSecret) < MinSecretBytes {
		return nil, fmt.Errorf("keys: %s must be at least %d bytes", SecretEnvVar, MinSecretBytes)
	}
	derived, err := hkdf.Key(sha256.New, encryptionSecret, nil, hkdfInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("keys: derive key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("keys: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keys: gcm: %w", err)
	}
	return &Store{pool: pool, aead: aead, clock: time.Now, roles: map[string]*RoleKeys{}}, nil
}

// SetClock replaces the store's clock, for tests.
func (s *Store) SetClock(clock func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = clock
	for _, r := range s.roles {
		r.mu.Lock()
		r.signer, r.public = nil, nil
		r.mu.Unlock()
	}
}

func (s *Store) now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clock()
}

// Role returns the keys of one role ("ps", "ss" or "resource"). An unknown role returns a RoleKeys
// whose methods answer ErrUnknownRole.
func (s *Store) Role(role string) *RoleKeys {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.roles[role]
	if !ok {
		r = &RoleKeys{store: s, role: role}
		s.roles[role] = r
	}
	return r
}

// RoleKeys is the key set of one role.
type RoleKeys struct {
	store *Store
	role  string

	mu          sync.Mutex
	signer      aauth.Signer
	signerUntil time.Time
	public      []aauth.JWK
	publicUntil time.Time
}

func (k *RoleKeys) checkRole() error {
	if k.role != RolePS && k.role != RoleSS && k.role != RoleResource {
		return fmt.Errorf("%w: %q", ErrUnknownRole, k.role)
	}
	return nil
}

// Signer returns the key currently signing for this role, generating the first one if
// there is none.
func (k *RoleKeys) Signer(ctx context.Context) (aauth.Signer, error) {
	if err := k.checkRole(); err != nil {
		return nil, err
	}
	now := k.store.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.signer != nil && now.Before(k.signerUntil) {
		return k.signer, nil
	}
	signer, err := k.loadSigner(ctx, now)
	if err != nil {
		return nil, err
	}
	k.signer, k.signerUntil = signer, now.Add(cacheTTL)
	return signer, nil
}

// PublicKeys returns every key that should be published: the signing key, a key about
// to sign, and keys in their overlap. The list is never empty.
func (k *RoleKeys) PublicKeys(ctx context.Context) ([]aauth.JWK, error) {
	if err := k.checkRole(); err != nil {
		return nil, err
	}
	now := k.store.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.public != nil && now.Before(k.publicUntil) {
		return append([]aauth.JWK(nil), k.public...), nil
	}
	if _, err := k.loadSigner(ctx, now); err != nil {
		return nil, err
	}
	rows, err := k.store.pool.Query(ctx, `
		SELECT public_jwk FROM aauth_signing_keys
		 WHERE role = $1 AND (retire_at IS NULL OR retire_at > $2)
		 ORDER BY not_before DESC, created_at DESC`, k.role, now)
	if err != nil {
		return nil, fmt.Errorf("keys: list %s: %w", k.role, err)
	}
	defer rows.Close()
	out := []aauth.JWK{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("keys: scan %s: %w", k.role, err)
		}
		var key aauth.JWK
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, fmt.Errorf("keys: decode %s public key: %w", k.role, err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keys: list %s: %w", k.role, err)
	}
	k.public, k.publicUntil = out, now.Add(cacheTTL)
	return append([]aauth.JWK(nil), out...), nil
}

// Rotate brings in a new key: published now, signing after PrePublish. Keys already in
// service stop being published Overlap after that. It returns the new key's kid.
func (k *RoleKeys) Rotate(ctx context.Context) (string, error) {
	if err := k.checkRole(); err != nil {
		return "", err
	}
	now := k.store.now()
	var kid string
	err := db.WithTx(ctx, k.store.pool, func(tx pgx.Tx) error {
		if err := k.lock(ctx, tx); err != nil {
			return err
		}
		starts := now.Add(PrePublish)
		if _, err := tx.Exec(ctx, `
			UPDATE aauth_signing_keys SET retire_at = $3
			 WHERE role = $1 AND retire_at IS NULL AND not_before <= $2`,
			k.role, now, starts.Add(Overlap)); err != nil {
			return fmt.Errorf("keys: retire %s: %w", k.role, err)
		}
		key, err := k.insert(ctx, tx, starts)
		if err != nil {
			return err
		}
		kid = key.PublicJWK().Kid
		return nil
	})
	if err != nil {
		return "", err
	}
	k.mu.Lock()
	k.signer, k.public = nil, nil
	k.mu.Unlock()
	return kid, nil
}

// Maintain rotates the role's key once it has been signing for maxAge (zero never
// rotates) and wipes the private part of retired keys. Run it from a periodic job.
func (k *RoleKeys) Maintain(ctx context.Context, maxAge time.Duration) error {
	if err := k.checkRole(); err != nil {
		return err
	}
	now := k.store.now()
	if _, err := k.store.pool.Exec(ctx, `
		UPDATE aauth_signing_keys SET private_enc = NULL
		 WHERE role = $1 AND retire_at <= $2 AND private_enc IS NOT NULL`, k.role, now); err != nil {
		return fmt.Errorf("keys: wipe %s: %w", k.role, err)
	}
	if maxAge <= 0 {
		return nil
	}
	var newest *time.Time
	if err := k.store.pool.QueryRow(ctx, `
		SELECT max(not_before) FROM aauth_signing_keys WHERE role = $1`, k.role).Scan(&newest); err != nil {
		return fmt.Errorf("keys: newest %s: %w", k.role, err)
	}
	if newest == nil || now.Sub(*newest) < maxAge {
		return nil
	}
	_, err := k.Rotate(ctx)
	return err
}

// loadSigner reads the signing key, generating the first one under an advisory lock so
// instances racing at first need agree on one key.
func (k *RoleKeys) loadSigner(ctx context.Context, now time.Time) (aauth.Signer, error) {
	key, err := k.readSigner(ctx, k.store.pool, now)
	if err != nil {
		return nil, err
	}
	if key != nil {
		return key, nil
	}
	err = db.WithTx(ctx, k.store.pool, func(tx pgx.Tx) error {
		if err := k.lock(ctx, tx); err != nil {
			return err
		}
		// now was read before waiting on the lock, and another instance's clock may be ahead of ours, so the key it
		// just made can be dated after now. Allowing for that here keeps two instances from making one key each.
		if key, err = k.readSigner(ctx, tx, now.Add(firstNeedSkew)); err != nil || key != nil {
			return err
		}
		key, err = k.insert(ctx, tx, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (k *RoleKeys) readSigner(ctx context.Context, q db.Querier, now time.Time) (*aauth.PrivateKey, error) {
	var kid string
	var sealed []byte
	err := q.QueryRow(ctx, `
		SELECT kid, private_enc FROM aauth_signing_keys
		 WHERE role = $1 AND not_before <= $2 AND private_enc IS NOT NULL
		   AND (retire_at IS NULL OR retire_at > $2)
		 ORDER BY not_before DESC, created_at DESC LIMIT 1`, k.role, now).Scan(&kid, &sealed)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keys: read %s: %w", k.role, err)
	}
	plain, err := k.store.open(k.role, kid, sealed)
	if err != nil {
		return nil, err
	}
	key, err := aauth.ParsePrivateJWK(plain)
	if err != nil {
		return nil, fmt.Errorf("keys: parse %s key %s: %w", k.role, kid, err)
	}
	return key, nil
}

func (k *RoleKeys) insert(ctx context.Context, tx pgx.Tx, notBefore time.Time) (*aauth.PrivateKey, error) {
	key, err := aauth.GenerateKey(aauth.AlgEd25519, "")
	if err != nil {
		return nil, fmt.Errorf("keys: generate: %w", err)
	}
	public := key.PublicJWK()
	publicJSON, err := json.Marshal(public)
	if err != nil {
		return nil, fmt.Errorf("keys: encode public key: %w", err)
	}
	private, err := key.MarshalPrivateJWK()
	if err != nil {
		return nil, fmt.Errorf("keys: encode private key: %w", err)
	}
	sealed, err := k.store.seal(k.role, public.Kid, private)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO aauth_signing_keys (role, kid, alg, public_jwk, private_enc, not_before)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		k.role, public.Kid, public.Alg, publicJSON, sealed, notBefore); err != nil {
		return nil, fmt.Errorf("keys: insert %s: %w", k.role, err)
	}
	return key, nil
}

func (k *RoleKeys) lock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobs.LockKey("aauth.keys."+k.role))
	if err != nil {
		return fmt.Errorf("keys: lock %s: %w", k.role, err)
	}
	return nil
}

func (s *Store) seal(role, kid string, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("keys: nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plain)+s.aead.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plain, additionalData(role, kid)), nil
}

func (s *Store) open(role, kid string, sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < 1+n || sealed[0] != sealVersion {
		return nil, fmt.Errorf("keys: %s key %s has an unknown seal format", role, kid)
	}
	plain, err := s.aead.Open(nil, sealed[1:1+n], sealed[1+n:], additionalData(role, kid))
	if err != nil {
		return nil, fmt.Errorf("keys: decrypt %s key %s: wrong %s, or the row was altered", role, kid, SecretEnvVar)
	}
	return plain, nil
}

func additionalData(role, kid string) []byte { return []byte(role + "\x00" + kid) }
