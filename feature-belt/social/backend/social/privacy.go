package social

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Privacy is social_profiles.privacy: a visibility per section key. Missing
// or invalid entries read as the section's registered default.
type Privacy map[string]string

func parsePrivacy(raw []byte) Privacy {
	p := Privacy{}
	if len(raw) == 0 {
		return p
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return p
	}
	for k, v := range m {
		if validVisibility(v) {
			p[k] = v
		}
	}
	return p
}

// of is one section's visibility: the stored choice, else the default,
// else private for a section nobody registered.
func (p Privacy) of(key string) string {
	if v, ok := p[key]; ok && validVisibility(v) {
		return v
	}
	if s, ok := section(key); ok {
		return s.Default
	}
	return VisibilityPrivate
}

// apply returns a copy with the patch applied. Unknown sections and invalid
// values are refused.
func (p Privacy) apply(patch map[string]string) (Privacy, error) {
	out := Privacy{}
	for k, v := range p {
		out[k] = v
	}
	for k, v := range patch {
		if _, ok := section(k); !ok {
			return p, httpx.BadRequest("Unknown privacy section " + k + ".")
		}
		if !validVisibility(v) {
			return p, httpx.BadRequest("Privacy is public, followers or private.")
		}
		out[k] = v
	}
	return out, nil
}

func validVisibility(v string) bool {
	return v == VisibilityPublic || v == VisibilityFollowers || v == VisibilityPrivate
}

// Viewer is who is looking, relative to a profile's owner.
type Viewer struct {
	Anonymous bool // no account (uuid.Nil)
	Self      bool
	Blocked   bool // a block in either direction
	Follower  bool // the viewer follows the owner
}

// Visible is the privacy matrix: the owner sees everything; a block in
// either direction hides everything; public sections are seen by everyone,
// followers sections by followers, private ones by nobody else.
func Visible(visibility string, v Viewer) bool {
	switch {
	case v.Self:
		return true
	case v.Blocked:
		return false
	case visibility == VisibilityPublic:
		return true
	case visibility == VisibilityFollowers:
		return !v.Anonymous && v.Follower
	}
	return false
}

// visibleSections lists the section keys v may see under privacy p (nil
// for the owner: every section, no filter).
func visibleSections(p Privacy, v Viewer) []string {
	if v.Self {
		return nil
	}
	out := []string{}
	if v.Blocked {
		return out
	}
	for _, s := range Sections() {
		if Visible(p.of(s.Key), v) {
			out = append(out, s.Key)
		}
	}
	return out
}

// viewerOf works out how viewer relates to owner.
func viewerOf(ctx context.Context, q db.Querier, owner, viewer uuid.UUID) (Viewer, error) {
	v := Viewer{Anonymous: viewer == uuid.Nil, Self: viewer != uuid.Nil && viewer == owner}
	if v.Anonymous || v.Self {
		return v, nil
	}
	var err error
	if v.Blocked, err = moderation.IsBlocked(ctx, q, owner, viewer); err != nil {
		return v, err
	}
	if v.Follower, err = follows(ctx, q, viewer, owner); err != nil {
		return v, err
	}
	return v, nil
}

// owner is what privacy checks need about a profile's owner.
type owner struct {
	id      uuid.UUID
	status  string
	privacy Privacy
}

// visible: banned and deleted accounts have no public profile.
func (o owner) visible() bool { return o.status == "active" || o.status == "suspended" }

func loadOwner(ctx context.Context, q db.Querier, id uuid.UUID) (owner, error) {
	o := owner{id: id, privacy: Privacy{}}
	var raw []byte
	err := q.QueryRow(ctx, `SELECT u.status, p.privacy FROM users u LEFT JOIN social_profiles p ON p.user_id = u.id WHERE u.id = $1`, id).
		Scan(&o.status, &raw)
	if isNoRows(err) {
		return o, nil // status "" is not visible
	}
	if err != nil {
		return o, err
	}
	o.privacy = parsePrivacy(raw)
	return o, nil
}

func follows(ctx context.Context, q db.Querier, follower, followee uuid.UUID) (bool, error) {
	if follower == uuid.Nil || followee == uuid.Nil || follower == followee {
		return false, nil
	}
	var x bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM social_follows WHERE follower_id = $1 AND followee_id = $2)`, follower, followee).Scan(&x)
	return x, err
}
