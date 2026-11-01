package feed

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/realtime"
)

// AudienceAll is the default audience: every signed-in user.
const AudienceAll = "all"

// Scope decides who reads and posts to which audience. Every post stores one
// audience key: "all", or "<kind>:<uuid>" for anything narrower (for example
// "org:<org id>"). The uuid also names the audience's realtime topic,
// feed:<uuid> ("all" uses the nil uuid).
//
// The default, Everyone, shows every post to every signed-in user. To scope
// the feed by org, implement Scope in the app and register it with SetScope
// from an init in internal/api (see FEATURE.md, Stitching).
type Scope interface {
	// Audiences returns the audience keys userID may read. The feed, the
	// unread count, the agent tools and realtime topics all filter on it.
	Audiences(ctx context.Context, userID uuid.UUID) ([]string, error)
	// CanPost reports whether caller may post to audience. Announcements are
	// additionally limited to staff by the feed itself.
	CanPost(ctx context.Context, caller *auth.Identity, audience string) (bool, error)
	// EachMember calls fn with batches of the user ids in audience (active
	// accounts only). Announcements fan out through it.
	EachMember(ctx context.Context, audience string, fn func(ids []uuid.UUID) error) error
}

// AudienceNamer is optional on a Scope: it names an audience for people
// (global search shows a post's context as "<name> · Feed") and says which
// org it belongs to (uuid.Nil for none), so search scoped to an org keeps
// only that org's posts.
type AudienceNamer interface {
	AudienceName(ctx context.Context, audience string) (name string, orgID uuid.UUID, err error)
}

var (
	scopeMu      sync.Mutex
	scopeFactory func(d *app.Deps) Scope
)

// SetScope replaces the default scope. Call it from an init function in
// internal/api (it runs before modules are constructed); the factory gets
// Deps so the scope can query the database.
func SetScope(factory func(d *app.Deps) Scope) {
	scopeMu.Lock()
	scopeFactory = factory
	scopeMu.Unlock()
}

func buildScope(d *app.Deps) Scope {
	scopeMu.Lock()
	f := scopeFactory
	scopeMu.Unlock()
	if f != nil {
		if sc := f(d); sc != nil {
			return sc
		}
	}
	return Everyone{Pool: d.Pool}
}

// Everyone is the default scope: one audience, "all", that every signed-in
// user reads and posts to.
type Everyone struct{ Pool *pgxpool.Pool }

func (Everyone) Audiences(context.Context, uuid.UUID) ([]string, error) {
	return []string{AudienceAll}, nil
}

func (Everyone) CanPost(_ context.Context, _ *auth.Identity, audience string) (bool, error) {
	return audience == AudienceAll, nil
}

func (e Everyone) EachMember(ctx context.Context, audience string, fn func(ids []uuid.UUID) error) error {
	if audience != AudienceAll {
		return nil
	}
	after := uuid.Nil
	for {
		rows, err := e.Pool.Query(ctx, `
			SELECT id FROM users WHERE status = 'active' AND id > $1 ORDER BY id LIMIT 500`, after)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := fn(ids); err != nil {
			return err
		}
		after = ids[len(ids)-1]
	}
}

// AudienceTopic is the realtime topic events for audience are published on
// ("" when the key is malformed).
func AudienceTopic(audience string) string {
	id, ok := audienceID(audience)
	if !ok {
		return ""
	}
	return realtime.Topic("feed", id)
}

// audienceID extracts the uuid an audience key carries ("all" → nil uuid).
func audienceID(audience string) (uuid.UUID, bool) {
	if audience == AudienceAll {
		return uuid.Nil, true
	}
	_, rest, ok := strings.Cut(audience, ":")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	return id, err == nil && id != uuid.Nil
}

// topicID parses a feed:<uuid> topic.
func topicID(topic string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(topic, "feed:")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	return id, err == nil
}
