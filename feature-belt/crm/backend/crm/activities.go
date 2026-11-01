package crm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// Activity types and the outcomes each logged type accepts.
var (
	activityTypes = []string{"note", "call", "email", "meeting", "task"}
	outcomes      = map[string][]string{
		"call":    {"connected", "no_answer", "left_voicemail", "busy", "wrong_number"},
		"meeting": {"scheduled", "completed", "rescheduled", "no_show", "canceled"},
	}
	priorities = []string{"none", "low", "medium", "high"}

	userMentionRe = regexp.MustCompile(`<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>`)
	teamMentionRe = regexp.MustCompile(`<@team:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>`)
)

// Mention is a person or team named in a body as <@id> or <@team:id>.
type Mention struct {
	ID   uuid.UUID `json:"id"`
	Type string    `json:"type"` // user | team
	Name string    `json:"name"`
}

// Activity is a note, logged call/email/meeting or task. Records lists the
// linked records the caller can view (the one it was logged on first).
type Activity struct {
	ID              uuid.UUID   `json:"id"`
	OrgID           uuid.UUID   `json:"org_id"`
	Type            string      `json:"type"`
	Records         []RecordRef `json:"records"`
	Subject         string      `json:"subject"`
	Body            string      `json:"body"`
	Mentions        []Mention   `json:"mentions"`
	OccurredAt      time.Time   `json:"occurred_at"`
	DurationMinutes *int        `json:"duration_minutes"`
	Outcome         *string     `json:"outcome"`
	Direction       *string     `json:"direction"`
	DueAt           *time.Time  `json:"due_at"`
	Assignee        *UserRef    `json:"assignee"`
	ReminderAt      *time.Time  `json:"reminder_at"`
	DoneAt          *time.Time  `json:"done_at"`
	Priority        string      `json:"priority"`
	CreatedBy       *UserRef    `json:"created_by"`
	Agent           *string     `json:"agent"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	CanEdit         bool        `json:"can_edit"`
	// Unreachable names people mentioned who can't see the record (not notified).
	Unreachable []string `json:"unreachable,omitempty"`

	primary    refKey
	mentionIDs []uuid.UUID
	authorID   *uuid.UUID
	assigneeID *uuid.UUID
}

type refKey struct {
	Type objectType
	ID   uuid.UUID
}

func (k refKey) kind() string { return k.Type.kind() }

// Event is a system entry on a timeline.
type Event struct {
	Kind  string         `json:"kind"`
	Actor *UserRef       `json:"actor"`
	Agent *string        `json:"agent"`
	Data  map[string]any `json:"data"`
}

// TimelineItem is an activity or an event.
type TimelineItem struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"` // activity | event
	At       time.Time `json:"at"`
	Activity *Activity `json:"activity,omitempty"`
	Event    *Event    `json:"event,omitempty"`
}

// ---- loading -------------------------------------------------------------------------

const activityCols = `a.id, a.org_id, a.type, a.record_type, a.record_id, a.subject, a.body, a.occurred_at, a.duration_minutes, a.outcome,
	a.direction, a.due_at, a.assignee_id, au.display_name, au.avatar_url, a.reminder_at, a.done_at, a.priority, a.mentions,
	a.created_by, cu.display_name, cu.avatar_url, a.agent, a.created_at, a.updated_at`

const activityFrom = `FROM crm_activities a
	LEFT JOIN users au ON au.id = a.assignee_id AND au.status <> 'deleted'
	LEFT JOIN users cu ON cu.id = a.created_by AND cu.status <> 'deleted'`

func scanActivity(row pgx.Row, extra ...any) (*Activity, error) {
	a := &Activity{}
	var rt string
	var aName, aAvatar, cName, cAvatar *string
	dest := []any{&a.ID, &a.OrgID, &a.Type, &rt, &a.primary.ID, &a.Subject, &a.Body, &a.OccurredAt, &a.DurationMinutes, &a.Outcome,
		&a.Direction, &a.DueAt, &a.assigneeID, &aName, &aAvatar, &a.ReminderAt, &a.DoneAt, &a.Priority, &a.mentionIDs,
		&a.authorID, &cName, &cAvatar, &a.Agent, &a.CreatedAt, &a.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	a.primary.Type = objectType(rt)
	if a.assigneeID != nil && aName != nil {
		a.Assignee = &UserRef{ID: *a.assigneeID, Name: *aName, AvatarURL: aAvatar}
	}
	if a.authorID != nil && cName != nil {
		a.CreatedBy = &UserRef{ID: *a.authorID, Name: *cName, AvatarURL: cAvatar}
	}
	return a, nil
}

// recordLevels returns the caller's level on each record (agent-capped).
func recordLevels(ctx context.Context, q db.Querier, userID uuid.UUID, keys []refKey) (map[refKey]access.Level, error) {
	out := map[refKey]access.Level{}
	if len(keys) == 0 {
		return out, nil
	}
	kinds := make([]string, len(keys))
	ids := make([]uuid.UUID, len(keys))
	for i, k := range keys {
		kinds[i], ids[i] = k.kind(), k.ID
	}
	rows, err := q.Query(ctx, `SELECT k, i, access_level($1, k, i, $2) FROM unnest($3::text[], $4::uuid[]) AS t (k, i)`,
		userID, access.ViaAgent(ctx), kinds, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var id uuid.UUID
		var lvl int
		if err := rows.Scan(&kind, &id, &lvl); err != nil {
			return nil, err
		}
		o, _ := objectByKind(kind)
		out[refKey{o, id}] = access.Level(lvl)
	}
	return out, rows.Err()
}

// finishActivities fills linked records (visible only), mentions and
// can_edit, and drops activities the caller can't see on any record.
func (s *Service) finishActivities(ctx context.Context, q db.Querier, userID uuid.UUID, list []*Activity) ([]*Activity, error) {
	if len(list) == 0 {
		return list, nil
	}
	ids := make([]uuid.UUID, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	rows, err := q.Query(ctx, `SELECT activity_id, record_type, record_id FROM crm_activity_records WHERE activity_id = ANY ($1) ORDER BY created_at, record_type, record_id`, ids)
	if err != nil {
		return nil, err
	}
	links := map[uuid.UUID][]refKey{}
	byType := map[objectType][]uuid.UUID{}
	for rows.Next() {
		var aid, rid uuid.UUID
		var rt string
		if err := rows.Scan(&aid, &rt, &rid); err != nil {
			rows.Close()
			return nil, err
		}
		k := refKey{objectType(rt), rid}
		links[aid] = append(links[aid], k)
		byType[k.Type] = append(byType[k.Type], rid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	visible := map[refKey]RecordRef{}
	for o, rids := range byType {
		refs, err := s.visibleRefs(ctx, q, userID, o, rids)
		if err != nil {
			return nil, err
		}
		for id, ref := range refs {
			visible[refKey{o, id}] = ref
		}
	}
	primaries := []refKey{}
	for _, a := range list {
		if !slices.Contains(primaries, a.primary) {
			primaries = append(primaries, a.primary)
		}
	}
	levels, err := recordLevels(ctx, q, userID, primaries)
	if err != nil {
		return nil, err
	}
	mentionSet := map[uuid.UUID]bool{}
	for _, a := range list {
		for _, m := range a.mentionIDs {
			mentionSet[m] = true
		}
	}
	names, err := s.mentionNames(ctx, q, mentionSet)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, a := range list {
		a.Records = []RecordRef{}
		if ref, ok := visible[a.primary]; ok {
			a.Records = append(a.Records, ref)
		}
		for _, k := range links[a.ID] {
			if ref, ok := visible[k]; ok && k != a.primary {
				a.Records = append(a.Records, ref)
			}
		}
		if len(a.Records) == 0 {
			continue
		}
		a.Mentions = []Mention{}
		for _, m := range a.mentionIDs {
			if n, ok := names[m]; ok {
				a.Mentions = append(a.Mentions, n)
			}
		}
		lvl := levels[a.primary]
		mine := a.authorID != nil && *a.authorID == userID
		a.CanEdit = lvl >= access.Edit || (mine && lvl >= access.Comment)
		out = append(out, a)
	}
	return out, nil
}

func (s *Service) mentionNames(ctx context.Context, q db.Querier, set map[uuid.UUID]bool) (map[uuid.UUID]Mention, error) {
	out := map[uuid.UUID]Mention{}
	if len(set) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	rows, err := q.Query(ctx, `
		SELECT id, 'user', display_name FROM users WHERE id = ANY ($1) AND status <> 'deleted'
		UNION ALL SELECT id, 'team', name FROM access_teams WHERE id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Mention
		if err := rows.Scan(&m.ID, &m.Type, &m.Name); err != nil {
			return nil, err
		}
		out[m.ID] = m
	}
	return out, rows.Err()
}

func (s *Service) loadActivity(ctx context.Context, q db.Querier, userID, id uuid.UUID) (*Activity, error) {
	a, err := scanActivity(q.QueryRow(ctx, `SELECT `+activityCols+` `+activityFrom+` WHERE a.id = $1`, id))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Activity not found.")
	}
	if err != nil {
		return nil, err
	}
	list, err := s.finishActivities(ctx, q, userID, []*Activity{a})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("Activity not found.")
	}
	return list[0], nil
}

// ---- timeline ------------------------------------------------------------------------

var timelineFilters = map[string][]string{
	"notes": {"note"}, "calls": {"call"}, "emails": {"email"}, "meetings": {"meeting"}, "tasks": {"task"},
	"activities": activityTypes,
}

// TimelinePage is GET /v1/crm/{type}/{id}/timeline. Upcoming (open tasks on
// the record, soonest first) comes with the first page.
type TimelinePage struct {
	Items      []TimelineItem `json:"items"`
	NextCursor *string        `json:"next_cursor"`
	Upcoming   []*Activity    `json:"upcoming"`
}

func (s *Service) timeline(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		id, err := idParam(r, "id")
		if err != nil {
			return err
		}
		p := httpx.ParsePage(r)
		page, err := s.loadTimeline(ctx, me(r), o, id, r.URL.Query().Get("filter"), p.Cursor, p.Limit)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

type timeCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) loadTimeline(ctx context.Context, userID uuid.UUID, o objectType, id uuid.UUID, filter, cursor string, limit int) (*TimelinePage, error) {
	if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, o, id, access.View); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	types := activityTypes
	withEvents := true
	switch {
	case filter == "" || filter == "all":
	case filter == "changes":
		types = nil
	default:
		t, ok := timelineFilters[filter]
		if !ok {
			return nil, httpx.BadRequest("filter must be all, activities, notes, calls, emails, meetings, tasks or changes.")
		}
		types, withEvents = t, false
	}
	var c timeCursor
	if err := httpx.DecodeCursor(cursor, &c); err != nil {
		return nil, err
	}
	args := []any{string(o), id, types, withEvents, limit + 1}
	keyset := ""
	if cursor != "" {
		args = append(args, c.T, c.ID)
		keyset = "WHERE (t.at, t.id) < ($6, $7)"
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT t.src, t.id, t.at FROM (
			SELECT 'activity'::text AS src, a.id, a.occurred_at AS at
			FROM crm_activity_records l JOIN crm_activities a ON a.id = l.activity_id
			WHERE l.record_type = $1 AND l.record_id = $2 AND a.type = ANY ($3::text[])
			UNION ALL
			SELECT 'event', e.id, e.created_at FROM crm_events e
			WHERE $4 AND e.record_type = $1 AND e.record_id = $2
		) t `+keyset+` ORDER BY t.at DESC, t.id DESC LIMIT $5`, args...)
	if err != nil {
		return nil, err
	}
	type entry struct {
		src string
		id  uuid.UUID
		at  time.Time
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.src, &e.id, &e.at); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page := &TimelinePage{Items: []TimelineItem{}}
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[limit-1]
		page.NextCursor = httpx.EncodeCursor(timeCursor{T: last.at, ID: last.id})
	}
	var actIDs, evIDs []uuid.UUID
	for _, e := range entries {
		if e.src == "activity" {
			actIDs = append(actIDs, e.id)
		} else {
			evIDs = append(evIDs, e.id)
		}
	}
	acts, err := s.activitiesByID(ctx, userID, actIDs)
	if err != nil {
		return nil, err
	}
	evs, err := s.eventsByID(ctx, userID, evIDs)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		item := TimelineItem{ID: e.id, Kind: e.src, At: e.at}
		if e.src == "activity" {
			if item.Activity = acts[e.id]; item.Activity == nil {
				continue
			}
		} else if item.Event = evs[e.id]; item.Event == nil {
			continue
		}
		page.Items = append(page.Items, item)
	}
	if cursor == "" {
		up, err := s.upcomingTasks(ctx, userID, o, id)
		if err != nil {
			return nil, err
		}
		page.Upcoming = up
	}
	return page, nil
}

func (s *Service) activitiesByID(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Activity, error) {
	out := map[uuid.UUID]*Activity{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+activityCols+` `+activityFrom+` WHERE a.id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	var list []*Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list, err = s.finishActivities(ctx, s.d.Pool, userID, list)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		out[a.ID] = a
	}
	return out, nil
}

func (s *Service) upcomingTasks(ctx context.Context, userID uuid.UUID, o objectType, id uuid.UUID) ([]*Activity, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+activityCols+` `+activityFrom+`
		JOIN crm_activity_records l ON l.activity_id = a.id AND l.record_type = $1 AND l.record_id = $2
		WHERE a.type = 'task' AND a.done_at IS NULL ORDER BY a.due_at NULLS LAST, a.id LIMIT 20`, string(o), id)
	if err != nil {
		return nil, err
	}
	var list []*Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list, err = s.finishActivities(ctx, s.d.Pool, userID, list)
	if list == nil {
		list = []*Activity{}
	}
	return list, err
}

// eventsByID loads timeline events; an associated record the caller can't
// view is named only by its type.
func (s *Service) eventsByID(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Event, error) {
	out := map[uuid.UUID]*Event{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT e.id, e.kind, e.data, e.agent, u.id, u.display_name, u.avatar_url FROM crm_events e
		LEFT JOIN users u ON u.id = e.actor_id AND u.status <> 'deleted' WHERE e.id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	others := map[objectType][]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var ev Event
		var data []byte
		var uid *uuid.UUID
		var uname, avatar *string
		if err := rows.Scan(&id, &ev.Kind, &data, &ev.Agent, &uid, &uname, &avatar); err != nil {
			rows.Close()
			return nil, err
		}
		ev.Data = map[string]any{}
		_ = json.Unmarshal(data, &ev.Data)
		if uid != nil && uname != nil {
			ev.Actor = &UserRef{ID: *uid, Name: *uname, AvatarURL: avatar}
		}
		if ot, ok := ev.Data["other_type"].(string); ok {
			if oid, err := uuid.Parse(asString(ev.Data["other_id"])); err == nil {
				others[objectType(ot)] = append(others[objectType(ot)], oid)
			}
		}
		e := ev
		out[id] = &e
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	visible := map[refKey]bool{}
	for o, ids := range others {
		if _, ok := objectByKind(o.kind()); !ok {
			continue
		}
		refs, err := s.visibleRefs(ctx, s.d.Pool, userID, o, ids)
		if err != nil {
			return nil, err
		}
		for id := range refs {
			visible[refKey{o, id}] = true
		}
	}
	for _, ev := range out {
		ot, ok := ev.Data["other_type"].(string)
		if !ok {
			continue
		}
		oid, _ := uuid.Parse(asString(ev.Data["other_id"]))
		if !visible[refKey{objectType(ot), oid}] {
			delete(ev.Data, "other_id")
			delete(ev.Data, "other_name")
			ev.Data["other_hidden"] = true
		}
	}
	return out, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// ---- writes --------------------------------------------------------------------------

type recordInput struct {
	Type string    `json:"type"`
	ID   uuid.UUID `json:"id"`
}

type activityReq struct {
	Type            string        `json:"type"`
	Subject         *string       `json:"subject"`
	Body            *string       `json:"body"`
	OccurredAt      *time.Time    `json:"occurred_at"`
	DurationMinutes *int          `json:"duration_minutes"`
	Outcome         *string       `json:"outcome"`
	Direction       *string       `json:"direction"`
	DueAt           *time.Time    `json:"due_at"`
	AssigneeID      *uuid.UUID    `json:"assignee_id"`
	ReminderAt      *time.Time    `json:"reminder_at"`
	Priority        *string       `json:"priority"`
	Done            *bool         `json:"done"`
	AlsoOn          []recordInput `json:"also_on"`
	// ClearDue / ClearReminder remove those task fields on update.
	ClearDue      bool `json:"clear_due"`
	ClearReminder bool `json:"clear_reminder"`
}

func (s *Service) createActivity(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := idParam(r, "id")
		if err != nil {
			return err
		}
		var req activityReq
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		a, err := s.logActivity(r.Context(), me(r), o, id, req)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, a)
		return nil
	}
}

// logActivity creates an activity on a record (comment needed there and on
// every also_on record). REST, the composer and agent tools share it.
func (s *Service) logActivity(ctx context.Context, userID uuid.UUID, o objectType, recordID uuid.UUID, req activityReq) (*Activity, error) {
	if !slices.Contains(activityTypes, req.Type) {
		return nil, httpx.BadRequest("type must be note, call, email, meeting or task.")
	}
	subject := strings.TrimSpace(deref(req.Subject))
	body := strings.TrimSpace(deref(req.Body))
	if len([]rune(subject)) > 200 {
		return nil, httpx.BadRequest("The subject can be at most 200 characters.")
	}
	if len([]rune(body)) > maxNoteChars {
		return nil, httpx.BadRequest("Notes can be at most 20,000 characters.")
	}
	switch req.Type {
	case "note":
		if body == "" {
			return nil, httpx.BadRequest("Write the note first.")
		}
	case "task":
		if subject == "" {
			return nil, httpx.BadRequest("Give the task a title.")
		}
	default:
		if body == "" && subject == "" && req.Outcome == nil {
			return nil, httpx.BadRequest("Add what happened before logging it.")
		}
	}
	if err := validateActivityFields(req.Type, req); err != nil {
		return nil, err
	}
	var (
		id       uuid.UUID
		orgID    uuid.UUID
		mentions []uuid.UUID
		assignee *uuid.UUID
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if orgID, _, err = s.requireRecord(ctx, tx, userID, o, recordID, access.Comment); err != nil {
			return err
		}
		links := []refKey{{o, recordID}}
		for _, x := range req.AlsoOn {
			ot, ok := parseObject(x.Type)
			if !ok {
				return httpx.BadRequest("also_on types are company, contact or deal.")
			}
			k := refKey{ot, x.ID}
			if slices.Contains(links, k) {
				continue
			}
			org, _, err := s.requireRecord(ctx, tx, userID, ot, x.ID, access.Comment)
			if err != nil {
				return err
			}
			if org != orgID {
				return httpx.BadRequest("Associate records from the same " + orgs.Noun() + ".")
			}
			links = append(links, k)
		}
		if len(links) > 20 {
			return httpx.BadRequest("Associate at most 20 records.")
		}
		if mentions, err = s.parseMentions(ctx, tx, orgID, body); err != nil {
			return err
		}
		occurred := time.Now()
		if req.OccurredAt != nil && req.Type != "task" {
			occurred = *req.OccurredAt
		}
		if req.Type == "task" {
			assignee = &userID
			if req.AssigneeID != nil {
				assignee = req.AssigneeID
			}
			if err := s.checkAssignee(ctx, tx, orgID, *assignee, o, recordID); err != nil {
				return err
			}
		}
		var done *time.Time
		if req.Type == "task" && req.Done != nil && *req.Done {
			now := time.Now()
			done = &now
		}
		priority := "none"
		if req.Priority != nil {
			priority = *req.Priority
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm_activities (org_id, type, record_type, record_id, subject, body, occurred_at, duration_minutes, outcome,
			                            direction, due_at, assignee_id, reminder_at, done_at, priority, mentions, agent, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18) RETURNING id`,
			orgID, req.Type, string(o), recordID, subject, body, occurred, req.DurationMinutes, req.Outcome, req.Direction,
			taskTime(req.Type, req.DueAt), assignee, taskTime(req.Type, req.ReminderAt), done, priority, mentions, agentName(ctx), userID).Scan(&id); err != nil {
			return err
		}
		for _, k := range links {
			if _, err := tx.Exec(ctx, `INSERT INTO crm_activity_records (activity_id, record_type, record_id, org_id) VALUES ($1, $2, $3, $4)`,
				id, string(k.Type), k.ID, orgID); err != nil {
				return err
			}
			if req.Type != "task" {
				if _, err := tx.Exec(ctx, `UPDATE `+k.Type.table()+` SET last_activity_at = GREATEST(COALESCE(last_activity_at, '-infinity'), $2) WHERE id = $1`,
					k.ID, occurred); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	a, err := s.loadActivity(ctx, s.d.Pool, userID, id)
	if err != nil {
		return nil, err
	}
	for _, rec := range a.Records {
		s.recordChanged(ctx, rec.Type, rec.ID, true)
	}
	a.Unreachable = s.notifyMentions(ctx, userID, orgID, a, mentions)
	if assignee != nil && *assignee != userID {
		s.notifyTask(ctx, userID, a)
	}
	return a, nil
}

func taskTime(typ string, t *time.Time) *time.Time {
	if typ != "task" {
		return nil
	}
	return t
}

func validateActivityFields(typ string, req activityReq) error {
	if req.Outcome != nil {
		allowed, ok := outcomes[typ]
		if !ok || !slices.Contains(allowed, *req.Outcome) {
			return httpx.BadRequest("That outcome doesn't fit this activity.")
		}
	}
	if req.Direction != nil && (*req.Direction != "inbound" && *req.Direction != "outbound" || (typ != "call" && typ != "email")) {
		return httpx.BadRequest("direction is inbound or outbound, for calls and emails.")
	}
	if req.DurationMinutes != nil && (*req.DurationMinutes < 0 || *req.DurationMinutes > 1440 || (typ != "call" && typ != "meeting")) {
		return httpx.BadRequest("Duration is 0 to 1,440 minutes, for calls and meetings.")
	}
	if req.Priority != nil && !slices.Contains(priorities, *req.Priority) {
		return httpx.BadRequest("priority is none, low, medium or high.")
	}
	if typ != "task" && (req.DueAt != nil || req.AssigneeID != nil || req.ReminderAt != nil || req.Done != nil) {
		return httpx.BadRequest("Due dates, assignees, reminders and done apply to tasks only.")
	}
	if req.OccurredAt != nil && req.OccurredAt.After(time.Now().Add(366*24*time.Hour)) {
		return httpx.BadRequest("That date is too far ahead.")
	}
	return nil
}

// checkAssignee: the assignee must be a member who can view the record (their
// own level, not an agent's).
func (s *Service) checkAssignee(ctx context.Context, q db.Querier, orgID, assignee uuid.UUID, o objectType, recordID uuid.UUID) error {
	var lvl int
	if err := q.QueryRow(ctx, `SELECT access_level($1, $2, $3, false)`, assignee, o.kind(), recordID).Scan(&lvl); err != nil {
		return err
	}
	if lvl < int(access.View) || !allowedHere(ctx, q, orgID, assignee, PermAccess) {
		return httpx.BadRequest("That person can't see this record. Share it with them first, or assign someone else.")
	}
	return nil
}

// parseMentions returns the members and teams of the org a body mentions.
func (s *Service) parseMentions(ctx context.Context, q db.Querier, orgID uuid.UUID, body string) ([]uuid.UUID, error) {
	var users, teams []uuid.UUID
	for _, m := range userMentionRe.FindAllStringSubmatch(body, 50) {
		if id, err := uuid.Parse(m[1]); err == nil && !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	for _, m := range teamMentionRe.FindAllStringSubmatch(body, 20) {
		if id, err := uuid.Parse(m[1]); err == nil && !slices.Contains(teams, id) {
			teams = append(teams, id)
		}
	}
	out := []uuid.UUID{}
	if len(users) > 0 {
		rows, err := q.Query(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, orgID, users)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		rows.Close()
	}
	if len(teams) > 0 {
		rows, err := q.Query(ctx, `SELECT id FROM access_teams WHERE org_id = $1 AND id = ANY ($2)`, orgID, teams)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		rows.Close()
	}
	return out, nil
}

// notifyMentions notifies mentioned people (and members of mentioned teams,
// up to 50) who can view the record; it returns the names of mentioned
// people who can't, so the author knows.
func (s *Service) notifyMentions(ctx context.Context, actor, orgID uuid.UUID, a *Activity, ids []uuid.UUID) []string {
	if len(ids) == 0 || len(a.Records) == 0 {
		return nil
	}
	rec := a.Records[0]
	rows, err := s.d.Pool.Query(ctx, `
		WITH targets AS (
			SELECT m.user_id, false AS via_team FROM org_members m WHERE m.org_id = $1 AND m.user_id = ANY ($2)
			UNION
			SELECT tm.user_id, true FROM access_team_members tm JOIN access_teams t ON t.id = tm.team_id
			WHERE t.org_id = $1 AND t.id = ANY ($2)
		)
		SELECT DISTINCT ON (x.user_id) x.user_id, x.via_team, u.display_name,
		       access_level(x.user_id, $3, $4, false) >= 1 AND access_permission_allowed($1, x.user_id, $5, $6)
		FROM targets x JOIN users u ON u.id = x.user_id AND u.status = 'active'
		WHERE x.user_id <> $7 ORDER BY x.user_id, x.via_team LIMIT 60`,
		orgID, ids, rec.Type.kind(), rec.ID, PermAccess, mustDefaults(PermAccess), actor)
	if err != nil {
		slog.WarnContext(ctx, "crm: mention targets", "err", err)
		return nil
	}
	defer rows.Close()
	var actorName string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, actor).Scan(&actorName)
	var unreachable []string
	sent := 0
	for rows.Next() {
		var uid uuid.UUID
		var viaTeam, canView bool
		var name string
		if err := rows.Scan(&uid, &viaTeam, &name, &canView); err != nil {
			return unreachable
		}
		if !canView {
			if !viaTeam {
				unreachable = append(unreachable, name)
			}
			continue
		}
		if sent >= 50 {
			continue
		}
		sent++
		s.send(ctx, notify.Notification{UserID: uid, Kind: NotifyMention, Title: actorName + " mentioned you on " + clip(rec.Name, 80),
			Body: clip(plainBody(a.Body, a.Mentions), 200), Link: rec.Type.link(rec.ID), Data: map[string]any{"type": rec.Type, "id": rec.ID, "activity_id": a.ID}})
	}
	return unreachable
}

func mustDefaults(key string) []string {
	p, _ := access.PermissionOf(key)
	return p.DefaultRoles
}

// plainBody writes mention tokens as @names.
func plainBody(body string, mentions []Mention) string {
	names := map[string]string{}
	for _, m := range mentions {
		names[m.ID.String()] = "@" + m.Name
	}
	body = userMentionRe.ReplaceAllStringFunc(body, func(t string) string {
		if n, ok := names[userMentionRe.FindStringSubmatch(t)[1]]; ok {
			return n
		}
		return "@someone"
	})
	return teamMentionRe.ReplaceAllStringFunc(body, func(t string) string {
		if n, ok := names[teamMentionRe.FindStringSubmatch(t)[1]]; ok {
			return n
		}
		return "@team"
	})
}

func (s *Service) notifyTask(ctx context.Context, actor uuid.UUID, a *Activity) {
	if a.Assignee == nil || len(a.Records) == 0 {
		return
	}
	var actorName string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, actor).Scan(&actorName)
	body := "On " + a.Records[0].Name
	if a.DueAt != nil {
		body += " · due " + a.DueAt.UTC().Format("Jan 2, 15:04 UTC")
	}
	s.send(ctx, notify.Notification{UserID: a.Assignee.ID, Kind: NotifyTaskAssigned, Title: actorName + " assigned you: " + clip(a.Subject, 80),
		Body: body, Link: a.Records[0].Type.link(a.Records[0].ID), Data: map[string]any{"type": a.Records[0].Type, "id": a.Records[0].ID, "activity_id": a.ID}})
}

// requireActivity loads an activity for a write: the caller must see its
// record; edits need edit there, or comment as its author (or, for done,
// being the assignee).
func (s *Service) requireActivity(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID, doneOnly bool) (*Activity, uuid.UUID, error) {
	a, err := scanActivity(tx.QueryRow(ctx, `SELECT `+activityCols+` `+activityFrom+` WHERE a.id = $1 FOR UPDATE OF a`, id))
	if db.IsNoRows(err) {
		return nil, uuid.Nil, httpx.NotFound("Activity not found.")
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	orgID, lvl, err := s.requireRecord(ctx, tx, userID, a.primary.Type, a.primary.ID, access.View)
	if err != nil {
		if e, ok := err.(*httpx.Error); ok && e.Status == http.StatusNotFound {
			return nil, uuid.Nil, httpx.NotFound("Activity not found.")
		}
		return nil, uuid.Nil, err
	}
	mine := a.authorID != nil && *a.authorID == userID
	assigned := a.assigneeID != nil && *a.assigneeID == userID
	switch {
	case lvl >= access.Edit, mine && lvl >= access.Comment, doneOnly && assigned:
		return a, orgID, nil
	}
	return nil, uuid.Nil, httpx.Forbidden("Only its author or someone who can edit the record can change this.")
}

// updateActivity: PATCH /v1/crm/activities/{activityID}
func (s *Service) updateActivity(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "activityID")
	if err != nil {
		return err
	}
	var req activityReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.editActivity(r.Context(), me(r), id, req)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

func (s *Service) editActivity(ctx context.Context, userID, id uuid.UUID, req activityReq) (*Activity, error) {
	doneOnly := req.Done != nil && req.Subject == nil && req.Body == nil && req.OccurredAt == nil && req.DurationMinutes == nil &&
		req.Outcome == nil && req.Direction == nil && req.DueAt == nil && req.AssigneeID == nil && req.ReminderAt == nil &&
		req.Priority == nil && !req.ClearDue && !req.ClearReminder && len(req.AlsoOn) == 0
	if len(req.AlsoOn) > 0 || req.Type != "" {
		return nil, httpx.BadRequest("An activity's type and records can't be changed.")
	}
	var (
		orgID      uuid.UUID
		before     *Activity
		newMention []uuid.UUID
		reassigned bool
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if before, orgID, err = s.requireActivity(ctx, tx, userID, id, doneOnly); err != nil {
			return err
		}
		if err := validateActivityFields(before.Type, req); err != nil {
			return err
		}
		sets := []string{"updated_at = now()"}
		args := []any{id}
		set := func(col string, v any) {
			args = append(args, v)
			sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
		}
		if req.Subject != nil {
			v := strings.TrimSpace(*req.Subject)
			if len([]rune(v)) > 200 || (before.Type == "task" && v == "") {
				return httpx.BadRequest("Give it a title of up to 200 characters.")
			}
			set("subject", v)
		}
		if req.Body != nil {
			v := strings.TrimSpace(*req.Body)
			if len([]rune(v)) > maxNoteChars || (before.Type == "note" && v == "") {
				return httpx.BadRequest("Notes need text, up to 20,000 characters.")
			}
			m, err := s.parseMentions(ctx, tx, orgID, v)
			if err != nil {
				return err
			}
			for _, x := range m {
				if !slices.Contains(before.mentionIDs, x) {
					newMention = append(newMention, x)
				}
			}
			set("body", v)
			set("mentions", m)
		}
		if req.OccurredAt != nil && before.Type != "task" {
			set("occurred_at", *req.OccurredAt)
		}
		if req.DurationMinutes != nil {
			set("duration_minutes", *req.DurationMinutes)
		}
		if req.Outcome != nil {
			set("outcome", *req.Outcome)
		}
		if req.Direction != nil {
			set("direction", *req.Direction)
		}
		if req.Priority != nil {
			set("priority", *req.Priority)
		}
		if req.DueAt != nil || req.ClearDue {
			set("due_at", req.DueAt)
		}
		if req.ReminderAt != nil || req.ClearReminder {
			set("reminder_at", req.ReminderAt)
			set("reminded_at", nil) // a new reminder time goes out again
		}
		if req.AssigneeID != nil {
			if err := s.checkAssignee(ctx, tx, orgID, *req.AssigneeID, before.primary.Type, before.primary.ID); err != nil {
				return err
			}
			reassigned = before.assigneeID == nil || *before.assigneeID != *req.AssigneeID
			set("assignee_id", *req.AssigneeID)
		}
		if req.Done != nil {
			if before.Type != "task" {
				return httpx.BadRequest("Only tasks can be done.")
			}
			if *req.Done {
				sets = append(sets, "done_at = COALESCE(done_at, now())")
			} else {
				sets = append(sets, "done_at = NULL")
			}
		}
		_, err = tx.Exec(ctx, `UPDATE crm_activities SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	a, err := s.loadActivity(ctx, s.d.Pool, userID, id)
	if err != nil {
		return nil, err
	}
	for _, rec := range a.Records {
		s.recordChanged(ctx, rec.Type, rec.ID, true)
	}
	a.Unreachable = s.notifyMentions(ctx, userID, orgID, a, newMention)
	if reassigned && a.Assignee != nil && a.Assignee.ID != userID {
		s.notifyTask(ctx, userID, a)
	}
	return a, nil
}

// deleteActivity: DELETE /v1/crm/activities/{activityID}
func (s *Service) deleteActivity(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "activityID")
	if err != nil {
		return err
	}
	var recs []refKey
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, _, err := s.requireActivity(ctx, tx, me(r), id, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT record_type, record_id FROM crm_activity_records WHERE activity_id = $1`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k refKey
			var rt string
			if err := rows.Scan(&rt, &k.ID); err != nil {
				rows.Close()
				return err
			}
			k.Type = objectType(rt)
			recs = append(recs, k)
		}
		rows.Close()
		_, err = tx.Exec(ctx, `DELETE FROM crm_activities WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	for _, k := range recs {
		s.recordChanged(ctx, k.Type, k.ID, true)
	}
	httpx.NoContent(w)
	return nil
}

// ---- tasks -----------------------------------------------------------------------------

// TaskSummary counts the caller's open tasks by when they're due.
type TaskSummary struct {
	Overdue  int `json:"overdue"`
	Today    int `json:"today"`
	Upcoming int `json:"upcoming"`
}

// taskVisible is the condition that the caller can view a record the task is on.
const taskVisible = `EXISTS (SELECT 1 FROM crm_activity_records l WHERE l.activity_id = a.id
	AND access_level($1, 'crm.' || l.record_type, l.record_id, $2) >= 1)`

// listTasks: GET /v1/crm/orgs/{orgID}/tasks?bucket=overdue|today|upcoming|done|open&assignee=me|any|<id>&tz=&cursor=&limit=
func (s *Service) listTasks(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	p := httpx.ParsePage(r)
	page, err := s.tasks(r.Context(), me(r), orgParam(r), q.Get("bucket"), q.Get("assignee"), q.Get("tz"), p.Cursor, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

type taskCursor struct {
	T  *time.Time `json:"t"`
	ID uuid.UUID  `json:"id"`
}

func (s *Service) tasks(ctx context.Context, userID, orgID uuid.UUID, bucket, assignee, tz, cursor string, limit int) (*httpx.Page[*Activity], error) {
	zone, err := location(tz)
	if err != nil {
		return nil, err
	}
	b := newSQL(ctx, userID)
	conds := []string{"a.org_id = " + b.arg(orgID), "a.type = 'task'", taskVisible}
	switch assignee {
	case "", "me":
		conds = append(conds, "a.assignee_id = $1")
	case "any":
	default:
		id, err := uuid.Parse(assignee)
		if err != nil {
			return nil, httpx.BadRequest("assignee is me, any or a person's id.")
		}
		conds = append(conds, "a.assignee_id = "+b.arg(id))
	}
	dayStart := func() string { // start of today in the caller's zone (the parameter is added only when used)
		z := b.arg(zone) + "::text"
		return "(date_trunc('day', now() AT TIME ZONE " + z + ") AT TIME ZONE " + z + ")"
	}
	order, sortCol := "a.due_at ASC NULLS LAST, a.id ASC", "a.due_at"
	switch bucket {
	case "overdue":
		conds = append(conds, "a.done_at IS NULL", "a.due_at < "+dayStart())
	case "today":
		ds := dayStart()
		conds = append(conds, "a.done_at IS NULL", "a.due_at >= "+ds, "a.due_at < "+ds+" + interval '1 day'")
	case "upcoming":
		conds = append(conds, "a.done_at IS NULL", "(a.due_at IS NULL OR a.due_at >= "+dayStart()+" + interval '1 day')")
	case "", "open":
		conds = append(conds, "a.done_at IS NULL")
	case "done":
		conds = append(conds, "a.done_at IS NOT NULL")
		order, sortCol = "a.done_at DESC, a.id DESC", "a.done_at"
	default:
		return nil, httpx.BadRequest("bucket is overdue, today, upcoming, open or done.")
	}
	if cursor != "" {
		var c taskCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil {
			return nil, err
		}
		id := b.arg(c.ID)
		if bucket == "done" {
			conds = append(conds, "(a.done_at, a.id) < ("+b.arg(c.T)+", "+id+")")
		} else if c.T == nil {
			conds = append(conds, "(a.due_at IS NULL AND a.id > "+id+")")
		} else {
			t := b.arg(*c.T)
			conds = append(conds, "(a.due_at > "+t+" OR (a.due_at = "+t+" AND a.id > "+id+") OR a.due_at IS NULL)")
		}
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+activityCols+`, `+sortCol+` `+activityFrom+` WHERE `+strings.Join(conds, " AND ")+
		` ORDER BY `+order+` LIMIT `+b.arg(limit+1), b.args...)
	if err != nil {
		return nil, err
	}
	var list []*Activity
	var keys []*time.Time
	for rows.Next() {
		var k *time.Time
		a, err := scanActivity(rows, &k)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list, keys = append(list, a), append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var next *string
	if len(list) > limit {
		list, keys = list[:limit], keys[:limit]
		next = httpx.EncodeCursor(taskCursor{T: keys[limit-1], ID: list[limit-1].ID})
	}
	list, err = s.finishActivities(ctx, s.d.Pool, userID, list)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []*Activity{}
	}
	return &httpx.Page[*Activity]{Items: list, NextCursor: next}, nil
}

// taskSummary: GET /v1/crm/orgs/{orgID}/tasks/summary?tz=
func (s *Service) taskSummary(w http.ResponseWriter, r *http.Request) error {
	sum, err := s.summary(r.Context(), me(r), orgParam(r), r.URL.Query().Get("tz"))
	if err != nil {
		return err
	}
	httpx.OK(w, sum)
	return nil
}

func (s *Service) summary(ctx context.Context, userID, orgID uuid.UUID, tz string) (*TaskSummary, error) {
	zone, err := location(tz)
	if err != nil {
		return nil, err
	}
	var sum TaskSummary
	err = s.d.Pool.QueryRow(ctx, `
		WITH d AS (SELECT (date_trunc('day', now() AT TIME ZONE $4) AT TIME ZONE $4) AS start)
		SELECT count(*) FILTER (WHERE a.due_at < d.start),
		       count(*) FILTER (WHERE a.due_at >= d.start AND a.due_at < d.start + interval '1 day'),
		       count(*) FILTER (WHERE a.due_at IS NULL OR a.due_at >= d.start + interval '1 day')
		FROM crm_activities a CROSS JOIN d
		WHERE a.org_id = $3 AND a.type = 'task' AND a.assignee_id = $1 AND a.done_at IS NULL AND `+taskVisible,
		userID, access.ViaAgent(ctx), orgID, zone).Scan(&sum.Overdue, &sum.Today, &sum.Upcoming)
	return &sum, err
}

// sendReminders sends each due task reminder once (reminded_at is set before
// sending), to an assignee who can still see the record.
func (s *Service) sendReminders(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM crm_activities
			WHERE type = 'task' AND reminder_at <= now() AND reminded_at IS NULL AND done_at IS NULL AND assignee_id IS NOT NULL
			ORDER BY reminder_at LIMIT 200 FOR UPDATE SKIP LOCKED
		)
		UPDATE crm_activities a SET reminded_at = now() FROM due WHERE a.id = due.id
		RETURNING a.id, a.org_id, a.assignee_id, a.subject, a.due_at, a.record_type, a.record_id`)
	if err != nil {
		return err
	}
	type due struct {
		id, org, assignee uuid.UUID
		subject           string
		dueAt             *time.Time
		rt                string
		rid               uuid.UUID
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.org, &d.assignee, &d.subject, &d.dueAt, &d.rt, &d.rid); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, d := range list {
		o := objectType(d.rt)
		var lvl int
		if err := s.d.Pool.QueryRow(ctx, `SELECT access_level($1, $2, $3, false)`, d.assignee, o.kind(), d.rid).Scan(&lvl); err != nil || lvl < 1 {
			continue
		}
		if !allowedHere(ctx, s.d.Pool, d.org, d.assignee, PermAccess) {
			continue
		}
		title, _ := recordTitle(ctx, s.d.Pool, o, d.rid)
		body := "On " + title
		if d.dueAt != nil {
			body += " · due " + d.dueAt.UTC().Format("Jan 2, 15:04 UTC")
		}
		s.send(ctx, notify.Notification{UserID: d.assignee, Kind: NotifyTaskReminder, Title: "Reminder: " + clip(d.subject, 80), Body: body,
			Link: o.link(d.rid), Data: map[string]any{"type": o, "id": d.rid, "activity_id": d.id}})
	}
	if len(list) > 0 {
		slog.InfoContext(ctx, "crm: task reminders sent", "count", len(list))
	}
	return nil
}
