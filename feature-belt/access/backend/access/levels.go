package access

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sync"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
)

// Level is what someone may do with a resource. Each level includes the ones
// below it. SQL uses the integers (access_level() >= 1 means "can view"); JSON
// uses the names.
type Level int

const (
	None    Level = 0
	View    Level = 1
	Comment Level = 2
	Edit    Level = 3
	Manage  Level = 4 // change sharing; the owner and admin-managed resources also get it
)

var levelNames = map[Level]string{None: "none", View: "view", Comment: "comment", Edit: "edit", Manage: "manage"}

func (l Level) String() string {
	if n, ok := levelNames[l]; ok {
		return n
	}
	return fmt.Sprintf("level(%d)", int(l))
}

// ParseLevel reads "view", "comment", "edit", "manage" (or "none").
func ParseLevel(s string) (Level, bool) {
	for l, n := range levelNames {
		if n == s {
			return l, true
		}
	}
	return None, false
}

func (l Level) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }

func (l *Level) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("level must be a string")
	}
	v, ok := ParseLevel(s)
	if !ok {
		return fmt.Errorf("level must be view, comment, edit or manage")
	}
	*l = v
	return nil
}

// Ref names one resource: a registered kind and the feature's row id.
type Ref struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

func (r Ref) String() string { return r.Kind + ":" + r.ID.String() }

// Kind is a type of shareable resource, registered by the feature that owns
// it (in its New). Name, Title and Link are required.
type Kind struct {
	// Name is "<feature>.<thing>", lowercase: docs.page, files.item, crm.record.
	Name string
	// Label is the singular noun used in copy: "page" ("Sam shared a page
	// with you", "Page not found.").
	Label string
	// Title returns the resource's display title for notifications, the share
	// dialog and agent tools. It runs only after the caller passed a view check.
	Title func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error)
	// Link returns the app path that opens the resource: "/docs/" + id.
	Link func(id uuid.UUID) string
	// PublicPath returns the web path a public link opens ("/p/" + token).
	// Nil: this kind has no public links.
	PublicPath func(token string) string
	// PublicComment lets public links grant comment (default: view only).
	PublicComment bool
	// AdminsManage gives org owners and admins manage on every resource of
	// this kind in their org. It is copied onto each resource when it is
	// created (change existing rows with an app migration). Resources
	// without an owner, or whose owner left the org, are admin-managed
	// regardless.
	AdminsManage bool
	// Levels that may be granted (nil: view, comment, edit, manage).
	Levels []Level
	// Permission is an org permission key (RegisterPermission) a person needs
	// in the resource's org to reach this kind at all ("crm.access"). Without
	// it LevelOf answers None and Require 404, so access's sharing endpoint,
	// topics and tools, and every feature route built on them, treat the
	// resource as missing. The SQL helpers (access_level, access_visible)
	// don't apply it: gate the feature's own routes with RequirePermission.
	// An unregistered key lets nobody in.
	Permission string
	// LinkPermission is an org permission the creator of a public link needs
	// besides access.share_public ("docs.publish"): to make one, and, live,
	// for it to keep opening (ResolveLink, LinkLevel). It applies to links
	// made on a resource of this kind.
	LinkPermission string
}

var (
	kindsMu   sync.RWMutex
	kinds     = map[string]Kind{}
	kindName  = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	allLevels = []Level{View, Comment, Edit, Manage}
)

// RegisterKind adds a kind of resource. Call it from the feature's New; it
// panics on an invalid kind (a programming error).
func RegisterKind(k Kind) {
	if !kindName.MatchString(k.Name) {
		panic("access: kind name must look like feature.thing: " + k.Name)
	}
	if k.Title == nil || k.Link == nil {
		panic("access: kind " + k.Name + " needs Title and Link")
	}
	if k.Label == "" {
		k.Label = "item"
	}
	if len(k.Levels) == 0 {
		k.Levels = allLevels
	}
	for _, l := range k.Levels {
		if l < View || l > Manage {
			panic("access: kind " + k.Name + " lists an invalid level")
		}
	}
	slices.Sort(k.Levels)
	kindsMu.Lock()
	kinds[k.Name] = k
	kindsMu.Unlock()
}

// KindOf returns a registered kind.
func KindOf(name string) (Kind, bool) {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	k, ok := kinds[name]
	return k, ok
}

func (k Kind) allows(l Level) bool { return slices.Contains(k.Levels, l) }
