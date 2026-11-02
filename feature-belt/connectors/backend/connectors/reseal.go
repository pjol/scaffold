package connectors

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Key rotation. A value sealed with CONNECTORS_ENCRYPTION_KEY_PREVIOUS still
// opens, but nothing else moves it to the current key: refresh tokens that
// never rotate, client secrets and pre-registered secrets would stay under
// the old key, and removing it would break every such connection for good.
// While a previous key is set, this job re-seals each stored secret still
// under one, a batch at a time, and logs once when none is left; only then
// is it safe to remove the previous key.

type sealedColumn struct {
	table, column string
	aad           func(id uuid.UUID) string
}

// sealedColumns is every sealed column with its additional data. Table and
// column names are constants, never input.
var sealedColumns = []sealedColumn{
	{"connectors", "oauth_client_secret_enc", func(id uuid.UUID) string { return aadConnector(id) }},
	{"connector_connections", "access_token_enc", func(id uuid.UUID) string { return aadConnection(id, "access") }},
	{"connector_connections", "refresh_token_enc", func(id uuid.UUID) string { return aadConnection(id, "refresh") }},
	{"connector_connections", "client_secret_enc", func(id uuid.UUID) string { return aadConnection(id, "client_secret") }},
	{"connector_oauth_clients", "client_secret_enc", func(id uuid.UUID) string { return aadClient(id) }},
	{"connector_oauth_flows", "code_verifier_enc", func(id uuid.UUID) string { return aadFlow(id, "verifier") }},
	{"connector_oauth_flows", "client_secret_enc", func(id uuid.UUID) string { return aadFlow(id, "client_secret") }},
	{"connector_oauth_flows", "access_token_enc", func(id uuid.UUID) string { return aadFlow(id, "access") }},
	{"connector_oauth_flows", "refresh_token_enc", func(id uuid.UUID) string { return aadFlow(id, "refresh") }},
	{"connector_revocations", "client_secret_enc", func(id uuid.UUID) string { return aadRevocation(id, "client_secret") }},
	{"connector_revocations", "token_enc", func(id uuid.UUID) string { return aadRevocation(id, "token") }},
	{"connector_platform_connections", "access_token_enc", func(id uuid.UUID) string { return aadPlatform(id, "access") }},
	{"connector_platform_connections", "refresh_token_enc", func(id uuid.UUID) string { return aadPlatform(id, "refresh") }},
	{"connector_platform_flows", "code_verifier_enc", func(id uuid.UUID) string { return aadPlatformFlow(id, "verifier") }},
	{"connector_platform_flows", "access_token_enc", func(id uuid.UUID) string { return aadPlatformFlow(id, "access") }},
	{"connector_platform_flows", "refresh_token_enc", func(id uuid.UUID) string { return aadPlatformFlow(id, "refresh") }},
}

const resealBatch = 200

// resealReported is set once the "previous key can go" line is logged.
var resealReported atomic.Bool

// previousKeyIDs are the key ids in the ring other than the current one.
func (s *sealer) previousKeyIDs() [][]byte {
	var out [][]byte
	for id := range s.keys {
		if id != s.current {
			out = append(out, append([]byte(nil), id[:]...))
		}
	}
	return out
}

// resealPrevious is the connectors.reseal job: up to resealBatch values per
// column move from a previous key to the current one. A compare-and-set
// update never overwrites a value that changed meanwhile.
func (s *Service) resealPrevious(ctx context.Context) error {
	old := s.seal.previousKeyIDs()
	if len(old) == 0 {
		return nil
	}
	moved, failed, more := 0, 0, false
	for _, sc := range sealedColumns {
		rows, err := s.d.Pool.Query(ctx, `SELECT id, `+sc.column+` FROM `+sc.table+`
			WHERE `+sc.column+` IS NOT NULL AND substring(`+sc.column+` FROM 2 FOR 4) = ANY($1::bytea[])
			LIMIT $2`, old, resealBatch)
		if err != nil {
			return err
		}
		type sealedRow struct {
			id     uuid.UUID
			sealed []byte
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (sealedRow, error) {
			var x sealedRow
			err := r.Scan(&x.id, &x.sealed)
			return x, err
		})
		if err != nil {
			return err
		}
		more = more || len(list) == resealBatch
		for _, x := range list {
			plain, err := s.seal.open(x.sealed, sc.aad(x.id))
			if err != nil {
				failed++
				continue
			}
			tag, err := s.d.Pool.Exec(ctx, `UPDATE `+sc.table+` SET `+sc.column+` = $2 WHERE id = $1 AND `+sc.column+` = $3`,
				x.id, s.seal.seal(plain, sc.aad(x.id)), x.sealed)
			if err != nil {
				return err
			}
			moved += int(tag.RowsAffected())
		}
	}
	switch {
	case moved > 0 || failed > 0:
		slog.InfoContext(ctx, "connectors: secrets re-sealed with the current key", "moved", moved, "unreadable", failed)
	case !more && !resealReported.Swap(true):
		slog.InfoContext(ctx, "connectors: every secret is sealed with the current key; CONNECTORS_ENCRYPTION_KEY_PREVIOUS can be removed")
	}
	return nil
}
