package repos

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Agent tools for issues and reviews. Same checks as the REST routes
// (toolRepo → access.Require, capped by the AI agent scope); writes target
// the repository and preview the exact change, so with scope "suggest"
// they become suggestions. Agents read, open and comment on issues, set
// labels, and leave comment reviews (with ```suggestion blocks); they never
// close, lock, delete, merge, post statuses or touch webhooks and settings.

const issueQueryHelp = `Search syntax: is:open | is:closed, label:bug (repeat for all of them), -label:x, no:label, assignee:@me | <email or name>, no:assignee, author:@me, mentions:@me, milestone:"v1.0", no:milestone, reason:completed | "not planned", sort:created | created-asc | updated | comments, plus free words matched against titles and descriptions.`

type toolIssueIn struct {
	RepoID uuid.UUID `json:"repo_id"`
	Number int       `json:"number"`
}

func (s *Service) issueTools() []app.Tool {
	return []app.Tool{
		{
			Name:        "repos_list_issues",
			Description: "List or search issues of one of this app's repositories, most recent first, with labels, assignees and milestone. " + issueQueryHelp,
			InputSchema: schema(repoIDProp+`,"query":{"type":"string","description":"Search syntax; default is:open."},"limit":{"type":"integer","minimum":1,"maximum":50}`, `"repo_id"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Query  string    `json:"query"`
					Limit  int       `json:"limit"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 25
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				q := strings.TrimSpace(in.Query)
				if q == "" {
					q = "is:open"
				}
				list, parsed, counts, err := s.searchIssues(ctx, repo, caller.UserID, q, "", in.Limit)
				if err != nil {
					return nil, err
				}
				type row struct {
					Number    int      `json:"number"`
					Title     string   `json:"title"`
					State     string   `json:"state"`
					Reason    *string  `json:"state_reason,omitempty"`
					Labels    []string `json:"labels"`
					Assignees []string `json:"assignees"`
					Milestone string   `json:"milestone,omitempty"`
					Comments  int      `json:"comments"`
					Author    string   `json:"author,omitempty"`
					Updated   string   `json:"updated_at"`
					Link      string   `json:"link"`
				}
				out := []row{}
				for _, is := range list {
					r := row{Number: is.Number, Title: is.Title, State: is.State, Reason: is.StateReason, Comments: is.CommentCount,
						Labels: []string{}, Assignees: []string{}, Updated: is.UpdatedAt.Format("2006-01-02T15:04Z"), Link: s.issueLink(repo, is.Number)}
					for _, l := range is.Labels {
						r.Labels = append(r.Labels, l.Name)
					}
					for _, a := range is.Assignees {
						r.Assignees = append(r.Assignees, a.DisplayName)
					}
					if is.Milestone != nil {
						r.Milestone = is.Milestone.Title
					}
					if is.Author != nil {
						r.Author = is.Author.DisplayName
					}
					out = append(out, r)
				}
				return map[string]any{"issues": out, "open": counts.Open, "closed": counts.Closed, "ignored_qualifiers": parsed.Unknown}, nil
			},
		},
		{
			Name:        "repos_read_issue",
			Description: "Read one issue of this app's repositories: description, labels, assignees, milestone, linked pull requests and the conversation (comments and events). Mentions read as @Name.",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1}`, `"repo_id","number"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in toolIssueIn
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, lvl, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return nil, err
				}
				if is, err = s.issueDetail(ctx, caller, is, lvl); err != nil {
					return nil, err
				}
				items, _, err := s.issueTimelineItems(ctx, caller, is, lvl)
				if err != nil {
					return nil, err
				}
				type entry struct {
					Type   string         `json:"type"`
					At     string         `json:"at"`
					Author string         `json:"author,omitempty"`
					Body   string         `json:"body,omitempty"`
					Kind   string         `json:"event,omitempty"`
					Data   map[string]any `json:"data,omitempty"`
				}
				conv := []entry{}
				for _, it := range items {
					e := entry{Type: it.Type, At: it.At.Format("2006-01-02T15:04Z")}
					if it.Comment != nil {
						if it.Comment.Deleted {
							continue
						}
						if it.Comment.Author != nil {
							e.Author = it.Comment.Author.DisplayName
						}
						e.Body = truncateUTF8(s.plainBody(ctx, it.Comment.Body), 4000)
					} else if it.Event != nil {
						e.Kind, e.Data = it.Event.Kind, it.Event.Data
						if it.Event.Actor != nil {
							e.Author = it.Event.Actor.DisplayName
						}
					}
					conv = append(conv, e)
					if len(conv) >= 200 {
						break
					}
				}
				labels := []string{}
				for _, l := range is.Labels {
					labels = append(labels, l.Name)
				}
				assignees := []string{}
				for _, a := range is.Assignees {
					assignees = append(assignees, a.DisplayName)
				}
				out := map[string]any{"number": is.Number, "title": is.Title, "body": s.plainBody(ctx, is.Body), "state": is.State, "state_reason": is.StateReason,
					"labels": labels, "assignees": assignees, "locked": is.Locked, "linked_pull_requests": is.Pulls, "conversation": conv, "link": s.issueLink(repo, is.Number)}
				if is.Milestone != nil {
					out["milestone"] = is.Milestone.Title
				}
				if is.Author != nil {
					out["author"] = is.Author.DisplayName
				}
				return out, nil
			},
		},
		{
			Name:        "repos_create_issue",
			Description: "Open an issue in one of this app's repositories (comment access). labels are label names and need edit access; repos_list_issues shows labels in use.",
			InputSchema: schema(repoIDProp+`,"title":{"type":"string"},"body":{"type":"string","description":"Markdown."},"labels":{"type":"array","items":{"type":"string"}}`, `"repo_id","title"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in toolCreateIssueIn
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				title, err := cleanIssueTitle(in.Title)
				if err != nil {
					return app.ToolPreview{}, err
				}
				after := "# " + title + "\n\n" + strings.TrimSpace(in.Body)
				if len(in.Labels) > 0 {
					after += "\n\nLabels: " + strings.Join(in.Labels, ", ")
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Open issue “%s” in %s", title, repo.Name), After: after, Link: "/repos/" + repo.ID.String() + "/issues"}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in toolCreateIssueIn
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, lvl, err := s.toolRepo(ctx, caller, in.RepoID, levelComment)
				if err != nil {
					return nil, err
				}
				ids, err := s.toolLabelIDs(ctx, repo, in.Labels)
				if err != nil {
					return nil, err
				}
				is, err := s.createIssue(ctx, caller, repo, lvl, IssueInput{Title: in.Title, Body: in.Body, Labels: ids})
				if err != nil {
					return nil, err
				}
				return map[string]any{"number": is.Number, "link": s.issueLink(repo, is.Number)}, nil
			},
		},
		{
			Name:        "repos_comment_on_issue",
			Description: "Comment on an issue of this app's repositories (comment access; locked conversations need edit access). Markdown; #12 links issues and pull requests.",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1},"body":{"type":"string"}`, `"repo_id","number","body"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					toolIssueIn
					Body string `json:"body"`
				}
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Comment on #%d “%s” in %s", is.Number, is.Title, repo.Name), After: in.Body, Link: s.issueLink(repo, is.Number)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					toolIssueIn
					Body string `json:"body"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, lvl, err := s.toolRepo(ctx, caller, in.RepoID, levelComment)
				if err != nil {
					return nil, err
				}
				is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return nil, err
				}
				c, err := s.addIssueComment(ctx, caller, repo, is, lvl, in.Body)
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment_id": c.ID, "link": s.issueLink(repo, is.Number)}, nil
			},
		},
		{
			Name:        "repos_label_issue",
			Description: "Add or remove labels (by name) on an issue or pull request of this app's repositories (edit access). Unknown names are refused; repos_list_issues shows labels in use.",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1},"add":{"type":"array","items":{"type":"string"}},"remove":{"type":"array","items":{"type":"string"}}`, `"repo_id","number"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in toolLabelIn
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				_, title, before, after, _, err := s.labelPlan(ctx, repo, in)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Change labels on #%d “%s” in %s", in.Number, title, repo.Name),
					Before: strings.Join(before, "\n"), After: strings.Join(after, "\n"), Link: s.issueLink(repo, in.Number)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in toolLabelIn
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelEdit)
				if err != nil {
					return nil, err
				}
				kind, _, _, after, ids, err := s.labelPlan(ctx, repo, in)
				if err != nil {
					return nil, err
				}
				kindName, id, err := itemByNumber(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil || id == uuid.Nil {
					return nil, httpx.NotFound(fmt.Sprintf("There's no issue or pull request #%d.", in.Number))
				}
				_ = kindName
				if err := s.applyLabels(ctx, caller, repo, kind, id, in.Number, ids); err != nil {
					return nil, err
				}
				return map[string]any{"labels": after}, nil
			},
		},
		{
			Name: "repos_review_pull_request",
			Description: "Leave a comment review on a pull request of this app's repositories (comment access): an overall body and line comments on changed files (path and line on the new side; start_line for a range; side \"old\" for removed lines). " +
				"To propose an exact change, put a ```suggestion block in a line comment with the replacement for start_line..line; people apply it with one click. Reviews never approve or request changes, and agents never merge.",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1},"body":{"type":"string"},"comments":{"type":"array","maxItems":50,"items":{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer","minimum":1},"start_line":{"type":"integer","minimum":1},"side":{"type":"string","enum":["new","old"]},"body":{"type":"string"}},"required":["path","line","body"],"additionalProperties":false}}`, `"repo_id","number"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in toolReviewIn
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return app.ToolPreview{}, err
				}
				var b strings.Builder
				b.WriteString(strings.TrimSpace(in.Body))
				for _, c := range in.Comments {
					lines := fmt.Sprintf("line %d", c.Line)
					if c.StartLine > 0 && c.StartLine < c.Line {
						lines = fmt.Sprintf("lines %d–%d", c.StartLine, c.Line)
					}
					fmt.Fprintf(&b, "\n\n— %s %s:\n%s", c.Path, lines, strings.TrimSpace(c.Body))
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Review #%d “%s” in %s with %d line %s", p.Number, p.Title, repo.Name, len(in.Comments), plural(len(in.Comments), "comment", "comments")),
					After: strings.TrimSpace(b.String()), Link: s.pullLink(repo, p.Number)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in toolReviewIn
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelComment)
				if err != nil {
					return nil, err
				}
				p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(in.Body) == "" && len(in.Comments) == 0 {
					return nil, httpx.BadRequest("Write a review body or line comments.")
				}
				if p.State != "open" {
					return nil, httpx.Conflict("This pull request is " + p.State + ".")
				}
				id, err := s.submitCommentReview(ctx, caller, repo, p, in)
				if err != nil {
					return nil, err
				}
				return map[string]any{"review_id": id, "link": s.pullLink(repo, p.Number)}, nil
			},
		},
	}
}

type toolCreateIssueIn struct {
	RepoID uuid.UUID `json:"repo_id"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Labels []string  `json:"labels"`
}

type toolLabelIn struct {
	RepoID uuid.UUID `json:"repo_id"`
	Number int       `json:"number"`
	Add    []string  `json:"add"`
	Remove []string  `json:"remove"`
}

type toolReviewIn struct {
	RepoID   uuid.UUID `json:"repo_id"`
	Number   int       `json:"number"`
	Body     string    `json:"body"`
	Comments []struct {
		Path      string `json:"path"`
		Line      int    `json:"line"`
		StartLine int    `json:"start_line"`
		Side      string `json:"side"`
		Body      string `json:"body"`
	} `json:"comments"`
}

// toolLabelIDs maps label names to ids, refusing unknown ones.
func (s *Service) toolLabelIDs(ctx context.Context, repo *Repo, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ids, err := s.labelIDsByName(ctx, s.d.Pool, repo.ID, names)
	if err != nil {
		return nil, err
	}
	if len(ids) != len(uniqueLower(names)) {
		return nil, httpx.BadRequest("Some labels don't exist in this repository. Use the names repos_list_issues shows.")
	}
	return ids, nil
}

func uniqueLower(list []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range list {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out[v] = true
		}
	}
	return out
}

// labelPlan works out an issue's or pull request's labels before and after.
func (s *Service) labelPlan(ctx context.Context, repo *Repo, in toolLabelIn) (kind, title string, before, after []string, ids []uuid.UUID, err error) {
	k, id, err := itemByNumber(ctx, s.d.Pool, repo.ID, in.Number)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	if id == uuid.Nil {
		return "", "", nil, nil, nil, httpx.NotFound(fmt.Sprintf("There's no issue or pull request #%d.", in.Number))
	}
	var current []Label
	if k == "pull" {
		p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
		if err != nil {
			return "", "", nil, nil, nil, err
		}
		title, current = p.Title, p.Labels
	} else {
		is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, in.Number)
		if err != nil {
			return "", "", nil, nil, nil, err
		}
		title, current = is.Title, is.Labels
	}
	if _, err := s.toolLabelIDs(ctx, repo, append(append([]string{}, in.Add...), in.Remove...)); err != nil {
		return "", "", nil, nil, nil, err
	}
	remove := uniqueLower(in.Remove)
	keep := map[string]bool{}
	for _, l := range current {
		before = append(before, l.Name)
		if !remove[strings.ToLower(l.Name)] {
			keep[strings.ToLower(l.Name)] = true
		}
	}
	for n := range uniqueLower(in.Add) {
		keep[n] = true
	}
	names := make([]string, 0, len(keep))
	for n := range keep {
		names = append(names, n)
	}
	ids, err = s.labelIDsByName(ctx, s.d.Pool, repo.ID, names)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	after = names
	sort.Strings(after)
	sort.Strings(before)
	return k, title, before, after, ids, nil
}

// submitCommentReview posts a "comment" review with line comments as the
// caller (agent tools; same checks as submitReview for that state).
func (s *Service) submitCommentReview(ctx context.Context, caller *auth.Identity, repo *Repo, p *Pull, in toolReviewIn) (uuid.UUID, error) {
	if len(in.Comments) > maxCommentsPerReview {
		return uuid.Nil, httpx.BadRequest(fmt.Sprintf("A review holds up to %d line comments.", maxCommentsPerReview))
	}
	lvl, err := access.Require(ctx, s.d.Pool, caller.UserID, RepoKind, repo.ID, access.Comment)
	if err != nil {
		return uuid.Nil, err
	}
	_ = lvl
	review := ReviewInput{State: "comment", Body: in.Body}
	for _, c := range in.Comments {
		review.Comments = append(review.Comments, CommentInput{Body: c.Body, Path: c.Path, Line: c.Line, StartLine: c.StartLine, Side: c.Side})
	}
	return s.postReview(ctx, caller, repo, p, review)
}
