package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Forms are views of type form. Each is the access resource docs.form under
// its database, so people who can open the form (shared in the workspace,
// or anyone with a public link while its creator may publish) can submit it
// — which adds a row — without edit on the database. Submissions are
// logged; public ones are rate-limited per address and per form.

const maxFormSubmissionsPerHour = 600

// registerForm makes a form view an access resource under its database.
func (s *Service) registerForm(ctx context.Context, tx pgx.Tx, me uuid.UUID, dbp *Page, viewID uuid.UUID) error {
	return access.CreateResource(ctx, tx, access.NewResource{Kind: KindForm, ID: viewID, OrgID: dbp.OrgID, OwnerID: me, Parent: &access.Ref{Kind: Kind, ID: dbp.ID}})
}

func (s *Service) formTitle(ctx context.Context, q db.Querier, viewID uuid.UUID) string {
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT config FROM docs_views WHERE id = $1`, viewID).Scan(&raw); err != nil {
		return ""
	}
	var cfg ViewConfig
	_ = json.Unmarshal(raw, &cfg)
	if cfg.Form != nil {
		return cfg.Form.Title
	}
	return ""
}

// FormQuestionDetail is a question with the property it fills.
type FormQuestionDetail struct {
	Property    Property `json:"property"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
}

// FormDetail is a form as the person filling it sees it.
type FormDetail struct {
	ViewID         uuid.UUID            `json:"view_id"`
	DatabaseID     uuid.UUID            `json:"database_id"`
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	SubmitLabel    string               `json:"submit_label"`
	SuccessMessage string               `json:"success_message"`
	Questions      []FormQuestionDetail `json:"questions"`
	Anonymous      bool                 `json:"anonymous"`
	Closed         bool                 `json:"closed"`
	Responses      *int                 `json:"responses,omitempty"` // editors of the database
	MyLevel        access.Level         `json:"my_level"`
}

// loadForm reads a form view and its database (trashed ones are gone).
func (s *Service) loadForm(ctx context.Context, viewID uuid.UUID) (*View, *Page, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE id = $1 AND type = 'form' AND block_id IS NULL`, viewID)
	if err != nil {
		return nil, nil, err
	}
	list, err := scanViews(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, httpx.NotFound("Form not found.")
	}
	dbp, err := loadPage(ctx, s.d.Pool, list[0].DatabaseID, false)
	if err != nil || dbp.DeletedAt != nil {
		return nil, nil, httpx.NotFound("Form not found.")
	}
	return &list[0], dbp, nil
}

func formDetail(v *View, dbp *Page) *FormDetail {
	f := v.Config.Form
	if f == nil {
		f = cleanForm(dbp.Schema, nil)
	}
	out := &FormDetail{ViewID: v.ID, DatabaseID: dbp.ID, Title: f.Title, Description: f.Description, SubmitLabel: f.SubmitLabel,
		SuccessMessage: f.SuccessMessage, Anonymous: f.Anonymous, Closed: f.Closed, Questions: []FormQuestionDetail{}}
	if out.Title == "" {
		out.Title = displayTitle(dbp.Title)
	}
	if out.SubmitLabel == "" {
		out.SubmitLabel = "Submit"
	}
	for _, q := range f.Questions {
		p, ok := findProp(dbp.Schema, q.Property)
		if !ok || !formAnswerable(p) {
			continue
		}
		label := q.Label
		if label == "" {
			label = p.Name
		}
		p.Description = ""
		out.Questions = append(out.Questions, FormQuestionDetail{Property: p, Label: label, Description: q.Description, Required: q.Required})
	}
	return out
}

// getForm: GET /docs/forms/{viewID} (view on the form).
func (s *Service) getForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	viewID, err := httpx.URLParamUUID(r, "viewID")
	if err != nil {
		return err
	}
	lvl, err := access.Require(ctx, s.d.Pool, me, KindForm, viewID, access.View)
	if err != nil {
		return err
	}
	v, dbp, err := s.loadForm(ctx, viewID)
	if err != nil {
		return err
	}
	out := formDetail(v, dbp)
	out.MyLevel = lvl
	if dl, err := access.LevelOf(ctx, s.d.Pool, me, Kind, dbp.ID); err == nil && dl >= access.Edit {
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_form_submissions WHERE view_id = $1`, viewID).Scan(&n); err == nil {
			out.Responses = &n
		}
	}
	httpx.OK(w, out)
	return nil
}

type formSubmitReq struct {
	Values map[string]json.RawMessage `json:"values"`
}

// submitForm: POST /docs/forms/{viewID}/submit (view on the form): adds a
// row with the answers. Agents can't submit forms for people.
func (s *Service) submitForm(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	if access.ViaAgent(ctx) {
		return httpx.Forbidden("AI agents can't submit forms.")
	}
	viewID, err := httpx.URLParamUUID(r, "viewID")
	if err != nil {
		return err
	}
	var req formSubmitReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, KindForm, viewID, access.View); err != nil {
		return err
	}
	v, dbp, err := s.loadForm(ctx, viewID)
	if err != nil {
		return err
	}
	by := &me
	if v.Config.Form != nil && v.Config.Form.Anonymous {
		by = nil
	}
	id, err := s.submit(ctx, v, dbp, by, req.Values)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"row_id": id, "message": formDetail(v, dbp).SuccessMessage})
	return nil
}

// publicForm: GET /docs/public/forms/{token} (anyone with the link).
func (s *Service) publicForm(w http.ResponseWriter, r *http.Request) error {
	v, dbp, err := s.publicFormTarget(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	out := formDetail(v, dbp)
	out.MyLevel = access.View
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}

// publicFormSubmit: POST /docs/public/forms/{token}: an anonymous response.
func (s *Service) publicFormSubmit(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	v, dbp, err := s.publicFormTarget(ctx, chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	var req formSubmitReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := s.submit(ctx, v, dbp, nil, req.Values); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"message": formDetail(v, dbp).SuccessMessage})
	return nil
}

// publicFormTarget resolves a public form link (access applies its live
// checks: revoked, expired, the creator still allowed to publish).
func (s *Service) publicFormTarget(ctx context.Context, token string) (*View, *Page, error) {
	l, err := access.ResolveLink(ctx, s.d.Pool, token)
	if err != nil {
		return nil, nil, err
	}
	if l.Ref.Kind != KindForm {
		return nil, nil, access.ErrLinkNotFound
	}
	v, dbp, err := s.loadForm(ctx, l.Ref.ID)
	if err != nil {
		return nil, nil, access.ErrLinkNotFound
	}
	if blocked, err := s.publicBlocked(ctx, dbp.ID); err != nil || blocked {
		return nil, nil, access.ErrLinkNotFound
	}
	return v, dbp, nil
}

// submit validates answers against the form's questions and adds the row:
// owned by nobody (it inherits the database's sharing), created by the
// submitter unless anonymous.
func (s *Service) submit(ctx context.Context, v *View, dbp *Page, by *uuid.UUID, answers map[string]json.RawMessage) (uuid.UUID, error) {
	d := formDetail(v, dbp)
	if d.Closed {
		return uuid.Nil, httpx.NewError(http.StatusConflict, "form_closed", "This form isn't taking responses.")
	}
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_form_submissions WHERE view_id = $1 AND created_at > now() - interval '1 hour'`, v.ID).Scan(&recent); err != nil {
		return uuid.Nil, err
	}
	if recent >= maxFormSubmissionsPerHour {
		return uuid.Nil, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "This form is getting a lot of responses. Try again in a few minutes.")
	}
	title := ""
	values := map[string]any{}
	for _, q := range d.Questions {
		raw, given := answers[q.Property.ID]
		empty := !given || len(raw) == 0 || string(raw) == "null" || string(raw) == `""` || string(raw) == "[]"
		if empty {
			if q.Required {
				return uuid.Nil, httpx.BadRequest(q.Label + ": answer this question.")
			}
			continue
		}
		if q.Property.Type == "title" {
			if err := json.Unmarshal(raw, &title); err != nil {
				return uuid.Nil, httpx.BadRequest(q.Label + ": enter text.")
			}
			continue
		}
		p := q.Property
		p.Name = q.Label
		val, err := s.cleanValue(ctx, dbp.OrgID, p, raw)
		if err != nil {
			return uuid.Nil, err
		}
		if val == nil && q.Required {
			return uuid.Nil, httpx.BadRequest(q.Label + ": answer this question.")
		}
		if val != nil {
			values[p.ID] = val
		}
	}
	t, err := cleanTitle(title)
	if err != nil {
		return uuid.Nil, err
	}
	var rowID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_pages WHERE parent_id = $1 AND is_row AND deleted_at IS NULL`, dbp.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxRows {
			return httpx.NewError(http.StatusConflict, "database_full", fmt.Sprintf("This form's database holds up to %d rows.", maxRows))
		}
		pos, err := siblingPosition(ctx, tx, dbp.OrgID, &dbp.ID, nil)
		if err != nil {
			return err
		}
		rowID = uuid.New()
		b, _ := json.Marshal(values)
		if _, err := tx.Exec(ctx, `INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
			VALUES ($1, $2, $3, 'page', true, $4, $5, $6, $7, $7)`, rowID, dbp.OrgID, dbp.ID, t, pos, b, by); err != nil {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: rowID, OrgID: dbp.OrgID, Parent: &access.Ref{Kind: Kind, ID: dbp.ID}}); err != nil {
			return err
		}
		if err := assignUniqueIDs(ctx, tx, dbp.ID, rowID, dbp.Schema); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO docs_form_submissions (view_id, row_id, user_id) VALUES ($1, $2, $3)`, v.ID, rowID, by)
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.publish(ctx, pageTopic(dbp.ID), EventRows, map[string]any{"database_id": dbp.ID, "row_id": rowID})
	if row, err := loadPage(ctx, s.d.Pool, rowID, false); err == nil {
		actor := uuid.Nil
		if by != nil {
			actor = *by
		}
		s.rowAdded(ctx, dbp, row, actor)
	}
	return rowID, nil
}

var _ = strings.TrimSpace
