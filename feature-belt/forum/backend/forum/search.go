package forum

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// searchQuery turns typed words into a prefix tsquery ("bread sour" →
// "bread:* & sour:*"), at most 8 terms; "" when nothing is searchable.
func searchQuery(q string) string {
	terms := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(terms) > 8 {
		terms = terms[:8]
	}
	for i, t := range terms {
		terms[i] = t + ":*"
	}
	return strings.Join(terms, " & ")
}

// Search implements app.SearchProvider: visible posts by title and body,
// without people blocked either way, newest activity first among matches.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	if searchQuery(query) == "" {
		return nil, nil
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return nil, err
	}
	page, err := s.listPosts(ctx, v, listQuery{sort: "new", search: query, limit: limit})
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]app.SearchResult, 0, len(page.Items))
	for _, p := range page.Items {
		score := 0.55
		if strings.Contains(strings.ToLower(p.Title), q) {
			score = 0.7
		}
		r := app.SearchResult{Kind: TargetPost, ID: p.ID.String(), Title: p.Title, Snippet: p.Excerpt, Link: postPath(p.ID),
			Icon: "messages-square", Score: score, At: p.CreatedAt, Context: app.SearchContext("Forum", p.Community.Title)}
		if p.Author != nil {
			r.AuthorID = p.Author.ID.String()
		}
		out = append(out, r)
	}
	return out, nil
}

// Tools are the forum's agent tools, all read-only. Agents don't post or
// comment in the person's name: a forum post is public speech others read,
// vote on and reply to, so the person writes it themselves.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "forum_list_communities",
			Description: "List the forum's communities with their description, rules and posts in the last 7 days.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, _ json.RawMessage) (any, error) {
				return s.listCommunities(ctx)
			},
		},
		{
			Name:        "forum_list_posts",
			Description: "List forum posts, in one community (its slug) or across all: sort hot (default), new or top (with t = day, week, month, year or all), optionally matching search words.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"community":{"type":"string","description":"community slug"},
				"sort":{"type":"string","enum":["hot","new","top"]},
				"t":{"type":"string","enum":["day","week","month","year","all"]},
				"q":{"type":"string","description":"search words"},
				"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Community, Sort, T, Q string
					Limit                 int
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return nil, httpx.BadRequest("Invalid input.")
				}
				v, err := s.viewerOf(ctx, caller)
				if err != nil {
					return nil, err
				}
				q := listQuery{sort: in.Sort, t: in.T, search: in.Q, limit: in.Limit}
				if in.Community != "" {
					c, err := s.communityBySlug(ctx, s.d.Pool, in.Community)
					if err != nil {
						return nil, err
					}
					q.community = &c.ID
				}
				return s.listPosts(ctx, v, q)
			},
		},
		{
			Name:        "forum_get_post",
			Description: "Read one forum post with its comment tree (top comments first, up to 50 top-level comments).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"post_id":{"type":"string","format":"uuid"},
				"sort":{"type":"string","enum":["top","new","old"]}},"required":["post_id"],"additionalProperties":false}`),
			ReadOnly: true,
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					PostID string `json:"post_id"`
				}
				_ = json.Unmarshal(input, &in)
				return TargetPost, in.PostID
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PostID uuid.UUID `json:"post_id"`
					Sort   string    `json:"sort"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return nil, httpx.BadRequest("post_id must be a post id.")
				}
				sort, err := validSort(in.Sort)
				if err != nil {
					return nil, err
				}
				v, err := s.viewerOf(ctx, caller)
				if err != nil {
					return nil, err
				}
				p, err := s.loadPost(ctx, s.d.Pool, in.PostID, false)
				if err != nil {
					return nil, err
				}
				if !v.canSeePost(p) {
					return nil, httpx.NotFound("That post doesn't exist.")
				}
				post, err := s.postView(ctx, v, p)
				if err != nil {
					return nil, err
				}
				all, err := s.tree(ctx, v, p.ID, sort, nil)
				if err != nil {
					return nil, err
				}
				comments, _ := pageComments(all, sort, nil, 50)
				return map[string]any{"post": post, "comments": comments}, nil
			},
		},
	}
}
