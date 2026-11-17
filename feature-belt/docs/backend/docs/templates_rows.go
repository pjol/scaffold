package docs

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Row templates are rows of their database marked is_template: preset
// values in props (with "@me" / "@today" resolved when used) and body blocks
// edited in the normal page editor. A view may name one as its default; a
// repeating template adds a row on a schedule as the person who set it up,
// with that person's access at the time.

// Repeat is a template's schedule.
type Repeat struct {
	Every    string `json:"every"`              // day | week | month
	Interval int    `json:"interval"`           // every N units (1–12)
	Weekdays []int  `json:"weekdays,omitempty"` // week: 0 Monday … 6 Sunday
	Day      int    `json:"day,omitempty"`      // month: day of month (1–28; 0 = the template's day)
	Time     string `json:"time"`               // HH:MM
	TZ       string `json:"tz"`
	// By is who set the schedule (server-set): rows are added as them.
	By *uuid.UUID `json:"by,omitempty"`
}

// RowTemplate is a template as lists show it.
type RowTemplate struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	Icon         *string    `json:"icon"`
	Repeat       *Repeat    `json:"repeat"`
	RepeatNextAt *time.Time `json:"repeat_next_at"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

var hhmmRE = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (s *Service) isTemplateOf(ctx context.Context, q db.Querier, id, dbID uuid.UUID) bool {
	var ok bool
	_ = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs_pages WHERE id = $1 AND parent_id = $2 AND is_template AND is_row AND deleted_at IS NULL)`, id, dbID).Scan(&ok)
	return ok
}

// listRowTemplates: GET /docs/databases/{id}/templates (view).
func (s *Service) listRowTemplates(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, title, icon, repeat, repeat_next_at, created_by, updated_at FROM docs_pages
		WHERE parent_id = $1 AND is_template AND is_row AND deleted_at IS NULL AND access_level($2, 'docs.page', id, $3) >= 1
		ORDER BY position, id`, id, me, access.ViaAgent(ctx))
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RowTemplate{}
	for rows.Next() {
		var t RowTemplate
		var rep []byte
		if err := rows.Scan(&t.ID, &t.Title, &t.Icon, &rep, &t.RepeatNextAt, &t.CreatedBy, &t.UpdatedAt); err != nil {
			return err
		}
		if len(rep) > 0 {
			_ = json.Unmarshal(rep, &t.Repeat)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

type createTemplateReq struct {
	Title  string                     `json:"title"`
	Values map[string]json.RawMessage `json:"values"`
}

// createRowTemplate: POST /docs/databases/{id}/templates (edit): a new row
// template; open it to write its content and preset values.
func (s *Service) createRowTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req createTemplateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	if err := editable(dbp, true); err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_pages WHERE parent_id = $1 AND is_template AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n >= 50 {
		return httpx.Conflict("A database has up to 50 templates.")
	}
	out, err := s.insertRow(ctx, me, dbp, createRowReq{Title: req.Title, Values: req.Values}, true)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

type updateTemplateReq struct {
	Repeat json.RawMessage `json:"repeat"` // Repeat, or null to stop repeating
}

// updateRowTemplate: PATCH /docs/templates/{id} (edit on the template): set or
// clear its repeat schedule. Its title, values and content change through
// the page routes like any row.
func (s *Service) updateRowTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateTemplateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	t, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if !t.IsTemplate || !t.IsRow || t.ParentID == nil {
		return httpx.NotFound("Template not found.")
	}
	if err := editable(t, false); err != nil {
		return err
	}
	if access.ViaAgent(ctx) {
		return httpx.Forbidden("AI agents can't schedule templates.")
	}
	var rep *Repeat
	if len(req.Repeat) > 0 && string(req.Repeat) != "null" {
		var in Repeat
		if err := json.Unmarshal(req.Repeat, &in); err != nil {
			return httpx.BadRequest("repeat must be {every, interval, weekdays?, day?, time, tz}.")
		}
		if rep, err = cleanRepeat(in); err != nil {
			return err
		}
	}
	var next *time.Time
	if rep != nil {
		n := nextRepeat(*rep, time.Now())
		next = &n
		rep.By = &me
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET repeat = $2, repeat_next_at = $3, updated_at = now(), updated_by = $4 WHERE id = $1`,
		id, jsonOrNil(rep), next, me); err != nil {
		return err
	}
	s.publish(ctx, pageTopic(*t.ParentID), EventSchema, map[string]any{"database_id": *t.ParentID, "templates": true})
	httpx.OK(w, RowTemplate{ID: t.ID, Title: t.Title, Icon: t.Icon, Repeat: rep, RepeatNextAt: next, CreatedBy: &me, UpdatedAt: time.Now()})
	return nil
}

func cleanRepeat(in Repeat) (*Repeat, error) {
	if in.Every != "day" && in.Every != "week" && in.Every != "month" {
		return nil, httpx.BadRequest("Repeat every day, week or month.")
	}
	if in.Interval < 1 || in.Interval > 12 {
		in.Interval = 1
	}
	if !hhmmRE.MatchString(in.Time) {
		return nil, httpx.BadRequest("Choose a time as HH:MM.")
	}
	if in.TZ == "" || fLoadTZ(in.TZ) == nil {
		return nil, httpx.BadRequest("That time zone isn't known.")
	}
	var days []int
	for _, d := range in.Weekdays {
		if d >= 0 && d <= 6 && !slices.Contains(days, d) {
			days = append(days, d)
		}
	}
	slices.Sort(days)
	if in.Every == "week" && len(days) == 0 {
		return nil, httpx.BadRequest("Choose the weekdays it repeats on.")
	}
	if in.Every != "week" {
		days = nil
	}
	if in.Every != "month" || in.Day < 0 || in.Day > 28 {
		in.Day = 0
	}
	if in.Every == "month" && in.Day == 0 {
		in.Day = 1
	}
	in.Weekdays = days
	return &in, nil
}

// nextRepeat is the first run of r strictly after t.
func nextRepeat(r Repeat, t time.Time) time.Time {
	loc := loadTZ(r.TZ)
	hh, mm := 9, 0
	if len(r.Time) == 5 {
		hh = int(r.Time[0]-'0')*10 + int(r.Time[1]-'0')
		mm = int(r.Time[3]-'0')*10 + int(r.Time[4]-'0')
	}
	local := t.In(loc)
	at := func(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc) }
	switch r.Every {
	case "week":
		// Weeks counted from the Monday of t's week; interval skips weeks.
		monday := local.AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
		for w := 0; w < 60; w += max(r.Interval, 1) {
			for _, wd := range r.Weekdays {
				c := at(monday.AddDate(0, 0, w*7+wd))
				if c.After(t) {
					return c
				}
			}
		}
	case "month":
		for m := 0; m < 60; m += max(r.Interval, 1) {
			c := time.Date(local.Year(), local.Month()+time.Month(m), r.Day, hh, mm, 0, 0, loc)
			if c.After(t) {
				return c
			}
		}
	default:
		c := at(local)
		for !c.After(t) {
			c = at(c.AddDate(0, 0, max(r.Interval, 1)))
		}
		return c
	}
	return t.Add(24 * time.Hour)
}

// runRepeatingTemplates adds the rows repeating templates are due to make,
// as each template's scheduler; a scheduler who can no longer edit the
// database stops the repeat.
func (s *Service) runRepeatingTemplates(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT id, parent_id, (repeat->>'by')::uuid, repeat FROM docs_pages
		WHERE repeat_next_at <= now() AND is_template AND deleted_at IS NULL ORDER BY repeat_next_at LIMIT 50`)
	if err != nil {
		return err
	}
	type due struct {
		ID, DB uuid.UUID
		By     *uuid.UUID
		Rep    []byte
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.ID, &d.DB, &d.By, &d.Rep); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	rows.Close()
	for _, d := range list {
		var rep Repeat
		_ = json.Unmarshal(d.Rep, &rep)
		next := nextRepeat(rep, time.Now())
		// Claim the run first so a slow insert never repeats it.
		tag, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET repeat_next_at = $2 WHERE id = $1 AND repeat_next_at <= now()`, d.ID, next)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		stop := func() {
			_, _ = s.d.Pool.Exec(ctx, `UPDATE docs_pages SET repeat_next_at = NULL WHERE id = $1`, d.ID)
		}
		if d.By == nil {
			stop()
			continue
		}
		dbp, err := requirePage(ctx, s.d.Pool, *d.By, d.DB, access.Edit)
		if err != nil || dbp.DeletedAt != nil {
			stop()
			continue
		}
		if _, err := s.insertRow(ctx, *d.By, dbp, createRowReq{TemplateID: &d.ID, TZ: rep.TZ}, false); err != nil {
			slogWarn(ctx, "docs: repeating template", err)
		}
	}
	return nil
}
