package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// objectType is one of the three record types. Every SQL identifier derived
// from it comes from the switch below, never from input.
type objectType string

const (
	objCompany objectType = "company"
	objContact objectType = "contact"
	objDeal    objectType = "deal"
)

var objectTypes = []objectType{objCompany, objContact, objDeal}

func parseObject(s string) (objectType, bool) {
	switch objectType(strings.TrimSpace(s)) {
	case objCompany, "companies":
		return objCompany, true
	case objContact, "contacts":
		return objContact, true
	case objDeal, "deals":
		return objDeal, true
	}
	return "", false
}

func objectByKind(kind string) (objectType, bool) {
	o, ok := strings.CutPrefix(kind, "crm.")
	if !ok {
		return "", false
	}
	switch objectType(o) {
	case objCompany, objContact, objDeal:
		return objectType(o), true
	}
	return "", false
}

func (o objectType) kind() string { return "crm." + string(o) }

func (o objectType) table() string {
	switch o {
	case objCompany:
		return "crm_companies"
	case objContact:
		return "crm_contacts"
	default:
		return "crm_deals"
	}
}

func (o objectType) path() string {
	switch o {
	case objCompany:
		return "companies"
	case objContact:
		return "contacts"
	default:
		return "deals"
	}
}

func (o objectType) label() string { return string(o) }

func (o objectType) labelPlural() string { return o.path() }

func (o objectType) link(id uuid.UUID) string { return "/crm/" + o.path() + "/" + id.String() }

func (o objectType) icon() string {
	switch o {
	case objCompany:
		return "building-2"
	case objContact:
		return "user-round"
	default:
		return "handshake"
	}
}

func (o objectType) titleCase() string { return strings.ToUpper(string(o)[:1]) + string(o)[1:] }

// titleExpr is the SQL display name of a record (alias r).
func (o objectType) titleExpr() string {
	switch o {
	case objContact:
		return `COALESCE(NULLIF(btrim(r.first_name || ' ' || r.last_name), ''), r.emails[1], 'Unnamed contact')`
	default:
		return `r.name`
	}
}

// recordTitle is the display name for access (share dialog, notifications).
func recordTitle(ctx context.Context, q db.Querier, o objectType, id uuid.UUID) (string, error) {
	var t string
	err := q.QueryRow(ctx, `SELECT `+o.titleExpr()+` FROM `+o.table()+` r WHERE r.id = $1`, id).Scan(&t)
	return t, err
}

// ---- references and DTOs ---------------------------------------------------

// UserRef is a person shown by name.
type UserRef struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatar_url"`
}

// TeamRef is an access team.
type TeamRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// Tag is an org tag.
type Tag struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Tone string    `json:"tone"`
}

// RecordRef names a record the caller can open.
type RecordRef struct {
	Type objectType `json:"type"`
	ID   uuid.UUID  `json:"id"`
	Name string     `json:"name"`
}

// StageRef is a deal's stage.
type StageRef struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Probability int       `json:"probability"`
	Outcome     string    `json:"outcome"`
	Position    int       `json:"position"`
}

// Record is what every row carries: the shared columns plus the caller's
// access level. Company, Contact and Deal embed it.
type Record struct {
	ID             uuid.UUID         `json:"id"`
	OrgID          uuid.UUID         `json:"org_id"`
	Type           objectType        `json:"type"`
	Title          string            `json:"title"`
	Owner          *UserRef          `json:"owner"`
	Team           *TeamRef          `json:"team"`
	Tags           []Tag             `json:"tags"`
	Props          map[string]any    `json:"props"`
	PropsDisplay   map[string]string `json:"props_display"`
	CreatedBy      *uuid.UUID        `json:"created_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastActivityAt *time.Time        `json:"last_activity_at"`
	MyLevel        access.Level      `json:"my_level"`

	tagIDs []uuid.UUID
}

func (r *Record) base() *Record { return r }

// Company is a crm_companies row.
type Company struct {
	Record
	Name        string  `json:"name"`
	Domain      *string `json:"domain"`
	Industry    string  `json:"industry"`
	Employees   *int    `json:"employees"`
	Phone       string  `json:"phone"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Description string  `json:"description"`
}

// Contact is a crm_contacts row with its primary company (when visible).
type Contact struct {
	Record
	FirstName      string     `json:"first_name"`
	LastName       string     `json:"last_name"`
	Name           string     `json:"name"`
	Emails         []string   `json:"emails"`
	Phones         []string   `json:"phones"`
	JobTitle       string     `json:"job_title"`
	LifecycleStage string     `json:"lifecycle_stage"`
	City           string     `json:"city"`
	Country        string     `json:"country"`
	Company        *RecordRef `json:"company"`
}

// Deal is a crm_deals row with its stage, pipeline and company (when visible).
type Deal struct {
	Record
	Name           string     `json:"name"`
	Amount         *int64     `json:"amount"`
	Currency       string     `json:"currency"`
	Weighted       *int64     `json:"weighted_amount"`
	PipelineID     uuid.UUID  `json:"pipeline_id"`
	PipelineName   string     `json:"pipeline_name"`
	Stage          StageRef   `json:"stage"`
	CloseDate      *string    `json:"close_date"` // YYYY-MM-DD
	Company        *RecordRef `json:"company"`
	StageEnteredAt time.Time  `json:"stage_entered_at"`
	ClosedAt       *time.Time `json:"closed_at"`
}

// recordish is implemented by *Company, *Contact and *Deal.
type recordish interface{ base() *Record }

// ---- request helpers ---------------------------------------------------------

// bootstrapMiddleware makes sure an org's CRM exists (settings row, the
// workspace access resource and, the first time, a default pipeline) before
// any org-scoped route runs. Mount after orgs.Require.
func (s *Service) bootstrapMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
		if err == nil {
			err = s.bootstrap(r.Context(), orgID)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func orgParam(r *http.Request) uuid.UUID {
	id, _ := uuid.Parse(chi.URLParam(r, "orgID"))
	return id
}

func me(r *http.Request) uuid.UUID { return auth.MustFromContext(r.Context()).UserID }

func idParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, httpx.NotFound("Not found.")
	}
	return id, nil
}

// errNoCRM answers record routes for people without crm.access in the
// record's org (the org routes get the same answer from RequirePermission).
func errNoCRM() *httpx.Error {
	e := httpx.NewError(http.StatusForbidden, "permission_required",
		"You don't have the \"Use the CRM\" permission in this "+orgs.Noun()+". Ask an admin.")
	e.Details = map[string]any{"permission": PermAccess}
	return e
}

// recordOrgAllowed reports whether userID holds crm.access in the record's org.
func (s *Service) recordOrgAllowed(ctx context.Context, o objectType, id, userID uuid.UUID) (bool, error) {
	var orgID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM `+o.table()+` WHERE id = $1`, id).Scan(&orgID); err != nil {
		if db.IsNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return access.Allowed(ctx, s.d.Pool, orgID, userID, PermAccess)
}

// requireRecord checks the caller's level on a record (404 without view, 403
// below min; agent-capped through ctx) and crm.access in its org, and returns
// the record's org and the level.
func (s *Service) requireRecord(ctx context.Context, q db.Querier, userID uuid.UUID, o objectType, id uuid.UUID, min access.Level) (uuid.UUID, access.Level, error) {
	lvl, err := access.Require(ctx, q, userID, o.kind(), id, min)
	if err != nil {
		return uuid.Nil, lvl, err
	}
	var orgID uuid.UUID
	if err := q.QueryRow(ctx, `SELECT org_id FROM `+o.table()+` WHERE id = $1`, id).Scan(&orgID); err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, access.None, httpx.NotFound(o.titleCase() + " not found.")
		}
		return uuid.Nil, access.None, err
	}
	ok, err := access.Allowed(ctx, q, orgID, userID, PermAccess)
	if err != nil {
		return uuid.Nil, access.None, err
	}
	if !ok {
		return uuid.Nil, access.None, errNoCRM()
	}
	return orgID, lvl, nil
}

// requireOrgCRM checks crm.access for org-level work outside the guarded
// routes (agent tools): non-members get 404, members without it 403.
func (s *Service) requireOrgCRM(ctx context.Context, orgID, userID uuid.UUID) error {
	var member bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, orgID, userID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	ok, err := access.Allowed(ctx, s.d.Pool, orgID, userID, PermAccess)
	if err != nil {
		return err
	}
	if !ok {
		return errNoCRM()
	}
	if err := s.bootstrap(ctx, orgID); err != nil {
		return err
	}
	return nil
}

// crmOrgs returns the orgs where userID holds crm.access (search, agent tools).
func (s *Service) crmOrgs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	p, _ := access.PermissionOf(PermAccess)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT m.org_id FROM org_members m
		WHERE m.user_id = $1 AND access_permission_allowed(m.org_id, $1, $2, $3)
		ORDER BY m.created_at, m.org_id`, userID, PermAccess, p.DefaultRoles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func allowedHere(ctx context.Context, q db.Querier, orgID, userID uuid.UUID, key string) bool {
	ok, err := access.Allowed(ctx, q, orgID, userID, key)
	return err == nil && ok
}

func decodeInput(input json.RawMessage, dst any) error {
	if len(input) == 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
