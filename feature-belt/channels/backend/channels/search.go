package channels

import (
	"context"
	"net/http"
	"regexp"
	"strings"
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
//	in:#general      only that channel (by name)
//	from:@sam        messages by people whose name or email starts with "sam" (from:me = yours)
//	has:file         messages with attachments
//	is:thread        thread replies only
type searchQuery struct {
	Text      string
	InChannel string
	From      string
	HasFile   bool
	IsThread  bool
}

var filterRe = regexp.MustCompile(`(?i)\b(in|from|has|is):(\S+)`)

func parseSearch(raw string) searchQuery {
	var q searchQuery
	text := filterRe.ReplaceAllStringFunc(raw, func(tok string) string {
		m := filterRe.FindStringSubmatch(tok)
		key, val := strings.ToLower(m[1]), m[2]
		switch {
		case key == "in" && strings.HasPrefix(val, "#"):
			q.InChannel = strings.ToLower(strings.TrimPrefix(val, "#"))
		case key == "in":
			q.InChannel = strings.ToLower(val)
		case key == "from":
			q.From = strings.ToLower(strings.TrimPrefix(val, "@"))
		case key == "has" && strings.EqualFold(val, "file"):
			q.HasFile = true
		case key == "is" && strings.EqualFold(val, "thread"):
			q.IsThread = true
		default:
			return tok
		}
		return " "
	})
	q.Text = strings.Join(strings.Fields(text), " ")
	return q
}

// searchMessages finds messages caller may read in orgID (every org they're
// in when orgID is nil), newest first. channelID narrows to one channel.
func (s *Service) searchMessages(ctx context.Context, me uuid.UUID, orgID, channelID *uuid.UUID, raw string, c cursor, limit int) ([]SearchHit, error) {
	q := parseSearch(raw)
	if utf8.RuneCountInString(q.Text) < 2 && q.InChannel == "" && q.From == "" && !q.HasFile {
		return nil, httpx.BadRequest("Type at least 2 characters, or use in:#channel or from:@person.")
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
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $13`, me, orgID, channelID, escapeLike(q.Text), q.InChannel, escapeLike(from), fromMe, q.HasFile, q.IsThread,
		blocked, nullTime(c.T), c.ID, limit)
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

// GET /channels/orgs/{orgID}/search?q=&channel_id=&cursor= — messages; the
// query may carry in:#channel, from:@person, from:me, has:file, is:thread.
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
	hits, err := s.searchMessages(ctx, me, &m.OrgID, channelID, r.URL.Query().Get("q"), c, p.Limit+1)
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
	hits, err := s.searchMessages(ctx, caller.UserID, scope, nil, query, cursor{}, limit)
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
