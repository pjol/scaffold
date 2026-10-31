package places

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// AdminPlace is a place as staff see it.
type AdminPlace struct {
	Place
	Owner      *Owner     `json:"owner"` // nil for staff-created listings and deleted owners
	ReviewedBy *uuid.UUID `json:"reviewed_by"`
	ReviewedAt *time.Time `json:"reviewed_at"`
}

// Owner is the submitting user.
type Owner struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

func scanAdminPlace(row pgx.Row) (AdminPlace, error) {
	var (
		a           AdminPlace
		ownerID     *uuid.UUID
		email, name *string
		reviewedBy  *uuid.UUID
		reviewedAt  *time.Time
	)
	p, err := scanPlace(row, &ownerID, &email, &name, &reviewedBy, &reviewedAt)
	if err != nil {
		return a, err
	}
	a.Place, a.ReviewedBy, a.ReviewedAt = p, reviewedBy, reviewedAt
	if ownerID != nil {
		a.Owner = &Owner{ID: *ownerID, Email: deref(email), DisplayName: deref(name)}
	}
	return a, nil
}

const adminFrom = ` FROM places p LEFT JOIN users u ON u.id = p.owner_id `
const adminExtraCols = `, p.owner_id, u.email, u.display_name, p.reviewed_by, p.reviewed_at`

type adminCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// adminList is GET /v1/admin/places?status=pending|approved|rejected&q=.
// The pending queue lists oldest first; other statuses newest first.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "approved", "rejected":
	default:
		return httpx.BadRequest("status must be pending, approved or rejected.")
	}
	text := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	pp := httpx.ParsePage(r)
	var c adminCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	w2 := &sqlWhere{}
	w2.and("p.status = " + w2.arg(status))
	if text != "" {
		w2.and("((p.name || ' ' || p.address) ILIKE " + w2.arg("%"+escapeLike(text)+"%") + " OR u.email ILIKE " + w2.arg("%"+escapeLike(text)+"%") + ")")
	}
	order, cmp := "DESC", "<"
	if status == "pending" {
		order, cmp = "ASC", ">"
	}
	if !c.T.IsZero() {
		w2.and("(p.created_at, p.id) " + cmp + " (" + w2.arg(c.T) + ", " + w2.arg(c.ID) + ")")
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+placeCols+adminExtraCols+adminFrom+`WHERE `+w2.sql()+
		` ORDER BY p.created_at `+order+`, p.id `+order+` LIMIT `+w2.arg(pp.Limit+1), w2.args...)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminPlace, error) { return scanAdminPlace(row) })
	if err != nil {
		return err
	}
	page := httpx.NewPage(list, pp.Limit, func(a AdminPlace) any { return adminCursor{a.CreatedAt, a.ID} })
	ptrs := make([]*Place, len(page.Items))
	for i := range page.Items {
		ptrs[i] = &page.Items[i].Place
	}
	if err := s.attachPhotos(ctx, s.d.Pool, ptrs); err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) loadAdminPlace(ctx context.Context, q db.Querier, id uuid.UUID, lock bool) (*AdminPlace, error) {
	sql := `SELECT ` + placeCols + adminExtraCols + adminFrom + `WHERE p.id = $1`
	if lock {
		sql += ` FOR UPDATE OF p`
	}
	a, err := scanAdminPlace(q.QueryRow(ctx, sql, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Place not found.")
	}
	if err != nil {
		return nil, err
	}
	if err := s.attachPhotos(ctx, q, []*Place{&a.Place}); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) adminGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.loadAdminPlace(r.Context(), s.d.Pool, id, false)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

// adminCreate adds an approved listing with no owner (staff-made directory
// entries are not anyone's personal content).
func (s *Service) adminCreate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	var in placeInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	f, err := s.merge(ctx, nil, in)
	if err != nil {
		return err
	}
	if err := checkPhotoKeys(in.PhotoKeys, staff.UserID, 0); err != nil {
		return err
	}
	var out *AdminPlace
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		id, err := insertPlace(ctx, tx, nil, f, "approved", &staff.UserID, in.PhotoKeys, staff.UserID)
		if err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, staff.UserID, "places.create", "place", id.String(), map[string]any{"name": f.Name}); err != nil {
			return err
		}
		out, err = s.loadAdminPlace(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) adminUpdate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in placeInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if len(in.PhotoKeys) > 0 {
		return httpx.BadRequest("Add photos with POST /v1/admin/places/{id}/photos.")
	}
	var out *AdminPlace
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		a, err := s.loadAdminPlace(ctx, tx, id, true)
		if err != nil {
			return err
		}
		before := fieldsOf(&a.Place)
		f, err := s.merge(ctx, &before, in)
		if err != nil {
			return err
		}
		if err := updateFields(ctx, tx, id, f); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, staff.UserID, "places.update", "place", id.String(),
			map[string]any{"changed": changedFields(before, f)}); err != nil {
			return err
		}
		out, err = s.loadAdminPlace(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// changedFields names the columns an edit changed (for the audit row).
func changedFields(a, b fields) []string {
	out := []string{}
	add := func(name string, changed bool) {
		if changed {
			out = append(out, name)
		}
	}
	ha, _ := hoursJSON(a.Hours)
	hb, _ := hoursJSON(b.Hours)
	add("name", a.Name != b.Name)
	add("category", a.Category != b.Category)
	add("description", a.Description != b.Description)
	add("address", a.Address != b.Address)
	add("location", a.Lat != b.Lat || a.Lng != b.Lng)
	add("phone", a.Phone != b.Phone)
	add("website", a.Website != b.Website)
	add("hours", string(ha) != string(hb))
	return out
}

type rejectReq struct {
	Reason string `json:"reason"`
}

func (s *Service) adminApprove(w http.ResponseWriter, r *http.Request) error {
	return s.review(w, r, "approved", "")
}

func (s *Service) adminReject(w http.ResponseWriter, r *http.Request) error {
	var req rejectReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return httpx.BadRequest("Give a reason of 3 to 500 characters. The submitter sees it.")
	}
	return s.review(w, r, "rejected", reason)
}

// review applies a staff decision with an audit row, then tells the owner.
func (s *Service) review(w http.ResponseWriter, r *http.Request, status, reason string) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var out *AdminPlace
	var from string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		a, err := s.loadAdminPlace(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.Status == status {
			return httpx.Conflict("This place is already " + status + ".")
		}
		from = a.Status
		if _, err := tx.Exec(ctx, `UPDATE places SET status = $2, review_note = $3, reviewed_by = $4,
			reviewed_at = now(), updated_at = now() WHERE id = $1`, id, status, reason, staff.UserID); err != nil {
			return err
		}
		data := map[string]any{"from": from, "name": a.Name}
		if reason != "" {
			data["reason"] = reason
		}
		action := map[string]string{"approved": "places.approve", "rejected": "places.reject"}[status]
		if err := audit.Log(ctx, tx, staff.UserID, action, "place", id.String(), data); err != nil {
			return err
		}
		out, err = s.loadAdminPlace(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return err
	}
	if out.Owner != nil {
		n := notify.Notification{UserID: out.Owner.ID, Kind: "places.review", Link: "/places/" + id.String(),
			Data: map[string]any{"place_id": id, "status": status}}
		if status == "approved" {
			n.Title, n.Body = "Place approved", out.Name+" is on the map."
		} else {
			n.Title, n.Body = "Place not approved", out.Name+": "+reason
		}
		if err := s.d.Notify.Send(context.WithoutCancel(ctx), n); err != nil {
			slog.WarnContext(ctx, "places: notify review", "place_id", id, "err", err)
		}
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) adminDelete(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var keys []string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		a, err := s.loadAdminPlace(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if keys, err = deletePlaceRows(ctx, tx, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "places.delete", "place", id.String(),
			map[string]any{"name": a.Name, "status": a.Status})
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	httpx.NoContent(w)
	return nil
}

// adminAddPhoto attaches a photo staff uploaded (purpose place_photo).
func (s *Service) adminAddPhoto(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req photoReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out *AdminPlace
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		a, err := s.loadAdminPlace(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err := insertPhoto(ctx, tx, &a.Place, req.Key, staff.UserID); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, staff.UserID, "places.photo.add", "place", id.String(), map[string]any{"key": req.Key}); err != nil {
			return err
		}
		out, err = s.loadAdminPlace(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) adminRemovePhoto(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	photoID, err := httpx.URLParamUUID(r, "photo_id")
	if err != nil {
		return err
	}
	var key string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := s.loadAdminPlace(ctx, tx, id, true); err != nil {
			return err
		}
		var err error
		if key, err = deletePhotoRow(ctx, tx, id, photoID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "places.photo.remove", "place", id.String(),
			map[string]any{"photo_id": photoID})
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, []string{key})
	httpx.NoContent(w)
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
