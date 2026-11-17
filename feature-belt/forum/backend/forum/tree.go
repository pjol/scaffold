package forum

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// commentRow is one stored comment.
type commentRow struct {
	ID           uuid.UUID
	PostID       uuid.UUID
	ParentID     *uuid.UUID
	Depth        int
	AuthorID     uuid.UUID
	AuthorName   string
	AuthorAvatar *string
	Body         string
	Labels       []string
	ModReasons   []string
	Status       string
	Score        int
	CreatedAt    time.Time
	EditedAt     *time.Time
}

// treeOpts controls buildTree.
type treeOpts struct {
	Sort        string // top (default) | new | old
	Viewer      uuid.UUID
	Staff       bool
	Blocked     map[uuid.UUID]bool
	Votes       map[uuid.UUID]int
	Mentions    map[uuid.UUID][]Author
	MaxChildren int        // replies per comment (MaxChildren); the rest count in more_replies
	Root        *uuid.UUID // build the replies under this comment instead of the top level
}

// visibility of one comment to the viewer.
const (
	showFull = iota
	showPlaceholder
)

// commentLess orders siblings: top = score, then older first; new; old.
func commentLess(sort string) func(a, b *commentRow) int {
	return func(a, b *commentRow) int {
		switch sort {
		case "new":
			if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
				return c
			}
		case "old":
			if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
				return c
			}
		default:
			if a.Score != b.Score {
				return b.Score - a.Score
			}
			if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
				return c
			}
		}
		return slices.Compare(a.ID[:], b.ID[:])
	}
}

// buildTree builds the comments a viewer sees, as the sorted list of
// top-level comments (or of Root's direct replies), each with its replies.
// Held and removed comments show in full to their author and staff; to
// everyone else they (and deleted comments, and comments by people blocked
// either way) become placeholders without author or text when they have
// replies the viewer sees, and disappear when they don't. A held comment
// reads as removed to others, so nobody learns it is pending review.
func buildTree(rows []commentRow, o treeOpts) []Comment {
	if o.MaxChildren <= 0 {
		o.MaxChildren = MaxChildren
	}
	byID := make(map[uuid.UUID]*commentRow, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	children := map[uuid.UUID][]*commentRow{}
	var top []*commentRow
	for i := range rows {
		r := &rows[i]
		if r.ParentID == nil {
			top = append(top, r)
			continue
		}
		if _, ok := byID[*r.ParentID]; !ok {
			continue // orphan
		}
		children[*r.ParentID] = append(children[*r.ParentID], r)
	}
	less := commentLess(o.Sort)
	vis := func(r *commentRow) int {
		if o.Blocked[r.AuthorID] && r.AuthorID != o.Viewer {
			return showPlaceholder
		}
		switch r.Status {
		case StatusVisible:
			return showFull
		case StatusDeleted:
			return showPlaceholder
		default: // held, removed
			if o.Staff || r.AuthorID == o.Viewer {
				return showFull
			}
			return showPlaceholder
		}
	}
	var build func(list []*commentRow, limit int) ([]Comment, int)
	build = func(list []*commentRow, limit int) ([]Comment, int) {
		slices.SortFunc(list, less)
		out := []Comment{}
		more := 0
		for _, r := range list {
			replies, moreReplies := build(children[r.ID], o.MaxChildren)
			v := vis(r)
			if v == showPlaceholder && len(replies) == 0 {
				continue
			}
			if len(out) >= limit {
				more++
				continue
			}
			c := Comment{ID: r.ID, PostID: r.PostID, ParentID: r.ParentID, Depth: r.Depth, Score: r.Score,
				ViewerVote: o.Votes[r.ID], Labels: nonNilStrings(r.Labels), Status: r.Status, ModerationReasons: []string{},
				Mentions: []Author{}, CreatedAt: r.CreatedAt, EditedAt: r.EditedAt, Replies: replies, MoreReplies: moreReplies}
			if v == showFull {
				c.Body = r.Body
				c.Author = &Author{ID: r.AuthorID, DisplayName: r.AuthorName, AvatarURL: r.AuthorAvatar}
				if m := o.Mentions[r.ID]; m != nil {
					c.Mentions = m
				}
				if r.Status == StatusHeld || r.Status == StatusRemoved {
					c.ModerationReasons = nonNilStrings(r.ModReasons)
				}
			} else {
				c.Labels, c.EditedAt = []string{}, nil
				if c.Status == StatusHeld {
					c.Status = StatusRemoved
				}
			}
			out = append(out, c)
		}
		return out, more
	}
	if o.Root != nil {
		out, _ := build(children[*o.Root], len(rows)+1)
		return out
	}
	out, _ := build(top, len(rows)+1)
	return out
}

// renderOne renders a single comment without its thread. It is built as a
// root: a reply's parent isn't in the list, and buildTree drops orphans.
func renderOne(c commentRow, o treeOpts) (Comment, bool) {
	row := c
	row.ParentID = nil
	out := buildTree([]commentRow{row}, o)
	if len(out) == 0 {
		return Comment{}, false
	}
	out[0].ParentID = c.ParentID
	return out[0], true
}

// depthFor is where a reply attaches: under its parent, or beside it once
// the parent is at MaxDepth.
func depthFor(parent *commentRow) (*uuid.UUID, int) {
	if parent == nil {
		return nil, 0
	}
	if parent.Depth >= MaxDepth {
		return parent.ParentID, parent.Depth
	}
	id := parent.ID
	return &id, parent.Depth + 1
}

// commentCursor is the keyset position after the last comment of a page.
type commentCursor struct {
	S  int       `json:"s"`
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// pageComments returns the comments after cursor in sort order, at most
// limit, and the cursor of the next page.
func pageComments(list []Comment, sort string, after *commentCursor, limit int) ([]Comment, *commentCursor) {
	less := commentLess(sort)
	start := 0
	if after != nil {
		pos := &commentRow{ID: after.ID, Score: after.S, CreatedAt: after.T}
		for start < len(list) {
			c := list[start]
			if less(pos, &commentRow{ID: c.ID, Score: c.Score, CreatedAt: c.CreatedAt}) < 0 {
				break
			}
			start++
		}
	}
	end := min(len(list), start+limit)
	page := list[start:end]
	if end < len(list) && len(page) > 0 {
		last := page[len(page)-1]
		return page, &commentCursor{S: last.Score, T: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}
