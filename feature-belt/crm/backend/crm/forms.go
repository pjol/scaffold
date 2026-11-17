package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Lead forms: a public page (or an embed) whose submissions create a contact,
// or update the one with the same email, as the form's owner. The public
// link is an access link on the form (crm.form): access checks it on every
// open and submit, including that its creator still holds
// access.share_public and crm.manage_forms. Bots are kept out with a
// honeypot field and per-address and per-form rate limits.

// FormField is one input: a contact or company property.
type FormField struct {
	Key      string   `json:"key"`    // property key on its object; "email" is the contact's primary email
	Object   string   `json:"object"` // contact | company
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Type     string   `json:"type"`    // filled from the property (read only)
	Options  []Option `json:"options"` // select fields (read only)
}

// Form is a crm_forms row.
type Form struct {
	ID              uuid.UUID   `json:"id"`
	OrgID           uuid.UUID   `json:"org_id"`
	Name            string      `json:"name"`
	Fields          []FormField `json:"fields"`
	SubmitLabel     string      `json:"submit_label"`
	SuccessMessage  string      `json:"success_message"`
	ConsentText     string      `json:"consent_text"`
	Owner           *UserRef    `json:"owner"`
	NotifyIDs       []uuid.UUID `json:"notify_ids"`
	Notify          []UserRef   `json:"notify"` // the people notified, by name
	LifecycleStage  *string     `json:"lifecycle_stage"`
	TagIDs          []uuid.UUID `json:"tag_ids"`
	Published       bool        `json:"published"`
	URL             *string     `json:"url"`
	EmbedHTML       *string     `json:"embed_html"`
	SubmissionCount int         `json:"submission_count"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`

	ownerID *uuid.UUID
	linkID  *uuid.UUID
	token   *string
}

const formCols = `f.id, f.org_id, f.name, f.fields, f.submit_label, f.success_message, f.consent_text, f.owner_id, u.display_name, u.avatar_url,
	f.notify_ids, f.lifecycle_stage, f.tag_ids, f.link_id, f.link_token, f.submission_count, f.created_at, f.updated_at`

const formFrom = `FROM crm_forms f LEFT JOIN users u ON u.id = f.owner_id AND u.status <> 'deleted'`

func scanForm(row pgx.Row) (*Form, error) {
	f := &Form{}
	var fields []byte
	var name, avatar *string
	if err := row.Scan(&f.ID, &f.OrgID, &f.Name, &fields, &f.SubmitLabel, &f.SuccessMessage, &f.ConsentText, &f.ownerID, &name, &avatar,
		&f.NotifyIDs, &f.LifecycleStage, &f.TagIDs, &f.linkID, &f.token, &f.SubmissionCount, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(fields, &f.Fields)
	if f.Fields == nil {
		f.Fields = []FormField{}
	}
	if f.ownerID != nil && name != nil {
		f.Owner = &UserRef{ID: *f.ownerID, Name: *name, AvatarURL: avatar}
	}
	f.Published = f.token != nil
	return f, nil
}

func (s *Service) formURL(token string) string {
	return strings.TrimSuffix(s.d.Cfg.PublicWebURL, "/") + "/forms/" + token
}

// decorate adds links (for people who manage forms) and field types.
func (s *Service) decorateForm(ctx context.Context, f *Form, manager bool) error {
	f.Notify = []UserRef{}
	if len(f.NotifyIDs) > 0 {
		set := map[string]bool{}
		for _, id := range f.NotifyIDs {
			set[id.String()] = true
		}
		names, err := s.peopleNames(ctx, s.d.Pool, set)
		if err != nil {
			return err
		}
		for _, id := range f.NotifyIDs {
			if n, ok := names[id.String()]; ok {
				f.Notify = append(f.Notify, UserRef{ID: id, Name: n})
			}
		}
	}
	if manager && f.token != nil {
		u := s.formURL(*f.token)
		embed := fmt.Sprintf(`<iframe src="%s?embed=1" title="%s" style="width:100%%;max-width:560px;height:640px;border:0" loading="lazy"></iframe>`,
			u, strings.ReplaceAll(f.Name, `"`, "&quot;"))
		f.URL, f.EmbedHTML = &u, &embed
	}
	return s.fillFieldTypes(ctx, f.OrgID, f.Fields)
}

func (s *Service) fillFieldTypes(ctx context.Context, orgID uuid.UUID, fields []FormField) error {
	cache := map[objectType][]Property{}
	for i := range fields {
		f := &fields[i]
		if f.Object == "contact" && f.Key == "email" {
			f.Type, f.Options = "email", []Option{}
			continue
		}
		o := objContact
		if f.Object == "company" {
			o = objCompany
		}
		if cache[o] == nil {
			props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
			if err != nil {
				return err
			}
			cache[o] = props
		}
		if p, ok := findProp(cache[o], f.Key); ok {
			f.Type, f.Options = p.Type, p.Options
		}
		if f.Options == nil {
			f.Options = []Option{}
		}
	}
	return nil
}

// formFieldTypes are the property types a form can ask for.
var formFieldTypes = []string{"text", "long_text", "number", "date", "select", "multi_select", "url", "email", "phone", "phones", "checkbox", "domain"}

// validateForm checks fields and settings.
func (s *Service) validateForm(ctx context.Context, orgID uuid.UUID, f *Form) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len([]rune(f.Name)) > 120 {
		return httpx.BadRequest("Give the form a name of up to 120 characters.")
	}
	if len(f.Fields) == 0 || len(f.Fields) > 30 {
		return httpx.BadRequest("A form has 1 to 30 fields.")
	}
	contactProps, err := s.loadProperties(ctx, s.d.Pool, orgID, objContact)
	if err != nil {
		return err
	}
	companyProps, err := s.loadProperties(ctx, s.d.Pool, orgID, objCompany)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	hasEmail := false
	for i := range f.Fields {
		fl := &f.Fields[i]
		fl.Label = strings.TrimSpace(fl.Label)
		if len([]rune(fl.Label)) > 120 {
			return httpx.BadRequest("Field labels can be at most 120 characters.")
		}
		k := fl.Object + "." + fl.Key
		if seen[k] {
			return httpx.BadRequest("Each property can be on the form once.")
		}
		seen[k] = true
		switch {
		case fl.Object == "contact" && fl.Key == "email":
			hasEmail, fl.Required = true, true
			if fl.Label == "" {
				fl.Label = "Email"
			}
			continue
		case fl.Object == "contact" || fl.Object == "company":
		default:
			return httpx.BadRequest("A field's object is contact or company.")
		}
		props := contactProps
		if fl.Object == "company" {
			props = companyProps
		}
		p, ok := findProp(props, fl.Key)
		if !ok || !p.Editable || !slices.Contains(formFieldTypes, p.Type) || accessFields[p.Key] || p.Key == "lifecycle_stage" || p.Key == "lead_status" {
			return httpx.BadRequest("“" + fl.Key + "” can't be a form field.")
		}
		if fl.Label == "" {
			fl.Label = p.Label
		}
	}
	if !hasEmail {
		return httpx.BadRequest("Add the contact's email: it's how submissions find their contact.")
	}
	f.SubmitLabel = strings.TrimSpace(f.SubmitLabel)
	if f.SubmitLabel == "" {
		f.SubmitLabel = "Submit"
	}
	if len([]rune(f.SubmitLabel)) > 40 || len([]rune(f.SuccessMessage)) > 500 || len([]rune(f.ConsentText)) > 1000 {
		return httpx.BadRequest("Keep the button under 40 characters, the thank-you message under 500 and the consent text under 1,000.")
	}
	if f.LifecycleStage != nil && !hasOption(lifecycleOptions, *f.LifecycleStage) {
		return httpx.BadRequest("Choose a lifecycle stage.")
	}
	if len(f.NotifyIDs) > 25 {
		return httpx.BadRequest("Notify at most 25 people.")
	}
	for _, u := range f.NotifyIDs {
		if ok, err := isOrgMember(ctx, s.d.Pool, orgID, u); err != nil || !ok {
			return httpx.BadRequest("Notify members only.")
		}
	}
	if f.ownerID != nil {
		if ok, err := isOrgMember(ctx, s.d.Pool, orgID, *f.ownerID); err != nil || !ok {
			return httpx.BadRequest("The owner must be a member.")
		}
	}
	if len(f.TagIDs) > 0 {
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_tags WHERE org_id = $1 AND id = ANY ($2)`, orgID, f.TagIDs).Scan(&n); err != nil {
			return err
		}
		if n != len(slices.Compact(slices.Clone(f.TagIDs))) {
			return httpx.BadRequest("Choose tags from this workspace.")
		}
	}
	return nil
}

// ---- management routes ---------------------------------------------------------------------

func (s *Service) loadForm(ctx context.Context, q db.Querier, orgID, id uuid.UUID) (*Form, error) {
	f, err := scanForm(q.QueryRow(ctx, `SELECT `+formCols+` `+formFrom+` WHERE f.id = $1 AND f.org_id = $2`, id, orgID))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Form not found.")
	}
	return f, err
}

// listForms: GET /v1/crm/orgs/{orgID}/forms (crm.access; links for crm.manage_forms).
func (s *Service) listForms(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+formCols+` `+formFrom+` WHERE f.org_id = $1 ORDER BY lower(f.name), f.id LIMIT 500`, orgID)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Form, error) { return scanForm(row) })
	if err != nil {
		return err
	}
	manager := allowedHere(ctx, s.d.Pool, orgID, me(r), PermForms)
	for _, f := range list {
		if err := s.decorateForm(ctx, f, manager); err != nil {
			return err
		}
	}
	if list == nil {
		list = []*Form{}
	}
	httpx.OK(w, map[string]any{"items": list, "can_manage": manager})
	return nil
}

func (s *Service) getForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "formID")
	if err != nil {
		return err
	}
	f, err := s.loadForm(ctx, s.d.Pool, orgParam(r), id)
	if err != nil {
		return err
	}
	if err := s.decorateForm(ctx, f, allowedHere(ctx, s.d.Pool, orgParam(r), me(r), PermForms)); err != nil {
		return err
	}
	httpx.OK(w, f)
	return nil
}

type formReq struct {
	Name           *string      `json:"name"`
	Fields         *[]FormField `json:"fields"`
	SubmitLabel    *string      `json:"submit_label"`
	SuccessMessage *string      `json:"success_message"`
	ConsentText    *string      `json:"consent_text"`
	OwnerID        *uuid.UUID   `json:"owner_id"`
	NotifyIDs      *[]uuid.UUID `json:"notify_ids"`
	LifecycleStage *string      `json:"lifecycle_stage"`
	TagIDs         *[]uuid.UUID `json:"tag_ids"`
}

func (req formReq) apply(f *Form) {
	if req.Name != nil {
		f.Name = *req.Name
	}
	if req.Fields != nil {
		f.Fields = *req.Fields
	}
	if req.SubmitLabel != nil {
		f.SubmitLabel = *req.SubmitLabel
	}
	if req.SuccessMessage != nil {
		f.SuccessMessage = strings.TrimSpace(*req.SuccessMessage)
	}
	if req.ConsentText != nil {
		f.ConsentText = strings.TrimSpace(*req.ConsentText)
	}
	if req.OwnerID != nil {
		f.ownerID = req.OwnerID
	}
	if req.NotifyIDs != nil {
		f.NotifyIDs = *req.NotifyIDs
	}
	if req.LifecycleStage != nil {
		f.LifecycleStage = req.LifecycleStage
		if *req.LifecycleStage == "" {
			f.LifecycleStage = nil
		}
	}
	if req.TagIDs != nil {
		f.TagIDs = *req.TagIDs
	}
}

func fieldsJSON(fields []FormField) []byte {
	type stored struct {
		Key      string `json:"key"`
		Object   string `json:"object"`
		Label    string `json:"label"`
		Required bool   `json:"required"`
	}
	out := make([]stored, len(fields))
	for i, f := range fields {
		out[i] = stored{f.Key, f.Object, f.Label, f.Required}
	}
	b, _ := json.Marshal(out)
	return b
}

// createForm: POST /v1/crm/orgs/{orgID}/forms (crm.manage_forms, audited).
func (s *Service) createForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req formReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	f := &Form{NotifyIDs: []uuid.UUID{}, TagIDs: []uuid.UUID{}, ownerID: ptr(me(r))}
	if req.Fields == nil {
		req.Fields = &[]FormField{{Key: "first_name", Object: "contact", Label: "First name"}, {Key: "last_name", Object: "contact", Label: "Last name"},
			{Key: "email", Object: "contact", Label: "Email", Required: true}}
	}
	req.apply(f)
	if err := s.validateForm(ctx, orgID, f); err != nil {
		return err
	}
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_forms WHERE org_id = $1`, orgID).Scan(&n); err != nil {
			return err
		}
		if n >= 200 {
			return httpx.BadRequest(orgNounA() + " can have at most 200 forms.")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm_forms (org_id, name, fields, submit_label, success_message, consent_text, owner_id, notify_ids,
			lifecycle_stage, tag_ids, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			orgID, f.Name, fieldsJSON(f.Fields), f.SubmitLabel, f.SuccessMessage, f.ConsentText, f.ownerID, f.NotifyIDs, f.LifecycleStage, f.TagIDs, me(r)).Scan(&id); err != nil {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindForm, ID: id, OrgID: orgID, OwnerID: me(r)}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.form.create", KindForm, id.String(), map[string]any{"org_id": orgID, "name": f.Name})
	})
	if err != nil {
		return err
	}
	out, err := s.loadForm(ctx, s.d.Pool, orgID, id)
	if err != nil {
		return err
	}
	if err := s.decorateForm(ctx, out, true); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// updateForm: PATCH /v1/crm/orgs/{orgID}/forms/{formID} (crm.manage_forms, audited).
func (s *Service) updateForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "formID")
	if err != nil {
		return err
	}
	var req formReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		f, err := scanForm(tx.QueryRow(ctx, `SELECT `+formCols+` `+formFrom+` WHERE f.id = $1 AND f.org_id = $2 FOR UPDATE OF f`, id, orgID))
		if db.IsNoRows(err) {
			return httpx.NotFound("Form not found.")
		}
		if err != nil {
			return err
		}
		req.apply(f)
		if err := s.validateForm(ctx, orgID, f); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_forms SET name = $2, fields = $3, submit_label = $4, success_message = $5, consent_text = $6, owner_id = $7,
			notify_ids = $8, lifecycle_stage = $9, tag_ids = $10, updated_at = now() WHERE id = $1`,
			id, f.Name, fieldsJSON(f.Fields), f.SubmitLabel, f.SuccessMessage, f.ConsentText, f.ownerID, f.NotifyIDs, f.LifecycleStage, f.TagIDs); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.form.update", KindForm, id.String(), map[string]any{"org_id": orgID, "name": f.Name})
	})
	if err != nil {
		return err
	}
	out, err := s.loadForm(ctx, s.d.Pool, orgID, id)
	if err != nil {
		return err
	}
	if err := s.decorateForm(ctx, out, true); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// publishForm / unpublishForm: POST /v1/crm/orgs/{orgID}/forms/{formID}/publish|unpublish
// (crm.manage_forms; publishing also needs access.share_public; audited).
func (s *Service) publishForm(publish bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		orgID := orgParam(r)
		id, err := idParam(r, "formID")
		if err != nil {
			return err
		}
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			f, err := scanForm(tx.QueryRow(ctx, `SELECT `+formCols+` `+formFrom+` WHERE f.id = $1 AND f.org_id = $2 FOR UPDATE OF f`, id, orgID))
			if db.IsNoRows(err) {
				return httpx.NotFound("Form not found.")
			}
			if err != nil {
				return err
			}
			if !publish {
				if _, err := tx.Exec(ctx, `UPDATE crm_forms SET link_id = NULL, link_token = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
					return err
				}
				return audit.Log(ctx, tx, me(r), "crm.form.unpublish", KindForm, id.String(), map[string]any{"org_id": orgID})
			}
			if f.token != nil {
				return httpx.Conflict("This form is already published.")
			}
			link, err := access.CreateLink(ctx, tx, access.Ref{Kind: KindForm, ID: id}, access.View, nil, me(r))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm_forms SET link_id = $2, link_token = $3, updated_at = now() WHERE id = $1`, id, link.ID, *link.Token); err != nil {
				return err
			}
			return audit.Log(ctx, tx, me(r), "crm.form.publish", KindForm, id.String(), map[string]any{"org_id": orgID})
		})
		if err != nil {
			return err
		}
		out, err := s.loadForm(ctx, s.d.Pool, orgID, id)
		if err != nil {
			return err
		}
		if err := s.decorateForm(ctx, out, true); err != nil {
			return err
		}
		httpx.OK(w, out)
		return nil
	}
}

// deleteForm: DELETE /v1/crm/orgs/{orgID}/forms/{formID} (crm.manage_forms, audited).
// Contacts it created stay.
func (s *Service) deleteForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "formID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_forms WHERE id = $1 AND org_id = $2 RETURNING name`, id, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Form not found.")
			}
			return err
		}
		if err := access.DeleteResource(ctx, tx, access.Ref{Kind: KindForm, ID: id}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.form.delete", KindForm, id.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// FormSubmission is one submission, with its contact when the caller can open it.
type FormSubmission struct {
	ID        uuid.UUID         `json:"id"`
	Contact   *RecordRef        `json:"contact"`
	Created   bool              `json:"created"`
	Values    map[string]string `json:"values"`
	Consent   bool              `json:"consent"`
	PageURL   string            `json:"page_url"`
	CreatedAt time.Time         `json:"created_at"`
}

// listSubmissions: GET /v1/crm/orgs/{orgID}/forms/{formID}/submissions?cursor= (crm.manage_forms).
// Values show only for contacts the caller can open.
func (s *Service) listSubmissions(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "formID")
	if err != nil {
		return err
	}
	if _, err := s.loadForm(ctx, s.d.Pool, orgID, id); err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	args := []any{id, page.Limit + 1}
	where := `form_id = $1`
	if page.Cursor != "" {
		var c struct {
			At time.Time `json:"at"`
			ID uuid.UUID `json:"id"`
		}
		if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
			return err
		}
		args = append(args, c.At, c.ID)
		where += ` AND (created_at, id) < ($3, $4)`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, contact_id, created, vals, consent, page_url, created_at FROM crm_form_submissions
		WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $2`, args...)
	if err != nil {
		return err
	}
	type row struct {
		FormSubmission
		contactID *uuid.UUID
	}
	list, err := pgx.CollectRows(rows, func(rw pgx.CollectableRow) (*row, error) {
		x := &row{}
		var raw []byte
		err := rw.Scan(&x.ID, &x.contactID, &x.Created, &raw, &x.Consent, &x.PageURL, &x.CreatedAt)
		_ = json.Unmarshal(raw, &x.Values)
		return x, err
	})
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for _, x := range list {
		if x.contactID != nil {
			ids = append(ids, *x.contactID)
		}
	}
	refs, err := s.visibleRefs(ctx, s.d.Pool, me(r), objContact, ids)
	if err != nil {
		return err
	}
	out := []FormSubmission{}
	for _, x := range list {
		if x.contactID != nil {
			if ref, ok := refs[*x.contactID]; ok {
				x.Contact = &ref
			}
		}
		if x.Contact == nil {
			x.Values = map[string]string{} // the caller can't open the contact: no personal data
		}
		out = append(out, x.FormSubmission)
	}
	httpx.OK(w, httpx.NewPage(out, page.Limit, func(fs FormSubmission) any { return map[string]any{"at": fs.CreatedAt, "id": fs.ID} }))
	return nil
}

// ---- public -------------------------------------------------------------------------------

// PublicForm is what anyone with the link sees.
type PublicForm struct {
	Name           string      `json:"name"`
	SellerName     string      `json:"seller_name"`
	Fields         []FormField `json:"fields"`
	SubmitLabel    string      `json:"submit_label"`
	SuccessMessage string      `json:"success_message"`
	ConsentText    string      `json:"consent_text"`
}

var errFormLink = httpx.NewError(http.StatusNotFound, "link_not_found", "This form isn't available anymore.")

func (s *Service) publicFormTarget(ctx context.Context, token string) (*Form, error) {
	l, err := access.ResolveLink(ctx, s.d.Pool, token)
	if err != nil || l.Ref.Kind != KindForm {
		return nil, errFormLink
	}
	f, err := scanForm(s.d.Pool.QueryRow(ctx, `SELECT `+formCols+` `+formFrom+` WHERE f.id = $1`, l.Ref.ID))
	if db.IsNoRows(err) {
		return nil, errFormLink
	}
	if err != nil {
		return nil, err
	}
	if f.linkID == nil || *f.linkID != l.ID || f.ownerID == nil {
		return nil, errFormLink
	}
	return f, nil
}

// publicForm: GET /v1/crm/public/forms/{token}.
func (s *Service) publicForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	f, err := s.publicFormTarget(ctx, chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	if err := s.fillFieldTypes(ctx, f.OrgID, f.Fields); err != nil {
		return err
	}
	pf := &PublicForm{Name: f.Name, Fields: f.Fields, SubmitLabel: f.SubmitLabel, SuccessMessage: f.SuccessMessage, ConsentText: f.ConsentText}
	_ = s.d.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, f.OrgID).Scan(&pf.SellerName)
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, pf)
	return nil
}

type submitReq struct {
	Values   map[string]json.RawMessage `json:"values"` // "contact.first_name" → value
	Consent  bool                       `json:"consent"`
	Honeypot string                     `json:"website"` // hidden field people never fill
	PageURL  string                     `json:"page_url"`
}

// submitForm: POST /v1/crm/public/forms/{token}.
func (s *Service) submitForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	f, err := s.publicFormTarget(ctx, chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	var req submitReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	done := map[string]any{"ok": true, "message": f.SuccessMessage}
	if strings.TrimSpace(req.Honeypot) != "" { // a bot: answer as if it worked, keep nothing
		httpx.OK(w, done)
		return nil
	}
	if f.ConsentText != "" && !req.Consent {
		return httpx.BadRequest("Tick the box to agree before sending.")
	}
	if err := s.fillFieldTypes(ctx, f.OrgID, f.Fields); err != nil {
		return err
	}
	res, err := s.applySubmission(ctx, f, req)
	if err != nil {
		return err
	}
	_ = res
	httpx.OK(w, done)
	return nil
}

// submissionResult says what a submission did.
type submissionResult struct {
	contactID uuid.UUID
	created   bool
}

// applySubmission creates or updates the contact (by email) as the form's
// owner and records the submission.
func (s *Service) applySubmission(ctx context.Context, f *Form, req submitReq) (*submissionResult, error) {
	owner := *f.ownerID
	if !allowedHere(ctx, s.d.Pool, f.OrgID, owner, PermAccess) {
		return nil, errFormLink
	}
	contactIn := map[string]json.RawMessage{}
	companyIn := map[string]json.RawMessage{}
	vals := map[string]string{}
	var email string
	for _, fl := range f.Fields {
		raw, ok := req.Values[fl.Object+"."+fl.Key]
		empty := !ok || len(raw) == 0 || string(raw) == "null" || string(raw) == `""` || string(raw) == "[]"
		if empty {
			if fl.Required {
				return nil, httpx.BadRequest(fl.Label + " is required.")
			}
			continue
		}
		if fl.Object == "contact" && fl.Key == "email" {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, httpx.BadRequest("Enter a valid email address.")
			}
			e, err := cleanEmail(v)
			if err != nil {
				return nil, httpx.BadRequest("Enter a valid email address.")
			}
			email = e
			vals["contact.email"] = e
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = strings.Trim(string(raw), `"`)
		} else if fl.Type == "phones" { // one phone on the form; the contact keeps a list
			raw, _ = json.Marshal([]string{text})
		}
		vals[fl.Object+"."+fl.Key] = clip(text, 500)
		if fl.Object == "company" {
			companyIn[fl.Key] = raw
		} else {
			contactIn[fl.Key] = raw
		}
	}
	if email == "" {
		return nil, httpx.BadRequest("Enter your email address.")
	}
	ctx = context.WithoutCancel(ctx)
	// Validate values the same way the record forms do, before writing.
	props, err := s.loadProperties(ctx, s.d.Pool, f.OrgID, objContact)
	if err != nil {
		return nil, err
	}
	var check pending
	if err := s.applyFields(ctx, s.d.Pool, f.OrgID, owner, objContact, props, nil, contactIn, &check); err != nil {
		return nil, asHTTP(err)
	}
	res := &submissionResult{}
	var existing uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT id FROM crm_contacts WHERE org_id = $1 AND $2 = ANY (emails) ORDER BY created_at, id LIMIT 1`, f.OrgID, email).Scan(&existing)
	switch {
	case err == nil:
		res.contactID = existing
		if len(contactIn) > 0 {
			if lvl, lerr := access.LevelOf(ctx, s.d.Pool, owner, KindContact, existing); lerr == nil && lvl >= access.Edit {
				if _, err := s.updateWith(ctx, owner, objContact, existing, contactIn, true); err != nil {
					return nil, err
				}
			}
		}
	case db.IsNoRows(err):
		in := map[string]json.RawMessage{}
		for k, v := range contactIn {
			in[k] = v
		}
		emails, _ := json.Marshal([]string{email})
		in["emails"] = emails
		in["allow_duplicate"] = json.RawMessage("true")
		if f.LifecycleStage != nil {
			in["lifecycle_stage"], _ = json.Marshal(*f.LifecycleStage)
		}
		if len(f.TagIDs) > 0 {
			in["tags"], _ = json.Marshal(f.TagIDs)
		}
		rec, err := s.create(ctx, owner, f.OrgID, objContact, in, false)
		if err != nil {
			return nil, err
		}
		res.contactID, res.created = rec.base().ID, true
	default:
		return nil, err
	}
	if name, ok := companyIn["name"]; ok {
		if err := s.formCompany(ctx, f, owner, res.contactID, name, companyIn); err != nil {
			return nil, err
		}
	}
	valsJSON, _ := json.Marshal(vals)
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO crm_form_submissions (form_id, org_id, contact_id, created, vals, consent, page_url) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		f.ID, f.OrgID, res.contactID, res.created, valsJSON, req.Consent, clip(req.PageURL, 500)); err != nil {
		return nil, err
	}
	_, _ = s.d.Pool.Exec(ctx, `UPDATE crm_forms SET submission_count = submission_count + 1 WHERE id = $1`, f.ID)
	if err := addEvent(ctx, s.d.Pool, f.OrgID, objContact, res.contactID, "form_submitted", uuid.Nil, map[string]any{
		"form_id": f.ID, "form_name": f.Name, "values": vals, "created": res.created, "consent": req.Consent}); err != nil {
		return nil, err
	}
	_, _ = s.d.Pool.Exec(ctx, `UPDATE crm_contacts SET last_activity_at = now() WHERE id = $1`, res.contactID)
	s.recordChanged(ctx, objContact, res.contactID, true)
	title, _ := recordTitle(ctx, s.d.Pool, objContact, res.contactID)
	for _, u := range f.NotifyIDs {
		if lvl, err := access.LevelOf(ctx, s.d.Pool, u, KindContact, res.contactID); err != nil || lvl < access.View {
			continue
		}
		s.send(ctx, notify.Notification{UserID: u, Kind: NotifyForm, Title: clip(title, 80) + " filled in “" + clip(f.Name, 60) + "”",
			Link: objContact.link(res.contactID), Data: map[string]any{"type": objContact, "id": res.contactID, "form_id": f.ID}})
	}
	return res, nil
}

// formCompany finds the company by name (or creates it, as the owner) and
// links the contact to it when the contact has no company yet.
func (s *Service) formCompany(ctx context.Context, f *Form, owner, contactID uuid.UUID, nameRaw json.RawMessage, in map[string]json.RawMessage) error {
	var name string
	if json.Unmarshal(nameRaw, &name) != nil || strings.TrimSpace(name) == "" {
		return nil
	}
	var linked bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_contact_companies WHERE contact_id = $1)`, contactID).Scan(&linked); err != nil || linked {
		return err
	}
	var companyID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id FROM crm_companies WHERE org_id = $1 AND lower(name) = lower($2) ORDER BY created_at, id LIMIT 1`, f.OrgID, strings.TrimSpace(name)).Scan(&companyID)
	if db.IsNoRows(err) {
		cin := map[string]json.RawMessage{"allow_duplicate": json.RawMessage("true")}
		for k, v := range in {
			cin[k] = v
		}
		rec, err := s.create(ctx, owner, f.OrgID, objCompany, cin, false)
		if err != nil {
			return err
		}
		companyID = rec.base().ID
	} else if err != nil {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if lvl, err := access.LevelOf(ctx, tx, owner, KindCompany, companyID); err != nil || lvl < access.View {
			return err
		}
		return asHTTP(s.linkContactCompany(ctx, tx, owner, f.OrgID, contactID, companyID, true))
	})
}
