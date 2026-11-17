package channels

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
)

// SearchHit is one matching message.
type SearchHit struct {
	Message *Message  `json:"message"`
	Channel BriefChan `json:"channel"`
}

// searchQuery is a parsed search box: free text plus filters.
//
//	in:#general        only that channel (by name)
//	from:@sam          messages by people whose name or email starts with "sam" (from:me = yours)
//	with:@sam          direct messages with them, and threads they took part in
//	to:me              messages that mention you, and direct messages to you
//	has:file|link|pin|reaction
//	is:thread|saved
//	before:2026-11-01  after:2026-11-01  on:2026-11-01  (also today, yesterday)
//	during:today|yesterday|week|month|year|2026|2026-11|november
type searchQuery struct {
	Text        string
	InChannel   string
	From        string
	With        string
	ToMe        bool
	HasFile     bool
	HasLink     bool
	HasPin      bool
	HasReaction bool
	IsThread    bool
	IsSaved     bool
	After       *time.Time // created at or after
	Before      *time.Time // created before
}

func (q searchQuery) filtered() bool {
	return q.InChannel != "" || q.From != "" || q.With != "" || q.ToMe || q.HasFile || q.HasLink || q.HasPin || q.HasReaction ||
		q.IsThread || q.IsSaved || q.After != nil || q.Before != nil
}

// escapeRe is a backslash escape of a markup character (\_ → _).
var escapeRe = regexp.MustCompile("\\\\([\\\\`*_~:\\[\\]()<>#-])")

var filterRe = regexp.MustCompile(`(?i)(^|\s)(in|from|with|to|has|is|before|after|on|during):(\S+)`)

var months = map[string]time.Month{
	"january": time.January, "jan": time.January, "february": time.February, "feb": time.February, "march": time.March, "mar": time.March,
	"april": time.April, "apr": time.April, "may": time.May, "june": time.June, "jun": time.June, "july": time.July, "jul": time.July,
	"august": time.August, "aug": time.August, "september": time.September, "sep": time.September, "sept": time.September,
	"october": time.October, "oct": time.October, "november": time.November, "nov": time.November, "december": time.December, "dec": time.December,
}

// searchDay reads a day: 2026-11-01, today, yesterday.
func searchDay(v string, now time.Time, loc *time.Location) (time.Time, bool) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch strings.ToLower(v) {
	case "today":
		return today, true
	case "yesterday":
		return today.AddDate(0, 0, -1), true
	}
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	return t, err == nil
}

// searchSpan reads a during: value as [from, to).
func searchSpan(v string, now time.Time, loc *time.Location) (time.Time, time.Time, bool) {
	now = now.In(loc)
	v = strings.ToLower(v)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch v {
	case "today", "yesterday":
		d, _ := searchDay(v, now, loc)
		return d, d.AddDate(0, 0, 1), true
	case "week":
		start := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7)) // Monday
		return start, start.AddDate(0, 0, 7), true
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), true
	case "year":
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(1, 0, 0), true
	}
	if t, err := time.ParseInLocation("2006-01", v, loc); err == nil {
		return t, t.AddDate(0, 1, 0), true
	}
	if t, err := time.ParseInLocation("2006", v, loc); err == nil {
		return t, t.AddDate(1, 0, 0), true
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t, t.AddDate(0, 0, 1), true
	}
	if m, ok := months[v]; ok {
		y := now.Year()
		if m > now.Month() {
			y-- // the most recent one
		}
		start := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), true
	}
	return time.Time{}, time.Time{}, false
}

func parseSearch(raw string, now time.Time, loc *time.Location) searchQuery {
	var q searchQuery
	narrow := func(from, to *time.Time) {
		if from != nil && (q.After == nil || from.After(*q.After)) {
			q.After = from
		}
		if to != nil && (q.Before == nil || to.Before(*q.Before)) {
			q.Before = to
		}
	}
	text := filterRe.ReplaceAllStringFunc(raw, func(tok string) string {
		m := filterRe.FindStringSubmatch(tok)
		key, val := strings.ToLower(m[2]), m[3]
		lower := strings.ToLower(val)
		switch {
		case key == "in":
			q.InChannel = strings.ToLower(strings.TrimPrefix(val, "#"))
		case key == "from":
			q.From = strings.ToLower(strings.TrimPrefix(val, "@"))
		case key == "with":
			q.With = strings.ToLower(strings.TrimPrefix(val, "@"))
		case key == "to" && (lower == "me" || lower == "@me"):
			q.ToMe = true
		case key == "has" && lower == "file":
			q.HasFile = true
		case key == "has" && lower == "link":
			q.HasLink = true
		case key == "has" && (lower == "pin" || lower == "pinned"):
			q.HasPin = true
		case key == "has" && (lower == "reaction" || lower == "reactions"):
			q.HasReaction = true
		case key == "is" && lower == "thread":
			q.IsThread = true
		case key == "is" && lower == "saved":
			q.IsSaved = true
		case key == "before":
			d, ok := searchDay(val, now, loc)
			if !ok {
				return tok
			}
			narrow(nil, &d)
		case key == "after":
			d, ok := searchDay(val, now, loc)
			if !ok {
				return tok
			}
			next := d.AddDate(0, 0, 1)
			narrow(&next, nil)
		case key == "on":
			d, ok := searchDay(val, now, loc)
			if !ok {
				return tok
			}
			next := d.AddDate(0, 0, 1)
			narrow(&d, &next)
		case key == "during":
			from, to, ok := searchSpan(val, now, loc)
			if !ok {
				return tok
			}
			narrow(&from, &to)
		default:
			return tok
		}
		return m[1]
	})
	q.Text = strings.Join(strings.Fields(text), " ")
	return q
}

// searchMessages finds messages caller may read in orgID (every org they're
// in when orgID is nil), newest first. channelID narrows to one channel.
func (s *Service) searchMessages(ctx context.Context, me uuid.UUID, orgID, channelID *uuid.UUID, raw string, loc *time.Location, c cursor, limit int) ([]SearchHit, error) {
	if loc == nil {
		loc = time.UTC
	}
	q := parseSearch(raw, time.Now(), loc)
	if utf8.RuneCountInString(q.Text) < 2 && !q.filtered() {
		return nil, httpx.BadRequest("Type at least 2 characters, or use a filter like in:#channel or from:@person.")
	}
	if channelID != nil {
		if _, err := s.loadAccess(ctx, s.d.Pool, *channelID, me); err != nil {
			return nil, err
		}
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	fromMe := q.From == "me"
	from := q.From
	if fromMe {
		from = ""
	}
	rows, err := s.d.Pool.Query(ctx, messageSelect+`
		JOIN channels c ON c.id = m.channel_id
		WHERE m.kind <> 'system' AND m.deleted_at IS NULL
		  AND `+readableSQL+`
		  AND ($2::uuid IS NULL OR c.org_id = $2)
		  AND ($3::uuid IS NULL OR c.id = $3)
		  AND ($4 = '' OR m.body ILIKE '%' || $4 || '%' ESCAPE '\' OR m.title ILIKE '%' || $4 || '%' ESCAPE '\')
		  AND ($5 = '' OR c.name = $5)
		  AND ($6 = '' OR lower(su.display_name) LIKE $6 || '%' ESCAPE '\' OR lower(su.display_name) LIKE '% ' || $6 || '%' ESCAPE '\'
		       OR lower(split_part(su.email, '@', 1)) LIKE $6 || '%' ESCAPE '\')
		  AND (NOT $7 OR m.sender_id = $1)
		  AND (NOT $8 OR EXISTS (SELECT 1 FROM channel_attachments a WHERE a.message_id = m.id))
		  AND (NOT $9 OR m.thread_root_id IS NOT NULL)
		  AND (m.sender_id IS NULL OR m.sender_id <> ALL($10))
		  AND ($11::timestamptz IS NULL OR (m.created_at, m.id) < ($11, $12))
		  AND (NOT $14 OR EXISTS (SELECT 1 FROM channel_message_links l WHERE l.message_id = m.id) OR m.body ~* 'https?://')
		  AND (NOT $15 OR m.pinned_at IS NOT NULL)
		  AND (NOT $16 OR EXISTS (SELECT 1 FROM channel_reactions r WHERE r.message_id = m.id))
		  AND (NOT $17 OR EXISTS (SELECT 1 FROM channel_saved sv WHERE sv.message_id = m.id AND sv.user_id = $1))
		  AND (NOT $18 OR (m.sender_id IS DISTINCT FROM $1 AND (
		       EXISTS (SELECT 1 FROM channel_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1)
		       OR c.kind IN ('dm', 'group_dm') OR m.mention_channel)))
		  AND ($19::timestamptz IS NULL OR m.created_at >= $19)
		  AND ($20::timestamptz IS NULL OR m.created_at < $20)
		  AND ($21 = '' OR EXISTS (
		       SELECT 1 FROM users wu JOIN org_members wom ON wom.user_id = wu.id AND wom.org_id = c.org_id
		       WHERE wu.id <> $1 AND (lower(wu.display_name) LIKE $21 || '%' ESCAPE '\' OR lower(wu.display_name) LIKE '% ' || $21 || '%' ESCAPE '\'
		              OR lower(split_part(wu.email, '@', 1)) LIKE $21 || '%' ESCAPE '\')
		         AND ((c.kind IN ('dm', 'group_dm') AND EXISTS (SELECT 1 FROM channel_members wm WHERE wm.channel_id = c.id AND wm.user_id = wu.id))
		              OR (m.thread_root_id IS NOT NULL AND EXISTS (
		                   SELECT 1 FROM channel_messages t WHERE (t.id = m.thread_root_id OR t.thread_root_id = m.thread_root_id) AND t.sender_id = wu.id)))))
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $13`, me, orgID, channelID, escapeLike(q.Text), q.InChannel, escapeLike(from), fromMe, q.HasFile, q.IsThread,
		blocked, nullTime(c.T), c.ID, limit, q.HasLink, q.HasPin, q.HasReaction, q.IsSaved, q.ToMe, q.After, q.Before, escapeLike(q.With))
	if err != nil {
		return nil, err
	}
	msgs, err := scanMessages(rows, false)
	if err != nil {
		return nil, err
	}
	if err := s.hydrate(ctx, s.d.Pool, msgs, me, false); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ChannelID
	}
	refs, err := s.channelRefs(ctx, me, ids)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(msgs))
	for _, m := range msgs {
		hits = append(hits, SearchHit{Message: m, Channel: refs[m.ChannelID]})
	}
	return hits, nil
}

// GET /channels/orgs/{orgID}/search?q=&channel_id=&tz=&cursor= — messages;
// the query may carry the filters searchQuery lists (dates read in tz).
func (s *Service) search(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var channelID *uuid.UUID
	if v := r.URL.Query().Get("channel_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid channel_id.")
		}
		channelID = &id
	}
	loc := zoneFor(ctx, s.d.Pool, me, m.OrgID, r.URL.Query().Get("tz"))
	hits, err := s.searchMessages(ctx, me, &m.OrgID, channelID, r.URL.Query().Get("q"), loc, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(hits, p.Limit, func(h SearchHit) any { return cursor{h.Message.CreatedAt, h.Message.ID} }))
	return nil
}

// ---- global search (app.SearchProvider) ----------------------------------------------------

var _ app.SearchProvider = (*Service)(nil)

// Search contributes channels (by name or topic) and messages the caller may
// read, across their orgs or in the one the search is scoped to
// (app.SearchOrg), with the same checks as the REST routes. Context is the
// org, then the channel (or the people in a DM) for messages.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 2 {
		return nil, nil
	}
	limit = max(1, min(limit, 20))
	reader, err := s.agentReader(ctx, caller)
	if err != nil {
		return nil, err
	}
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	out := []app.SearchResult{}
	var orgIDs []uuid.UUID
	like := escapeLike(strings.ToLower(strings.TrimPrefix(query, "#")))
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.org_id, c.name, c.topic, c.kind, c.last_message_at,
		       CASE WHEN c.name = $2 THEN 1.0 WHEN c.name LIKE $2 || '%' ESCAPE '\' THEN 0.9 ELSE 0.7 END
		FROM channels c
		WHERE c.kind IN ('public', 'private') AND c.archived_at IS NULL AND `+readableSQL+`
		  AND ($4::uuid IS NULL OR c.org_id = $4)
		  AND (c.name LIKE '%' || $2 || '%' ESCAPE '\' OR lower(c.topic) LIKE '%' || $2 || '%' ESCAPE '\')
		ORDER BY 7 DESC, c.name LIMIT $3`, caller.UserID, like, limit, scope)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r app.SearchResult
		var id, orgID uuid.UUID
		var name, topic, kind string
		if err := rows.Scan(&id, &orgID, &name, &topic, &kind, &r.At, &r.Score); err != nil {
			rows.Close()
			return nil, err
		}
		if ok, err := reader.can(id); err != nil || !ok {
			if err != nil {
				rows.Close()
				return nil, err
			}
			continue
		}
		r.Kind, r.ID, r.Title, r.Snippet, r.Link, r.Icon = "channels.channel", id.String(), "#"+name, topic, "/channels/"+id.String(), "hash"
		r.OrgID = orgID.String()
		orgIDs = append(orgIDs, orgID)
		if kind == kindPrivate {
			r.Icon = "lock"
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hits, err := s.searchMessages(ctx, caller.UserID, scope, nil, query, time.UTC, cursor{}, limit)
	if err != nil {
		if _, ok := err.(*httpx.Error); !ok {
			return nil, err
		}
		hits = nil
	}
	if hits, err = s.agentFilterHits(ctx, caller, hits); err != nil {
		return nil, err
	}
	for _, h := range hits {
		orgIDs = append(orgIDs, h.Channel.OrgID)
	}
	orgNames, err := s.orgNames(ctx, orgIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		id, _ := uuid.Parse(out[i].OrgID)
		out[i].Context = app.SearchContext(orgNames[id])
	}
	for i, h := range hits {
		where := "#" + h.Channel.Name
		if h.Channel.Kind == kindDM || h.Channel.Kind == kindGroupDM {
			where = h.Channel.Name
		}
		link := "/channels/" + h.Channel.ID.String() + "?message=" + h.Message.ID.String()
		if h.Message.ThreadRootID != nil {
			link = "/channels/" + h.Channel.ID.String() + "?thread=" + h.Message.ThreadRootID.String() + "&message=" + h.Message.ID.String()
		}
		author := deref(h.Message.BotName)
		if h.Message.Sender != nil {
			author = h.Message.Sender.DisplayName
		}
		title := author
		if h.Message.Title != nil {
			title = *h.Message.Title // a forum post
		}
		out = append(out, app.SearchResult{
			Kind: "channels.message", ID: h.Message.ID.String(), Title: title,
			Snippet: truncate(plainText(h.Message.Body, h.Message.Mentions), 160),
			Link:    link, Icon: "message-square", Score: 0.6 - float64(i)*0.01, At: h.Message.CreatedAt,
			OrgID: h.Channel.OrgID.String(), Context: app.SearchContext(orgNames[h.Channel.OrgID], where),
		})
	}
	return out, nil
}

// orgNames names orgs by id (for search context).
func (s *Service) orgNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, name FROM orgs WHERE id = ANY($1)`, dedupe(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// plainText turns a body into one plain line: mention tokens become names,
// markdown marks are dropped.
func plainText(body string, mentioned []UserRef) string {
	names := map[string]string{}
	for _, u := range mentioned {
		names[u.ID.String()] = "@" + u.DisplayName
	}
	body = userTokenRe.ReplaceAllStringFunc(body, func(tok string) string {
		if n, ok := names[userTokenRe.FindStringSubmatch(tok)[1]]; ok {
			return n
		}
		return "@someone"
	})
	body = specialTokenRe.ReplaceAllString(body, "@$1")
	body = groupTokenRe.ReplaceAllString(body, "@$2")
	body = strings.NewReplacer("```", "", "**", "", "~~", "", "`", "", "> ", "").Replace(body)
	body = escapeRe.ReplaceAllString(body, "$1")
	return strings.Join(strings.Fields(body), " ")
}

// channelByName finds a channel the caller may read in an org by name (agent tools).
func (s *Service) channelByName(ctx context.Context, me, orgID uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT c.id FROM channels c WHERE c.org_id = $2 AND c.name = $3 AND `+readableSQL, me, orgID,
		strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "#"))).Scan(&id)
	if db.IsNoRows(err) {
		return uuid.Nil, errChannelNotFound
	}
	return id, err
}
