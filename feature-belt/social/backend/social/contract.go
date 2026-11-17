package social

// The cross-feature contract. Features that require social import this
// package and use only what is in this file: Refs/UserIDByHandle to show
// people by handle, RegisterSection/CanSee for privacy-gated parts of a
// profile, and RegisterKind/Record/RemoveActivity for the activity feed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Ref is how a person appears anywhere social shows them.
type Ref struct {
	UserID      uuid.UUID `json:"user_id"`
	Handle      string    `json:"handle"` // "" for a deleted account
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Refs returns the Ref of each id that has an account. People without a
// profile yet get one (a generated handle) on the way. Deleted accounts come
// back as "Deleted user" with no handle.
func Refs(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]Ref, error) {
	out := make(map[uuid.UUID]Ref, len(ids))
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	if err := ensureProfiles(ctx, q, ids); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT u.id, CASE WHEN u.status = 'deleted' THEN '' ELSE COALESCE(p.handle, '') END,
		       CASE WHEN u.status = 'deleted' THEN 'Deleted user' ELSE u.display_name END,
		       CASE WHEN u.status = 'deleted' THEN NULL ELSE u.avatar_url END
		FROM users u LEFT JOIN social_profiles p ON p.user_id = u.id
		WHERE u.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Ref
		if err := rows.Scan(&r.UserID, &r.Handle, &r.DisplayName, &r.AvatarURL); err != nil {
			return nil, err
		}
		out[r.UserID] = r
	}
	return out, rows.Err()
}

// UserIDByHandle resolves a handle ("sam", "@Sam") to its owner: the current
// holder, else the person who gave it up within the redirect window. It
// returns an *httpx.Error 404 when nobody holds it.
func UserIDByHandle(ctx context.Context, q db.Querier, handle string) (uuid.UUID, error) {
	id, _, err := resolveHandle(ctx, q, handle)
	return id, err
}

// ---- privacy sections ------------------------------------------------------

// Visibility of a profile section.
const (
	VisibilityPublic    = "public"    // everyone signed in (anonymous readers too, where an app serves them)
	VisibilityFollowers = "followers" // people who follow the owner
	VisibilityPrivate   = "private"   // only the owner
)

// Built-in sections.
const (
	// SectionActivity gates activity recorded without a section of its own.
	SectionActivity = "activity"
	// SectionConnections gates the followers and following lists (counts
	// always show).
	SectionConnections = "connections"
)

// Section is a privacy-controlled part of a profile.
type Section struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Default string `json:"default"`
}

var (
	regMu        sync.RWMutex
	sections     = map[string]Section{}
	sectionOrder []string
	kinds        = map[string]Kind{}
)

// RegisterSection adds a privacy section people set in Settings (call from
// the owning module's New). key is lowercase a–z, 0–9 and '_'; def is the
// visibility until the person chooses. A later call with the same key
// replaces the label and default.
func RegisterSection(key, label, def string) {
	key = strings.TrimSpace(key)
	if !validKey(key) || !validVisibility(def) || strings.TrimSpace(label) == "" {
		panic(fmt.Sprintf("social: RegisterSection(%q, %q, %q): key a-z0-9_, a label and a visibility", key, label, def))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := sections[key]; !ok {
		sectionOrder = append(sectionOrder, key)
	}
	sections[key] = Section{Key: key, Label: label, Default: def}
}

func init() {
	RegisterSection(SectionActivity, "Activity", VisibilityPublic)
	RegisterSection(SectionConnections, "Followers and following", VisibilityPublic)
	RegisterKind(Kind{ID: KindFollowed, Label: "Follows", Section: SectionConnections, Icon: "user-plus", Render: renderFollowed})
}

// Sections lists the registered sections in registration order.
func Sections() []Section {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Section, 0, len(sectionOrder))
	for _, k := range sectionOrder {
		out = append(out, sections[k])
	}
	return out
}

func section(key string) (Section, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	s, ok := sections[key]
	return s, ok
}

// CanSee reports whether viewer may see section of owner's profile: the
// owner always; nobody across a block (either direction); then the owner's
// setting (public: anyone, uuid.Nil included; followers: people who follow
// the owner; private: nobody else). Unknown sections and accounts that are
// banned or deleted are never visible to others.
func CanSee(ctx context.Context, q db.Querier, owner, viewer uuid.UUID, sectionKey string) (bool, error) {
	if owner == uuid.Nil {
		return false, nil
	}
	if owner == viewer {
		return true, nil
	}
	if _, ok := section(sectionKey); !ok {
		return false, nil
	}
	o, err := loadOwner(ctx, q, owner)
	if err != nil || !o.visible() {
		return false, err
	}
	vis := o.privacy.of(sectionKey)
	if vis == VisibilityPrivate {
		return false, nil
	}
	v, err := viewerOf(ctx, q, owner, viewer)
	if err != nil {
		return false, err
	}
	return Visible(vis, v), nil
}

// ---- activity --------------------------------------------------------------

// Activity is one thing a person did, for their followers' feeds and their
// profile's Activity tab.
type Activity struct {
	ActorID uuid.UUID
	Kind    string // a registered kind ("feed.post")
	// Section gates who sees it; "" uses the kind's section, else
	// SectionActivity.
	Section string
	// RefID is the source object's id ("<post id>"). Recording the same
	// (actor, kind, RefID) again updates the item; RemoveActivity deletes it.
	RefID string
	// SubjectID is another person the item is about (who was followed,
	// whose work was reviewed). Feeds show their current Ref and hide the
	// item from people a block separates from them.
	SubjectID uuid.UUID
	// Data is what Render reads. Shown to everyone who may see the item:
	// never put private fields in it.
	Data map[string]any
	// VisibleAfter holds the item out of feeds until then (zero: now). The
	// owner sees it on their own profile at once.
	VisibleAfter time.Time
}

// Rendered is an activity item as people read it.
type Rendered struct {
	Title   string // "Posted in Design"
	Summary string // a line of the content, plain text
	Link    string // app path ("/feed/posts/<id>"), same on web and mobile
}

// Kind declares an activity kind and how to show it.
type Kind struct {
	ID      string // module-prefixed: "feed.post"
	Label   string // "Posts"
	Section string // privacy section; "" = SectionActivity
	Icon    string // lucide icon name clients may show ("newspaper")
	// Render turns stored data (and the subject's current Ref, when the item
	// has one) into text. Nil: data "title", "summary" and "link". Return
	// ok=false to leave the item out (the data no longer makes sense).
	Render func(data map[string]any, subject *Ref) (r Rendered, ok bool)
}

// RegisterKind adds or replaces an activity kind (call from New).
func RegisterKind(k Kind) {
	k.ID = strings.TrimSpace(k.ID)
	if k.ID == "" || !strings.Contains(k.ID, ".") {
		panic("social: RegisterKind needs a module-prefixed ID (\"feed.post\")")
	}
	if k.Label == "" {
		k.Label = k.ID
	}
	regMu.Lock()
	defer regMu.Unlock()
	kinds[k.ID] = k
}

func kind(id string) (Kind, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	k, ok := kinds[id]
	return k, ok
}

// ErrUnknownKind is returned by Record for a kind nobody registered.
var ErrUnknownKind = errors.New("social: unknown activity kind")

// Record stores an activity item (in the caller's transaction when q is
// one). Cheap: one insert. Privacy is applied when feeds are read, so a
// later change of the owner's settings applies to old items too.
func Record(ctx context.Context, q db.Querier, a Activity) error {
	k, ok := kind(a.Kind)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownKind, a.Kind)
	}
	if a.ActorID == uuid.Nil {
		return errors.New("social: Record without ActorID")
	}
	sec := a.Section
	if sec == "" {
		sec = k.Section
	}
	if sec == "" {
		sec = SectionActivity
	}
	if _, ok := section(sec); !ok {
		return fmt.Errorf("social: unknown section %q (RegisterSection first)", sec)
	}
	data := a.Data
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var subject *uuid.UUID
	if a.SubjectID != uuid.Nil {
		subject = &a.SubjectID
	}
	var after *time.Time
	if !a.VisibleAfter.IsZero() {
		after = &a.VisibleAfter
	}
	_, err = q.Exec(ctx, `
		INSERT INTO social_activity (actor_id, kind, section, ref_id, subject_id, data, visible_after)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, now()))
		ON CONFLICT (actor_id, kind, ref_id) WHERE ref_id <> '' DO UPDATE
		SET section = EXCLUDED.section, subject_id = EXCLUDED.subject_id, data = EXCLUDED.data,
		    visible_after = EXCLUDED.visible_after`,
		a.ActorID, a.Kind, sec, strings.TrimSpace(a.RefID), subject, raw, after)
	return err
}

// RemoveActivity deletes every item of kind recorded for refID (the source
// was deleted or made private).
func RemoveActivity(ctx context.Context, q db.Querier, kindID, refID string) error {
	if strings.TrimSpace(refID) == "" {
		return nil
	}
	_, err := q.Exec(ctx, `DELETE FROM social_activity WHERE kind = $1 AND ref_id = $2`, kindID, refID)
	return err
}

// ---- errors ----------------------------------------------------------------

// Errors other features may see from the exported calls and branch on by code.
var (
	ErrNotFound    = httpx.NotFound("That profile isn't available.")
	ErrSelfFollow  = httpx.NewError(http.StatusUnprocessableEntity, "self_follow", "You can't follow yourself.")
	ErrBlocked     = httpx.NewError(http.StatusForbidden, "blocked", "You can't follow this person.")
	errFollowLimit = func(n int) *httpx.Error {
		return httpx.NewError(http.StatusUnprocessableEntity, "follow_limit", fmt.Sprintf("You follow the most people allowed (%s). Unfollow someone first.", grouped(n)))
	}
)

// ---- helpers ---------------------------------------------------------------

func validKey(k string) bool {
	if k == "" || len(k) > 40 {
		return false
	}
	return strings.Trim(k, "abcdefghijklmnopqrstuvwxyz0123456789_") == ""
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// grouped formats 2000 as "2,000".
func grouped(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + grouped(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// blockedSet returns everyone a block separates from viewer, as a set and
// as the slice moderation returns (for `<> ALL($n)` filters).
func blockedSet(ctx context.Context, q db.Querier, viewer uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	if viewer == uuid.Nil {
		return map[uuid.UUID]bool{}, []uuid.UUID{}, nil
	}
	ids, err := moderation.BlockedIDs(ctx, q, viewer)
	if err != nil {
		return nil, nil, err
	}
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, ids, nil
}

// isNoRows reports pgx.ErrNoRows.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
