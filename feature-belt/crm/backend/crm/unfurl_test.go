package crm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// The connectors feature offers the CRM's mail capabilities only when the
// module declares them.
func TestCapabilitiesDeclareMail(t *testing.T) {
	keys := map[string]bool{}
	for _, c := range (&Service{}).Capabilities() {
		if c.Module != "crm" || c.Provider == "" || len(c.Scopes) == 0 {
			t.Errorf("incomplete capability %+v", c)
		}
		keys[c.Key] = true
	}
	for _, k := range []string{CapGoogleMailRead, CapGoogleMailSend, CapMicrosoftMailRead, CapMicrosoftMailSend, CapHubSpotRead, CapSalesforceRead} {
		if !keys[k] {
			t.Errorf("capability %s not declared", k)
		}
	}
}

// The package-level Unfurl reaches the service New made: the owner gets the
// card, a member who can't open the record, an agent whose CRM scope is off,
// a deleted record and paths that aren't records get nil.
func TestPackageUnfurl(t *testing.T) {
	w := newWorld(t)
	id := w.deal(w.ann, "Renewal", 120000, "2026-12-01")
	path := "/crm/deals/" + id.String()
	ann, bob := &auth.Identity{UserID: w.ann}, &auth.Identity{UserID: w.bob}

	card, err := Unfurl(w.ctx, ann, path)
	w.must(err)
	if card == nil || card.Kind != KindDeal || card.Title != "Renewal" || card.Link != path || card.At == nil {
		t.Fatalf("owner's card = %+v", card)
	}
	if card, err := Unfurl(w.ctx, bob, path); err != nil || card != nil {
		t.Fatalf("member without access: %+v, %v", card, err)
	}
	for _, p := range []string{"/crm/deals/" + uuid.NewString(), "/crm/deals", "/crm/widgets/" + id.String(), "/docs/" + id.String(), path + "/edit"} {
		if card, err := Unfurl(w.ctx, ann, p); err != nil || card != nil {
			t.Errorf("%s: %+v, %v", p, card, err)
		}
	}

	var hasAgents bool
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT to_regclass('agents_feature_scopes') IS NOT NULL`).Scan(&hasAgents))
	if hasAgents {
		agentCtx := auth.WithAgent(w.ctx, auth.Agent{Via: "mcp", Client: "Test"})
		if card, err := Unfurl(agentCtx, ann, path); err != nil || card == nil {
			t.Fatalf("agent at read: %+v, %v", card, err)
		}
		_, err := w.s.d.Pool.Exec(w.ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, 'crm', 'off')`, w.ann)
		w.must(err)
		if card, err := Unfurl(agentCtx, ann, path); err != nil || card != nil {
			t.Fatalf("agent with crm off: %+v, %v", card, err)
		}
		_, err = w.s.d.Pool.Exec(w.ctx, `DELETE FROM agents_feature_scopes WHERE user_id = $1`, w.ann)
		w.must(err)
	} else {
		t.Log("agents feature not installed: agent scope part skipped")
	}

	w.must(db.WithTx(w.ctx, w.s.d.Pool, func(tx pgx.Tx) error { return deleteRecordTx(w.ctx, tx, objDeal, id) }))
	if card, err := Unfurl(w.ctx, ann, path); err != nil || card != nil {
		t.Fatalf("deleted: %+v, %v", card, err)
	}
}
