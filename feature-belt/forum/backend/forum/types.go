package forum

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/moderation"
)

// Author is a person as the forum shows them.
type Author struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// CommunityRef is the community a post belongs to.
type CommunityRef struct {
	ID    uuid.UUID `json:"id"`
	Slug  string    `json:"slug"`
	Title string    `json:"title"`
}

// Community is GET /v1/forum/communities.
type Community struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Rules       []string  `json:"rules"`
	Locked      bool      `json:"locked"`
	RefKind     *string   `json:"ref_kind"` // the app object it belongs to (EnsureCommunity)
	RefID       *string   `json:"ref_id"`
	Posts7d     int       `json:"posts_7d"`
	CreatedAt   time.Time `json:"created_at"`
}

// CommunityDetail is GET /v1/forum/communities/{slug}.
type CommunityDetail struct {
	Community
	Moderators  []Author      `json:"moderators"`
	Pinned      []PostSummary `json:"pinned"`
	CanModerate bool          `json:"can_moderate"` // staff or a moderator of this community
}

// PostSummary is a post in a list.
type PostSummary struct {
	ID           uuid.UUID    `json:"id"`
	Community    CommunityRef `json:"community"`
	Author       *Author      `json:"author"` // null once deleted
	Title        string       `json:"title"`
	Kind         string       `json:"kind"` // text | link
	URL          *string      `json:"url"`
	Domain       *string      `json:"domain"`
	Excerpt      string       `json:"excerpt"`
	Score        int          `json:"score"`
	CommentCount int          `json:"comment_count"`
	ViewerVote   int          `json:"viewer_vote"` // -1, 0, 1
	Labels       []string     `json:"labels"`
	Status       string       `json:"status"` // visible | held | removed | deleted
	Pinned       bool         `json:"pinned"`
	Locked       bool         `json:"locked"`
	CreatedAt    time.Time    `json:"created_at"`
	EditedAt     *time.Time   `json:"edited_at"`
}

// Post is GET /v1/forum/posts/{id}.
type Post struct {
	PostSummary
	Body     string   `json:"body"`
	Mentions []Author `json:"mentions"`
	// ModerationReasons say why a held or removed post is not public (its author and staff only).
	ModerationReasons []string `json:"moderation_reasons"`
	CanModerate       bool     `json:"can_moderate"`
}

// Comment is one comment with its replies.
type Comment struct {
	ID                uuid.UUID  `json:"id"`
	PostID            uuid.UUID  `json:"post_id"`
	ParentID          *uuid.UUID `json:"parent_id"`
	Depth             int        `json:"depth"`
	Author            *Author    `json:"author"` // null for placeholders
	Body              string     `json:"body"`
	Score             int        `json:"score"`
	ViewerVote        int        `json:"viewer_vote"`
	Labels            []string   `json:"labels"`
	Status            string     `json:"status"`
	ModerationReasons []string   `json:"moderation_reasons"`
	Mentions          []Author   `json:"mentions"`
	CreatedAt         time.Time  `json:"created_at"`
	EditedAt          *time.Time `json:"edited_at"`
	Replies           []Comment  `json:"replies"`
	MoreReplies       int        `json:"more_replies"` // replies not included; GET /forum/comments/{id}/replies pages them
}

// PostResult is what creating or editing a post returns.
type PostResult struct {
	Post       Post               `json:"post"`
	Moderation moderation.Outcome `json:"moderation"`
}

// CommentResult is what creating or editing a comment returns.
type CommentResult struct {
	Comment    Comment            `json:"comment"`
	Moderation moderation.Outcome `json:"moderation"`
}

// Hot is the ranking: sign(s)·log10(max(|s|,1)) + (created_s − 1.7e9)/45000,
// minus 2 for posts labelled off-topic. Newer posts start higher, so stored
// hot values never need re-ranking as time passes.
func Hot(score int, created time.Time, offTopic bool) float64 {
	s := float64(score)
	var sign float64
	switch {
	case s > 0:
		sign = 1
	case s < 0:
		sign = -1
	}
	h := sign*math.Log10(math.Max(math.Abs(s), 1)) + (float64(created.Unix())-1.7e9)/45000
	if offTopic {
		h -= 2
	}
	return h
}

var (
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdMarkRe  = regexp.MustCompile("[*_`#>~|]+")
	spaceRe   = regexp.MustCompile(`\s+`)
)

// Excerpt is the first 280 characters of a Markdown body as plain text.
func Excerpt(body string) string {
	s := mdImageRe.ReplaceAllString(body, "$1")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdMarkRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 280 {
		s = string(r[:279]) + "…"
	}
	return s
}

// labelRe keeps labels to short snake_case words.
var labelRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func mergeLabels(cur, add []string) []string {
	out := append([]string{}, cur...)
	for _, l := range add {
		if !labelRe.MatchString(l) {
			continue
		}
		dup := false
		for _, x := range out {
			if x == l {
				dup = true
				break
			}
		}
		if !dup && len(out) < 8 {
			out = append(out, l)
		}
	}
	return out
}

func hasLabel(labels []string, l string) bool {
	for _, x := range labels {
		if x == l {
			return true
		}
	}
	return false
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
