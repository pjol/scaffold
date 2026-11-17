package docs

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

type verifyReq struct {
	Days int `json:"days"` // 0: until someone changes it; else 1–365
}

// verifyPage: PUT /docs/pages/{id}/verification (edit) {days}: mark the page
// as verified by the caller, for days or until changed. Agents can't vouch.
func (s *Service) verifyPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req verifyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Days < 0 || req.Days > 365 {
		return httpx.BadRequest("days must be 0 (until changed) to 365.")
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, false); err != nil {
		return err
	}
	var until *time.Time
	if req.Days > 0 {
		t := time.Now().AddDate(0, 0, req.Days)
		until = &t
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET verified_by = $2, verified_at = now(), verified_until = $3 WHERE id = $1`, id, me, until); err != nil {
		return err
	}
	return s.pageAfterChrome(w, r, id, p.MyLevel)
}

// unverifyPage: DELETE /docs/pages/{id}/verification (edit).
func (s *Service) unverifyPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET verified_by = NULL, verified_at = NULL, verified_until = NULL WHERE id = $1`, id); err != nil {
		return err
	}
	return s.pageAfterChrome(w, r, id, p.MyLevel)
}

func (s *Service) pageAfterChrome(w http.ResponseWriter, r *http.Request, id uuid.UUID, lvl access.Level) error {
	ctx := r.Context()
	out, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out.MyLevel = lvl
	s.pageChanged(ctx, out, false)
	httpx.OK(w, out)
	return nil
}
