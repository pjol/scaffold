// Package audit records admin mutations in admin_audit. Every admin write
// endpoint calls Log in the same transaction as the change.
package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
)

// Log writes one audit row. data should be small (ids, before/after values).
func Log(ctx context.Context, q db.Querier, adminID uuid.UUID, action, targetType, targetID string, data any) error {
	if data == nil {
		data = map[string]any{}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO admin_audit (admin_id, action, target_type, target_id, data)
		VALUES ($1, $2, $3, $4, $5)`, adminID, action, targetType, targetID, b)
	return err
}
