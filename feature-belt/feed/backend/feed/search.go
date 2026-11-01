package feed

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Search implements app.SearchProvider: posts the caller's feed shows, by
// exactly the rules of GET /feed/posts?q= (audiences from the Scope, removed
// posts and blocked authors excluded, full-text match). For an agent call
// each post must also be readable at the caller's agent scope. Context is
// "Feed", after the audience's name and with its org when the Scope is an
// AudienceNamer.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	if searchQuery(query) == "" {
		return nil, nil // nothing searchable: an empty query would list the whole feed
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	page, err := s.listPosts(ctx, v, listFilter{limit: limit, q: query})
	if err != nil {
		return nil, err
	}
	if page, err = s.agentPosts(ctx, caller, page); err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	namer, _ := s.scope.(AudienceNamer)
	type named struct {
		name  string
		orgID string
	}
	audiences := map[string]named{}
	out := make([]app.SearchResult, 0, len(page.Items))
	for _, p := range page.Items {
		a, seen := audiences[p.Audience]
		if !seen && namer != nil {
			name, orgID, err := namer.AudienceName(ctx, p.Audience)
			if err != nil {
				return nil, err
			}
			a = named{name: name}
			if orgID != uuid.Nil {
				a.orgID = orgID.String()
			}
			audiences[p.Audience] = a
		}
		body := strings.Join(strings.Fields(p.Body), " ")
		title := body
		if r := []rune(title); len(r) > 90 {
			title = string(r[:89]) + "…"
		}
		if title == "" {
			title = "Post by " + p.Author.DisplayName
		}
		score := 0.55
		if strings.Contains(strings.ToLower(body), q) {
			score = 0.65 // the words appear together, not only as separate terms
		}
		if p.Kind == "announcement" {
			score += 0.05
		}
		out = append(out, app.SearchResult{
			Kind:    "feed.post",
			ID:      p.ID.String(),
			Title:   title,
			Snippet: p.Author.DisplayName,
			Link:    "/feed/posts/" + p.ID.String(),
			Icon:    "newspaper",
			Score:   score,
			At:      p.CreatedAt,
			OrgID:   a.orgID,
			Context: app.SearchContext(a.name, "Feed"),
		})
	}
	return out, nil
}
