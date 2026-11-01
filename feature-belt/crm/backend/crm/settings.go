package crm

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Settings is GET /v1/crm/orgs/{orgID}/settings: the org's CRM settings and
// what the caller may do.
type Settings struct {
	OrgID        uuid.UUID       `json:"org_id"`
	RecordAccess string          `json:"record_access"`
	Currency     string          `json:"currency"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Can          map[string]bool `json:"can"` // manage_settings, export, import, delete
}

func (s *Service) loadSettings(ctx context.Context, q db.Querier, orgID, userID uuid.UUID) (*Settings, error) {
	st := &Settings{OrgID: orgID, Can: map[string]bool{}}
	if err := q.QueryRow(ctx, `SELECT record_access, currency, updated_at FROM crm_settings WHERE org_id = $1`, orgID).
		Scan(&st.RecordAccess, &st.Currency, &st.UpdatedAt); err != nil {
		return nil, err
	}
	for key, perm := range map[string]string{"manage_settings": PermManageSettings, "export": PermExport, "import": PermImport, "delete": PermDelete} {
		st.Can[key] = allowedHere(ctx, q, orgID, userID, perm)
	}
	return st, nil
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.loadSettings(r.Context(), s.d.Pool, orgParam(r), me(r))
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

type settingsReq struct {
	RecordAccess *string `json:"record_access"`
	Currency     *string `json:"currency"`
}

// putSettings: PUT /v1/crm/orgs/{orgID}/settings {record_access?, currency?}
// (crm.manage_settings, audited). Changing record access rewrites the grants
// on the workspace and team resources in one transaction.
func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req settingsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.RecordAccess != nil && !slices.Contains([]string{AccessEveryoneEdits, AccessEveryoneViews, AccessOwnersTeams}, *req.RecordAccess) {
		return httpx.BadRequest("record_access is everyone_edits, everyone_views or owners_teams.")
	}
	if req.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*req.Currency))
		if !currencyRe.MatchString(c) {
			return httpx.BadRequest("Currency must be a 3-letter code like USD.")
		}
		req.Currency = &c
	}
	var shrank bool
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var mode, cur string
		if err := tx.QueryRow(ctx, `SELECT record_access, currency FROM crm_settings WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&mode, &cur); err != nil {
			return err
		}
		data := map[string]any{"before": map[string]any{"record_access": mode, "currency": cur}}
		if req.Currency != nil && *req.Currency != cur {
			if _, err := tx.Exec(ctx, `UPDATE crm_settings SET currency = $2, updated_by = $3, updated_at = now() WHERE org_id = $1`, orgID, *req.Currency, me(r)); err != nil {
				return err
			}
			data["currency"] = *req.Currency
		}
		if req.RecordAccess != nil && *req.RecordAccess != mode {
			if err := applyRecordAccess(ctx, tx, orgID, *req.RecordAccess); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm_settings SET record_access = $2, updated_by = $3, updated_at = now() WHERE org_id = $1`, orgID, *req.RecordAccess, me(r)); err != nil {
				return err
			}
			data["record_access"] = *req.RecordAccess
			shrank = modeGeneral(*req.RecordAccess) < modeGeneral(mode) || *req.RecordAccess == AccessOwnersTeams || mode == AccessOwnersTeams
		}
		if len(data) == 1 {
			return nil
		}
		return audit.Log(ctx, tx, me(r), "crm.settings.update", "org", orgID.String(), data)
	})
	if err != nil {
		return err
	}
	if shrank {
		access.Changed(ctx, workspaceRef(orgID), nil)
	}
	s.settingsChanged(ctx, orgID, "settings")
	st, err := s.loadSettings(ctx, s.d.Pool, orgID, me(r))
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

// applyRecordAccess sets the org grant on the workspace and each team's grant
// on its crm.team resource for a mode.
func applyRecordAccess(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, mode string) error {
	everyone := access.SubjectRef{Type: "org", ID: orgID}
	if lvl := modeGeneral(mode); lvl == access.None {
		if err := access.RemoveGrant(ctx, tx, workspaceRef(orgID), everyone); err != nil {
			return err
		}
	} else if err := access.SetGrant(ctx, tx, workspaceRef(orgID), everyone, lvl, uuid.Nil); err != nil {
		return err
	}
	// Teams that still exist (deleting a team removed its grants already).
	rows, err := tx.Query(ctx, `SELECT r.id FROM access_resources r JOIN access_teams t ON t.id = r.id AND t.org_id = r.org_id
		WHERE r.kind = $1 AND r.org_id = $2`, KindTeam, orgID)
	if err != nil {
		return err
	}
	var teams []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		teams = append(teams, id)
	}
	rows.Close()
	for _, t := range teams {
		if err := setTeamGrant(ctx, tx, t, mode == AccessOwnersTeams); err != nil {
			return err
		}
	}
	return nil
}

// ---- custom properties ---------------------------------------------------------------

// listProperties: GET /v1/crm/orgs/{orgID}/properties?object=contact (every
// object when omitted, as {company: [...], contact: [...], deal: [...]}).
func (s *Service) listProperties(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	if v := r.URL.Query().Get("object"); v != "" {
		o, ok := parseObject(v)
		if !ok {
			return httpx.BadRequest("object is company, contact or deal.")
		}
		list, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]any{"items": list})
		return nil
	}
	out := map[string][]Property{}
	for _, o := range objectTypes {
		list, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
		if err != nil {
			return err
		}
		out[string(o)] = list
	}
	httpx.OK(w, out)
	return nil
}

type propertyReq struct {
	ObjectType string    `json:"object_type"`
	Key        *string   `json:"key"`
	Label      *string   `json:"label"`
	Type       string    `json:"type"`
	Options    *[]Option `json:"options"`
	Position   *int      `json:"position"`
}

var nonKey = regexp.MustCompile(`[^a-z0-9]+`)

func keyFromLabel(label string) string {
	k := strings.Trim(nonKey.ReplaceAllString(strings.ToLower(label), "_"), "_")
	if k == "" || k[0] < 'a' || k[0] > 'z' {
		k = "p_" + k
	}
	if len(k) > 40 {
		k = strings.TrimRight(k[:40], "_")
	}
	return k
}

func cleanOptions(in []Option) ([]Option, error) {
	if len(in) > 200 {
		return nil, httpx.BadRequest("A property can have at most 200 options.")
	}
	out := []Option{}
	seen := map[string]bool{}
	for _, o := range in {
		o.Label = strings.TrimSpace(o.Label)
		o.Value = strings.TrimSpace(o.Value)
		if o.Value == "" {
			o.Value = keyFromLabel(o.Label)
		}
		if o.Label == "" || len([]rune(o.Label)) > 80 || len(o.Value) > 80 {
			return nil, httpx.BadRequest("Give every option a label of up to 80 characters.")
		}
		if seen[o.Value] {
			return nil, httpx.BadRequest("Two options have the same value: " + o.Value + ".")
		}
		seen[o.Value] = true
		out = append(out, o)
	}
	return out, nil
}

// createProperty: POST /v1/crm/orgs/{orgID}/properties {object_type, label, type, key?, options?}
func (s *Service) createProperty(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req propertyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return httpx.BadRequest("object_type is company, contact or deal.")
	}
	label := strings.TrimSpace(deref(req.Label))
	if label == "" || len([]rune(label)) > 60 {
		return httpx.BadRequest("Give the property a label of up to 60 characters.")
	}
	if !slices.Contains(customTypes, req.Type) {
		return httpx.BadRequest("type is one of: " + strings.Join(customTypes, ", ") + ".")
	}
	key := keyFromLabel(label)
	if req.Key != nil && strings.TrimSpace(*req.Key) != "" {
		key = strings.TrimSpace(*req.Key)
	}
	if !propKeyRe.MatchString(key) {
		return httpx.BadRequest("The key must start with a letter and use a–z, 0–9 and _ (up to 40).")
	}
	for _, b := range builtinProps(o) {
		if b.Key == key {
			return httpx.Conflict("“" + key + "” is a built-in field. Choose another key.")
		}
	}
	opts := []Option{}
	if req.Type == "select" || req.Type == "multi_select" {
		if req.Options == nil || len(*req.Options) == 0 {
			return httpx.BadRequest("Add at least one option.")
		}
		var err error
		if opts, err = cleanOptions(*req.Options); err != nil {
			return err
		}
	}
	optsJSON, _ := json.Marshal(opts)
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_properties WHERE org_id = $1 AND object_type = $2`, orgID, string(o)).Scan(&n); err != nil {
			return err
		}
		if n >= 200 {
			return httpx.BadRequest("An object can have at most 200 custom properties.")
		}
		err := tx.QueryRow(ctx, `INSERT INTO crm_properties (org_id, object_type, key, label, type, options, position, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, orgID, string(o), key, label, req.Type, optsJSON, n, me(r)).Scan(&id)
		if db.IsUniqueViolation(err, "crm_properties_key_key") {
			return httpx.Conflict("A property with the key “" + key + "” already exists.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.property.create", "crm.property", id.String(), map[string]any{"org_id": orgID, "object": o, "key": key, "type": req.Type})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "properties")
	return s.respondProperty(w, r, orgID, o, key, http.StatusCreated)
}

func (s *Service) respondProperty(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, o objectType, key string, status int) error {
	list, err := s.loadProperties(r.Context(), s.d.Pool, orgID, o)
	if err != nil {
		return err
	}
	if p, ok := findProp(list, key); ok {
		httpx.JSON(w, status, p)
		return nil
	}
	return httpx.NotFound("Property not found.")
}

// updateProperty: PATCH /v1/crm/orgs/{orgID}/properties/{propertyID} {label?, options?, position?}.
// The type and key never change; removed options are cleared from records.
func (s *Service) updateProperty(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	pid, err := idParam(r, "propertyID")
	if err != nil {
		return err
	}
	var req propertyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Type != "" || req.Key != nil || req.ObjectType != "" {
		return httpx.BadRequest("A property's type, key and object can't change. Add a new property instead.")
	}
	var o objectType
	var key string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var typ, ot, label string
		var optsRaw []byte
		if err := tx.QueryRow(ctx, `SELECT object_type, key, type, label, options FROM crm_properties WHERE id = $1 AND org_id = $2 FOR UPDATE`, pid, orgID).
			Scan(&ot, &key, &typ, &label, &optsRaw); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Property not found.")
			}
			return err
		}
		o = objectType(ot)
		data := map[string]any{"org_id": orgID, "key": key}
		if req.Label != nil {
			l := strings.TrimSpace(*req.Label)
			if l == "" || len([]rune(l)) > 60 {
				return httpx.BadRequest("Give the property a label of up to 60 characters.")
			}
			if _, err := tx.Exec(ctx, `UPDATE crm_properties SET label = $2, updated_at = now() WHERE id = $1`, pid, l); err != nil {
				return err
			}
			data["label"] = l
		}
		if req.Position != nil {
			if _, err := tx.Exec(ctx, `UPDATE crm_properties SET position = $2, updated_at = now() WHERE id = $1`, pid, *req.Position); err != nil {
				return err
			}
		}
		if req.Options != nil {
			if typ != "select" && typ != "multi_select" {
				return httpx.BadRequest("Only select properties have options.")
			}
			opts, err := cleanOptions(*req.Options)
			if err != nil {
				return err
			}
			if len(opts) == 0 {
				return httpx.BadRequest("Keep at least one option.")
			}
			b, _ := json.Marshal(opts)
			if _, err := tx.Exec(ctx, `UPDATE crm_properties SET options = $2, updated_at = now() WHERE id = $1`, pid, b); err != nil {
				return err
			}
			values := make([]string, len(opts))
			for i, op := range opts {
				values[i] = op.Value
			}
			// Values no longer offered are cleared.
			if typ == "select" {
				_, err = tx.Exec(ctx, `UPDATE `+o.table()+` SET props = props - $2 WHERE org_id = $1 AND props ? $2 AND NOT (props->>$2 = ANY ($3))`, orgID, key, values)
			} else {
				_, err = tx.Exec(ctx, `UPDATE `+o.table()+` SET props = CASE WHEN x.kept = '[]'::jsonb THEN props - $2 ELSE jsonb_set(props, ARRAY[$2], x.kept) END
					FROM (SELECT id, COALESCE((SELECT jsonb_agg(v) FROM jsonb_array_elements_text(props->$2) v WHERE v = ANY ($3)), '[]'::jsonb) AS kept
					      FROM `+o.table()+` WHERE org_id = $1 AND props ? $2) x
					WHERE `+o.table()+`.id = x.id`, orgID, key, values)
			}
			if err != nil {
				return err
			}
			data["options"] = values
		}
		return audit.Log(ctx, tx, me(r), "crm.property.update", "crm.property", pid.String(), data)
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "properties")
	return s.respondProperty(w, r, orgID, o, key, http.StatusOK)
}

// deleteProperty: DELETE /v1/crm/orgs/{orgID}/properties/{propertyID}; its
// values are removed from every record.
func (s *Service) deleteProperty(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	pid, err := idParam(r, "propertyID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var ot, key, label string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_properties WHERE id = $1 AND org_id = $2 RETURNING object_type, key, label`, pid, orgID).Scan(&ot, &key, &label); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Property not found.")
			}
			return err
		}
		o := objectType(ot)
		if _, err := tx.Exec(ctx, `UPDATE `+o.table()+` SET props = props - $2 WHERE org_id = $1 AND props ? $2`, orgID, key); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.property.delete", "crm.property", pid.String(), map[string]any{"org_id": orgID, "object": o, "key": key, "label": label})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "properties")
	httpx.NoContent(w)
	return nil
}

// ---- tags ------------------------------------------------------------------------------

var tagTones = []string{"neutral", "primary", "accent", "success", "warning", "danger", "info"}

func (s *Service) listTags(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.d.Pool.Query(r.Context(), `SELECT id, name, tone FROM crm_tags WHERE org_id = $1 ORDER BY lower(name), id LIMIT 1000`, orgParam(r))
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Tag])
	if err != nil {
		return err
	}
	if list == nil {
		list = []Tag{}
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type tagReq struct {
	Name *string `json:"name"`
	Tone *string `json:"tone"`
}

func cleanTag(req tagReq) (string, string, error) {
	name := strings.TrimSpace(deref(req.Name))
	tone := "neutral"
	if req.Tone != nil {
		tone = *req.Tone
	}
	if !slices.Contains(tagTones, tone) {
		return "", "", httpx.BadRequest("tone is one of: " + strings.Join(tagTones, ", ") + ".")
	}
	return name, tone, nil
}

// createTag: POST /v1/crm/orgs/{orgID}/tags {name, tone?} — anyone who uses
// the CRM can add a tag while tagging; an existing name returns that tag.
func (s *Service) createTag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req tagReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, tone, err := cleanTag(req)
	if err != nil {
		return err
	}
	if name == "" || len([]rune(name)) > 40 {
		return httpx.BadRequest("Give the tag a name of up to 40 characters.")
	}
	var t Tag
	err = s.d.Pool.QueryRow(ctx, `SELECT id, name, tone FROM crm_tags WHERE org_id = $1 AND lower(name) = lower($2)`, orgID, name).Scan(&t.ID, &t.Name, &t.Tone)
	if err == nil {
		httpx.OK(w, t)
		return nil
	}
	if !db.IsNoRows(err) {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_tags WHERE org_id = $1`, orgID).Scan(&n); err != nil {
		return err
	}
	if n >= 1000 {
		return httpx.BadRequest(orgNounA() + " can have at most 1,000 tags.")
	}
	err = s.d.Pool.QueryRow(ctx, `INSERT INTO crm_tags (org_id, name, tone, created_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, lower(name)) DO UPDATE SET updated_at = crm_tags.updated_at RETURNING id, name, tone`, orgID, name, tone, me(r)).Scan(&t.ID, &t.Name, &t.Tone)
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "tags")
	httpx.JSON(w, http.StatusCreated, t)
	return nil
}

// updateTag: PATCH /v1/crm/orgs/{orgID}/tags/{tagID} {name?, tone?} (settings).
func (s *Service) updateTag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "tagID")
	if err != nil {
		return err
	}
	var req tagReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var t Tag
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id, name, tone FROM crm_tags WHERE id = $1 AND org_id = $2 FOR UPDATE`, id, orgID).Scan(&t.ID, &t.Name, &t.Tone); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Tag not found.")
			}
			return err
		}
		before := t
		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" || len([]rune(n)) > 40 {
				return httpx.BadRequest("Give the tag a name of up to 40 characters.")
			}
			t.Name = n
		}
		if req.Tone != nil {
			if !slices.Contains(tagTones, *req.Tone) {
				return httpx.BadRequest("tone is one of: " + strings.Join(tagTones, ", ") + ".")
			}
			t.Tone = *req.Tone
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_tags SET name = $2, tone = $3, updated_at = now() WHERE id = $1`, id, t.Name, t.Tone); err != nil {
			if db.IsUniqueViolation(err, "crm_tags_name_key") {
				return httpx.Conflict("A tag with that name already exists.")
			}
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.tag.update", "crm.tag", id.String(), map[string]any{"org_id": orgID, "before": before, "after": t})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "tags")
	httpx.OK(w, t)
	return nil
}

// deleteTag: DELETE /v1/crm/orgs/{orgID}/tags/{tagID} (settings); removed from every record.
func (s *Service) deleteTag(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "tagID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_tags WHERE id = $1 AND org_id = $2 RETURNING name`, id, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Tag not found.")
			}
			return err
		}
		for _, o := range objectTypes {
			if _, err := tx.Exec(ctx, `UPDATE `+o.table()+` SET tag_ids = array_remove(tag_ids, $2) WHERE org_id = $1 AND $2 = ANY (tag_ids)`, orgID, id); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, me(r), "crm.tag.delete", "crm.tag", id.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "tags")
	httpx.NoContent(w)
	return nil
}

// ---- saved views -------------------------------------------------------------------------

// View is a saved list view: personal (only its owner sees it) or shared
// with everyone who uses the CRM in the org.
type View struct {
	ID         uuid.UUID  `json:"id"`
	OrgID      uuid.UUID  `json:"org_id"`
	ObjectType objectType `json:"object_type"`
	Name       string     `json:"name"`
	Owner      *UserRef   `json:"owner"`
	Shared     bool       `json:"shared"`
	Columns    []string   `json:"columns"`
	SortKey    string     `json:"sort_key"`
	SortDir    string     `json:"sort_dir"`
	Filters    []Filter   `json:"filters"`
	Q          string     `json:"q"`
	CanEdit    bool       `json:"can_edit"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

const viewCols = `v.id, v.org_id, v.object_type, v.name, v.owner_id, u.display_name, u.avatar_url, v.shared, v.columns, v.sort_key, v.sort_dir,
	v.filters, v.q, v.created_at, v.updated_at`

func scanView(row pgx.Row, userID uuid.UUID, canManage bool) (*View, error) {
	var v View
	var ownerID *uuid.UUID
	var name, avatar *string
	var filters []byte
	var ot string
	if err := row.Scan(&v.ID, &v.OrgID, &ot, &v.Name, &ownerID, &name, &avatar, &v.Shared, &v.Columns, &v.SortKey, &v.SortDir,
		&filters, &v.Q, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.ObjectType = objectType(ot)
	if ownerID != nil && name != nil {
		v.Owner = &UserRef{ID: *ownerID, Name: *name, AvatarURL: avatar}
	}
	v.Filters = []Filter{}
	_ = json.Unmarshal(filters, &v.Filters)
	if v.Columns == nil {
		v.Columns = []string{}
	}
	mine := ownerID != nil && *ownerID == userID
	v.CanEdit = mine || (v.Shared && canManage)
	return &v, nil
}

// listViews: GET /v1/crm/orgs/{orgID}/views?object= — yours and shared ones.
func (s *Service) listViews(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	o, ok := parseObject(r.URL.Query().Get("object"))
	if !ok {
		return httpx.BadRequest("object is company, contact or deal.")
	}
	canManage := allowedHere(ctx, s.d.Pool, orgID, me(r), PermManageSettings)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+viewCols+` FROM crm_views v LEFT JOIN users u ON u.id = v.owner_id AND u.status <> 'deleted'
		WHERE v.org_id = $1 AND v.object_type = $2 AND (v.owner_id = $3 OR v.shared)
		ORDER BY v.shared, lower(v.name), v.id LIMIT 200`, orgID, string(o), me(r))
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []*View{}
	for rows.Next() {
		v, err := scanView(rows, me(r), canManage)
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

type viewReq struct {
	ObjectType string    `json:"object_type"`
	Name       *string   `json:"name"`
	Shared     *bool     `json:"shared"`
	Columns    *[]string `json:"columns"`
	SortKey    *string   `json:"sort_key"`
	SortDir    *string   `json:"sort_dir"`
	Filters    *[]Filter `json:"filters"`
	Q          *string   `json:"q"`
}

// validateView checks columns, sort and filters against the properties.
func (s *Service) validateView(ctx context.Context, orgID uuid.UUID, o objectType, req *viewReq) error {
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
	if err != nil {
		return err
	}
	if req.Columns != nil {
		if len(*req.Columns) > 40 {
			return httpx.BadRequest("A view can show at most 40 columns.")
		}
		for _, c := range *req.Columns {
			if _, ok := findProp(props, c); !ok {
				return httpx.BadRequest("Unknown column “" + c + "”.")
			}
		}
	}
	spec := listSpec{SortKey: deref(req.SortKey), SortDir: deref(req.SortDir)}
	if req.Filters != nil {
		spec.Filters = *req.Filters
	}
	b := newSQL(ctx, uuid.Nil)
	if _, err := buildWhere(b, o, props, orgID, spec, uuid.Nil); err != nil {
		return err
	}
	if _, _, _, err := orderBy(b, props, spec); err != nil {
		return err
	}
	return nil
}

func (s *Service) createView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req viewReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return httpx.BadRequest("object_type is company, contact or deal.")
	}
	name := strings.TrimSpace(deref(req.Name))
	if name == "" || len([]rune(name)) > 60 {
		return httpx.BadRequest("Give the view a name of up to 60 characters.")
	}
	if err := s.validateView(ctx, orgID, o, &req); err != nil {
		return err
	}
	shared := req.Shared != nil && *req.Shared
	cols, filters := []string{}, []Filter{}
	if req.Columns != nil {
		cols = *req.Columns
	}
	if req.Filters != nil {
		filters = *req.Filters
	}
	fb, _ := json.Marshal(filters)
	sortKey, sortDir := "created_at", "desc"
	if req.SortKey != nil && *req.SortKey != "" {
		sortKey = *req.SortKey
	}
	if req.SortDir != nil && *req.SortDir != "" {
		sortDir = *req.SortDir
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_views WHERE org_id = $1 AND owner_id = $2`, orgID, me(r)).Scan(&n); err != nil {
		return err
	}
	if n >= 100 {
		return httpx.BadRequest("You can save at most 100 views in " + orgs.NounA() + ".")
	}
	var id uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO crm_views (org_id, object_type, name, owner_id, shared, columns, sort_key, sort_dir, filters, q)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		orgID, string(o), name, me(r), shared, cols, sortKey, sortDir, fb, strings.TrimSpace(deref(req.Q))).Scan(&id); err != nil {
		return err
	}
	if shared {
		s.settingsChanged(ctx, orgID, "views")
	}
	return s.respondView(w, r, orgID, id, http.StatusCreated)
}

func (s *Service) respondView(w http.ResponseWriter, r *http.Request, orgID, id uuid.UUID, status int) error {
	ctx := r.Context()
	canManage := allowedHere(ctx, s.d.Pool, orgID, me(r), PermManageSettings)
	v, err := scanView(s.d.Pool.QueryRow(ctx, `SELECT `+viewCols+` FROM crm_views v LEFT JOIN users u ON u.id = v.owner_id AND u.status <> 'deleted'
		WHERE v.id = $1`, id), me(r), canManage)
	if err != nil {
		return err
	}
	httpx.JSON(w, status, v)
	return nil
}

// loadEditableView: personal views edit by their owner; shared ones by their
// owner or settings managers. Views you can't see are 404.
func (s *Service) loadEditableView(ctx context.Context, tx pgx.Tx, orgID, userID, id uuid.UUID) (*View, error) {
	canManage := allowedHere(ctx, tx, orgID, userID, PermManageSettings)
	v, err := scanView(tx.QueryRow(ctx, `SELECT `+viewCols+` FROM crm_views v LEFT JOIN users u ON u.id = v.owner_id AND u.status <> 'deleted'
		WHERE v.id = $1 AND v.org_id = $2 AND (v.owner_id = $3 OR v.shared) FOR UPDATE OF v`, id, orgID, userID), userID, canManage)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("View not found.")
	}
	if err != nil {
		return nil, err
	}
	if !v.CanEdit {
		return nil, httpx.Forbidden("Only the view's owner or someone who manages CRM settings can change a shared view.")
	}
	return v, nil
}

func (s *Service) updateView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "viewID")
	if err != nil {
		return err
	}
	var req viewReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var shared bool
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		v, err := s.loadEditableView(ctx, tx, orgID, me(r), id)
		if err != nil {
			return err
		}
		if err := s.validateView(ctx, orgID, v.ObjectType, &req); err != nil {
			return err
		}
		sets := []string{"updated_at = now()"}
		args := []any{id}
		add := func(col string, val any) {
			args = append(args, val)
			sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
		}
		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" || len([]rune(n)) > 60 {
				return httpx.BadRequest("Give the view a name of up to 60 characters.")
			}
			add("name", n)
		}
		if req.Shared != nil {
			add("shared", *req.Shared)
		}
		if req.Columns != nil {
			add("columns", *req.Columns)
		}
		if req.SortKey != nil {
			add("sort_key", *req.SortKey)
		}
		if req.SortDir != nil {
			add("sort_dir", *req.SortDir)
		}
		if req.Filters != nil {
			b, _ := json.Marshal(*req.Filters)
			add("filters", b)
		}
		if req.Q != nil {
			add("q", strings.TrimSpace(*req.Q))
		}
		shared = v.Shared || (req.Shared != nil && *req.Shared)
		_, err = tx.Exec(ctx, `UPDATE crm_views SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		return err
	})
	if err != nil {
		return err
	}
	if shared {
		s.settingsChanged(ctx, orgID, "views")
	}
	return s.respondView(w, r, orgID, id, http.StatusOK)
}

func (s *Service) deleteView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "viewID")
	if err != nil {
		return err
	}
	var shared bool
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		v, err := s.loadEditableView(ctx, tx, orgID, me(r), id)
		if err != nil {
			return err
		}
		shared = v.Shared
		_, err = tx.Exec(ctx, `DELETE FROM crm_views WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if shared {
		s.settingsChanged(ctx, orgID, "views")
	}
	httpx.NoContent(w)
	return nil
}
