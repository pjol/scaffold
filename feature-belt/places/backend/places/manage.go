package places

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// placeInput is the create/update body. Absent fields keep their value on
// update; "hours": null clears the hours.
type placeInput struct {
	Name        *string         `json:"name"`
	Category    *string         `json:"category"`
	Description *string         `json:"description"`
	Address     *string         `json:"address"`
	Lat         *float64        `json:"lat"`
	Lng         *float64        `json:"lng"`
	Phone       *string         `json:"phone"`
	Website     *string         `json:"website"`
	Hours       json.RawMessage `json:"hours"`
	PhotoKeys   []string        `json:"photo_keys"` // create only: keys from POST /v1/uploads (purpose place_photo)
}

// fields are a place's editable columns after validation.
type fields struct {
	Name, Category, Description, Address, Phone, Website string
	Lat, Lng                                             float64
	Hours                                                *Hours
}

func fieldsOf(p *Place) fields {
	return fields{Name: p.Name, Category: p.Category, Description: p.Description, Address: p.Address,
		Phone: p.Phone, Website: p.Website, Lat: p.Lat, Lng: p.Lng, Hours: p.Hours}
}

// merge applies in over base (nil base = create, where name, category, lat
// and lng are required) and validates the result.
func (s *Service) merge(ctx context.Context, base *fields, in placeInput) (fields, error) {
	var f fields
	if base != nil {
		f = *base
	} else if in.Name == nil || in.Category == nil || in.Lat == nil || in.Lng == nil {
		return f, httpx.BadRequest("Name, category and location are required.")
	}
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&f.Name, in.Name)
	f.Name = strings.Join(strings.Fields(f.Name), " ")
	set(&f.Category, in.Category)
	set(&f.Description, in.Description)
	set(&f.Address, in.Address)
	f.Address = strings.Join(strings.Fields(f.Address), " ")
	set(&f.Phone, in.Phone)
	set(&f.Website, in.Website)
	if in.Lat != nil {
		f.Lat = *in.Lat
	}
	if in.Lng != nil {
		f.Lng = *in.Lng
	}
	if in.Hours != nil {
		h, err := parseHours(in.Hours)
		if err != nil {
			return f, err
		}
		f.Hours = h
	}

	switch n := utf8.RuneCountInString(f.Name); {
	case n == 0:
		return f, httpx.BadRequest("Enter the place's name.")
	case n > 120:
		return f, httpx.BadRequest("Name must be 120 characters or fewer.")
	}
	if !s.cfg.categorySet[f.Category] {
		return f, httpx.BadRequest("Choose one of the listed categories.")
	}
	if utf8.RuneCountInString(f.Description) > 2000 {
		return f, httpx.BadRequest("Description must be 2,000 characters or fewer.")
	}
	if utf8.RuneCountInString(f.Address) > 300 {
		return f, httpx.BadRequest("Address must be 300 characters or fewer.")
	}
	if !validLat(f.Lat) || !validLng(f.Lng) {
		return f, httpx.BadRequest("Latitude must be -90 to 90 and longitude -180 to 180.")
	}
	if a := s.cfg.serviceArea; a != nil && !a.Contains(f.Lat, f.Lng) {
		return f, httpx.NewError(http.StatusUnprocessableEntity, "outside_service_area", "That location is outside the area this app covers.")
	}
	if !validPhone(f.Phone) {
		return f, httpx.BadRequest("Phone may use digits, spaces and + ( ) - . only, up to 40 characters.")
	}
	w, err := normalizeWebsite(f.Website)
	if err != nil {
		return f, err
	}
	f.Website = w
	if contentFilter != nil {
		if err := contentFilter(ctx, f.Name+"\n"+f.Description); err != nil {
			return f, err
		}
	}
	return f, nil
}

func validPhone(p string) bool {
	if len(p) > 40 {
		return false
	}
	for _, c := range p {
		if !(c >= '0' && c <= '9' || strings.ContainsRune(" +()-.x", c)) {
			return false
		}
	}
	return true
}

// normalizeWebsite accepts http(s) URLs (a bare domain gets https://). The
// server stores the link; it never fetches it.
func normalizeWebsite(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		len(raw) > 2048 || strings.ContainsAny(u.Host, " <>\"") {
		return "", httpx.BadRequest("Website must be a web address like https://example.com.")
	}
	return u.String(), nil
}

func hoursJSON(h *Hours) ([]byte, error) {
	if h == nil {
		return nil, nil
	}
	return json.Marshal(h)
}

// insertPlace writes a new place and its photos (keys already validated).
func insertPlace(ctx context.Context, tx pgx.Tx, owner *uuid.UUID, f fields, status string, reviewer *uuid.UUID, keys []string, uploader uuid.UUID) (uuid.UUID, error) {
	hours, err := hoursJSON(f.Hours)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO places (owner_id, name, category, description, address, lat, lng, phone, website, hours, status,
		                    reviewed_by, reviewed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, CASE WHEN $12::uuid IS NULL THEN NULL ELSE now() END)
		RETURNING id`,
		owner, f.Name, f.Category, f.Description, f.Address, f.Lat, f.Lng, f.Phone, f.Website, hours, status, reviewer,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	for i, k := range keys {
		if _, err := tx.Exec(ctx, `INSERT INTO place_photos (place_id, key, uploaded_by, position) VALUES ($1, $2, $3, $4)`,
			id, k, uploader, i); err != nil {
			if db.IsUniqueViolation(err) {
				return uuid.Nil, httpx.Conflict("That photo is already attached to a place.")
			}
			return uuid.Nil, err
		}
	}
	return id, nil
}

func updateFields(ctx context.Context, tx pgx.Tx, id uuid.UUID, f fields) error {
	hours, err := hoursJSON(f.Hours)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE places SET name = $2, category = $3, description = $4, address = $5, lat = $6, lng = $7,
		  phone = $8, website = $9, hours = $10, updated_at = now()
		WHERE id = $1`, id, f.Name, f.Category, f.Description, f.Address, f.Lat, f.Lng, f.Phone, f.Website, hours)
	return err
}

// checkPhotoKeys validates keys the client uploaded as uploader.
func checkPhotoKeys(keys []string, uploader uuid.UUID, existing int) error {
	if existing+len(keys) > maxPhotosPerPlace {
		return httpx.BadRequest(fmt.Sprintf("A place can have up to %d photos.", maxPhotosPerPlace))
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if !storage.KeyOwnedBy(k, photoPurpose, uploader) || seen[k] {
			return httpx.BadRequest("Upload each photo first (purpose place_photo), then send its key once.")
		}
		seen[k] = true
	}
	return nil
}

// ---- owner routes ----------------------------------------------------------------

func (s *Service) createPlace(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var in placeInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	f, err := s.merge(ctx, nil, in)
	if err != nil {
		return err
	}
	if err := checkPhotoKeys(in.PhotoKeys, me.UserID, 0); err != nil {
		return err
	}
	status := "approved"
	if s.cfg.requireApproval {
		status = "pending"
	}
	var p *Place
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		id, err := insertPlace(ctx, tx, &me.UserID, f, status, nil, in.PhotoKeys, me.UserID)
		if err != nil {
			return err
		}
		p, err = s.loadPlace(ctx, tx, `p.id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if p.Status == "pending" {
		s.announcePending(ctx, p.ID)
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// lockOwned locks a place the caller owns.
func (s *Service) lockOwned(ctx context.Context, tx pgx.Tx, id, owner uuid.UUID) (*Place, error) {
	return s.loadPlace(ctx, tx, `p.id = $1 AND p.owner_id = $2 FOR UPDATE`, id, owner)
}

// requeue is the status after an owner change: with approval on, an edit to
// an approved place goes back to review; a rejected place edited is
// resubmitted (staff decisions stand until staff look again).
func (s *Service) requeue(status string) string {
	if status == "rejected" || (status == "approved" && s.cfg.requireApproval) {
		return "pending"
	}
	return status
}

func (s *Service) updatePlace(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in placeInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if len(in.PhotoKeys) > 0 {
		return httpx.BadRequest("Add photos with POST /v1/places/{id}/photos.")
	}
	var out *Place
	requeued := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		p, err := s.lockOwned(ctx, tx, id, me.UserID)
		if err != nil {
			return err
		}
		base := fieldsOf(p)
		f, err := s.merge(ctx, &base, in)
		if err != nil {
			return err
		}
		if err := updateFields(ctx, tx, id, f); err != nil {
			return err
		}
		if next := s.requeue(p.Status); next != p.Status {
			requeued = true
			if _, err := tx.Exec(ctx, `UPDATE places SET status = $2, review_note = '', reviewed_by = NULL,
				reviewed_at = NULL WHERE id = $1`, id, next); err != nil {
				return err
			}
		}
		out, err = s.loadPlace(ctx, tx, `p.id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if requeued {
		s.announcePending(ctx, id)
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) deletePlace(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var keys []string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := s.lockOwned(ctx, tx, id, me.UserID); err != nil {
			return err
		}
		var err error
		if keys, err = deletePlaceRows(ctx, tx, id); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	httpx.NoContent(w)
	return nil
}

// deletePlaceRows deletes a place (photos and favourites cascade) and returns
// its photo keys for removal from storage after commit.
func deletePlaceRows(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT key FROM place_photos WHERE place_id = $1`, id)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM places WHERE id = $1`, id); err != nil {
		return nil, err
	}
	return keys, nil
}

// deleteObjects removes stored photos, best-effort, after the rows are gone.
func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	ctx = context.WithoutCancel(ctx)
	for _, k := range keys {
		if err := s.d.Storage.Delete(ctx, k); err != nil {
			slog.WarnContext(ctx, "places: delete photo object", "key", k, "err", err)
		}
	}
}

type photoReq struct {
	Key string `json:"key"`
}

func (s *Service) addPhoto(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req photoReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out *Place
	requeued := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		p, err := s.lockOwned(ctx, tx, id, me.UserID)
		if err != nil {
			return err
		}
		if err := insertPhoto(ctx, tx, p, req.Key, me.UserID); err != nil {
			return err
		}
		// A new photo is public content: with approval on it goes through review.
		if next := s.requeue(p.Status); next != p.Status && p.Status == "approved" {
			requeued = true
			if _, err := tx.Exec(ctx, `UPDATE places SET status = 'pending', reviewed_by = NULL, reviewed_at = NULL,
				updated_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
		}
		out, err = s.loadPlace(ctx, tx, `p.id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	if requeued {
		s.announcePending(ctx, id)
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func insertPhoto(ctx context.Context, tx pgx.Tx, p *Place, key string, uploader uuid.UUID) error {
	if err := checkPhotoKeys([]string{key}, uploader, len(p.Photos)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO place_photos (place_id, key, uploaded_by, position) VALUES ($1, $2, $3,
		(SELECT COALESCE(max(position) + 1, 0) FROM place_photos WHERE place_id = $1))`, p.ID, key, uploader)
	if db.IsUniqueViolation(err) {
		return httpx.Conflict("That photo is already attached to a place.")
	}
	return err
}

func (s *Service) removePhoto(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
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
		if _, err := s.lockOwned(ctx, tx, id, me.UserID); err != nil {
			return err
		}
		var err error
		key, err = deletePhotoRow(ctx, tx, id, photoID)
		return err
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, []string{key})
	httpx.NoContent(w)
	return nil
}

func deletePhotoRow(ctx context.Context, tx pgx.Tx, placeID, photoID uuid.UUID) (string, error) {
	var key string
	err := tx.QueryRow(ctx, `DELETE FROM place_photos WHERE id = $1 AND place_id = $2 RETURNING key`, photoID, placeID).Scan(&key)
	if db.IsNoRows(err) {
		return "", httpx.NotFound("Photo not found.")
	}
	return key, err
}

// PlaceView is a place with the caller's relation to it.
type PlaceView struct {
	Place    *Place `json:"place"`
	Owned    bool   `json:"owned"`
	Favorite bool   `json:"favorite"`
}

// viewPlace is GET /v1/places/{id}/viewer: an approved place, or one of the
// caller's own in any status, with whether they own it and saved it.
func (s *Service) viewPlace(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.loadPlace(ctx, s.d.Pool, `p.id = $1 AND (p.status = 'approved' OR p.owner_id = $2)`, id, me.UserID)
	if err != nil {
		return err
	}
	out := PlaceView{Place: p}
	if err := s.d.Pool.QueryRow(ctx, `SELECT COALESCE(owner_id = $2, false),
		EXISTS (SELECT 1 FROM place_favorites WHERE user_id = $2 AND place_id = $1)
		FROM places WHERE id = $1`, id, me.UserID).Scan(&out.Owned, &out.Favorite); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

type mineCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// listMine is GET /v1/places/mine: the caller's submissions in every status.
func (s *Service) listMine(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c mineCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+placeCols+` FROM places p
		WHERE p.owner_id = $1 AND ($2::timestamptz IS NULL OR (p.created_at, p.id) < ($2, $3))
		ORDER BY p.created_at DESC, p.id DESC LIMIT $4`, me.UserID, after, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Place, error) { return scanPlace(row) })
	if err != nil {
		return err
	}
	page := httpx.NewPage(list, p.Limit, func(pl Place) any { return mineCursor{pl.CreatedAt, pl.ID} })
	ptrs := make([]*Place, len(page.Items))
	for i := range page.Items {
		ptrs[i] = &page.Items[i]
	}
	if err := s.attachPhotos(ctx, s.d.Pool, ptrs); err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// ---- favourites ------------------------------------------------------------------

// FavoritePlace is a favourite with when it was added.
type FavoritePlace struct {
	PlaceSummary
	FavoritedAt time.Time `json:"favorited_at"`
}

func (s *Service) addFavorite(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `
		INSERT INTO place_favorites (user_id, place_id)
		SELECT $1, id FROM places WHERE id = $2 AND status = 'approved'
		ON CONFLICT DO NOTHING`, me.UserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var ok bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM place_favorites WHERE user_id = $1 AND place_id = $2)`,
			me.UserID, id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("Place not found.")
		}
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) removeFavorite(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM place_favorites WHERE user_id = $1 AND place_id = $2`, me.UserID, id); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type favCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// listFavorites is GET /v1/places/favorites: approved favourites, newest first.
func (s *Service) listFavorites(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	pp := httpx.ParsePage(r)
	var c favCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, NULL::float8, f.created_at
		FROM place_favorites f JOIN places p ON p.id = f.place_id
		WHERE f.user_id = $1 AND p.status = 'approved'
		  AND ($2::timestamptz IS NULL OR (f.created_at, f.place_id) < ($2, $3))
		ORDER BY f.created_at DESC, f.place_id DESC LIMIT $4`, me.UserID, after, c.ID, pp.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (FavoritePlace, error) {
		var f FavoritePlace
		var key *string
		p := &f.PlaceSummary
		if err := row.Scan(&p.ID, &p.Name, &p.Category, &p.Address, &p.Lat, &p.Lng, &key, &p.DistanceM, &f.FavoritedAt); err != nil {
			return f, err
		}
		if key != nil {
			u := s.d.Storage.PublicURL(*key)
			p.PhotoURL = &u
		}
		return f, nil
	})
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, pp.Limit, func(f FavoritePlace) any { return favCursor{f.FavoritedAt, f.ID} }))
	return nil
}

// favoriteIDs is GET /v1/places/favorites/ids: ids for marking map pins
// (newest 1000).
func (s *Service) favoriteIDs(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	rows, err := s.d.Pool.Query(r.Context(), `SELECT place_id FROM place_favorites WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 1000`, me.UserID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	httpx.OK(w, map[string][]uuid.UUID{"ids": ids})
	return nil
}

// announcePending tells the admin console a place awaits review (best-effort).
func (s *Service) announcePending(ctx context.Context, id uuid.UUID) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), realtime.AdminLiveTopic, "places.pending",
		map[string]uuid.UUID{"id": id}); err != nil {
		slog.WarnContext(ctx, "places: publish pending", "place_id", id, "err", err)
	}
}
