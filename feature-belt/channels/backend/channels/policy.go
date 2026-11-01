package channels

// Who may do what in a workspace is decided here and nowhere else. The
// defaults follow org roles; an app with a permissions system replaces the
// decision with SetPermissionCheck (and may fall back to DefaultPermission).

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/orgs"
)

// Action is something a workspace member may or may not be allowed to do.
type Action string

const (
	ActCreatePublic  Action = "channels.create_public"  // create a public channel
	ActCreatePrivate Action = "channels.create_private" // create a private channel
	ActArchive       Action = "channels.archive"        // archive or unarchive a channel
	ActEdit          Action = "channels.edit"           // rename, description, layout tags (any member sets the topic)
	ActRemoveMember  Action = "channels.remove_member"  // remove someone else from a channel
	ActMentionAll    Action = "channels.mention_all"    // @channel and @here
	ActSetDefault    Action = "channels.set_default"    // make a public channel one everyone joins
)

// PermissionChannel is the channel an action is about.
type PermissionChannel struct {
	ID          uuid.UUID
	Kind        string // public | private | dm | group_dm
	IsDefault   bool
	CreatedBy   *uuid.UUID
	MemberCount int
	Role        string // the caller's channel role: admin | member | "" (not a member)
}

// PermissionRequest is one decision: may UserID do Action in OrgID (on Channel)?
type PermissionRequest struct {
	Action  Action
	OrgID   uuid.UUID
	UserID  uuid.UUID
	OrgRole string             // owner | admin | member
	Channel *PermissionChannel // nil for creating
}

// PermissionCheck decides a request. Return (false, nil) to refuse.
type PermissionCheck func(ctx context.Context, q db.Querier, req PermissionRequest) (bool, error)

var permissionCheck atomic.Pointer[PermissionCheck]

// SetPermissionCheck replaces the default decisions (wire the app's access
// feature here). Call DefaultPermission inside it for actions it doesn't own.
func SetPermissionCheck(fn PermissionCheck) {
	if fn != nil {
		permissionCheck.Store(&fn)
	}
}

// mentionAllMax is CHANNELS_MENTION_ALL_MAX_MEMBERS, read once in New.
var mentionAllMax atomic.Int64

// DefaultPermission is the built-in rule set:
//   - any org member creates public and private channels;
//   - org owners and admins archive, edit and remove members anywhere, and
//     choose default channels; a channel's admins (its creator) archive,
//     edit and remove members in their own non-default channels;
//   - @channel and @here: owners and admins anywhere; members in direct
//     messages and in non-default channels with at most
//     CHANNELS_MENTION_ALL_MAX_MEMBERS members.
func DefaultPermission(req PermissionRequest) bool {
	orgAdmin := orgs.RoleAtLeast(req.OrgRole, orgs.RoleAdmin)
	ch := req.Channel
	switch req.Action {
	case ActCreatePublic, ActCreatePrivate:
		return orgs.RoleAtLeast(req.OrgRole, orgs.RoleMember)
	case ActSetDefault:
		return orgAdmin
	}
	if ch == nil {
		return false
	}
	direct := ch.Kind == kindDM || ch.Kind == kindGroupDM
	channelAdmin := ch.Role == roleAdmin && !ch.IsDefault
	switch req.Action {
	case ActArchive, ActEdit, ActRemoveMember:
		return !direct && (orgAdmin || channelAdmin)
	case ActMentionAll:
		if orgAdmin || direct {
			return true
		}
		limit := int(mentionAllMax.Load())
		return !ch.IsDefault && (limit == 0 || ch.MemberCount <= limit)
	}
	return false
}

// allowed runs the installed check (or the defaults).
func allowed(ctx context.Context, q db.Querier, req PermissionRequest) (bool, error) {
	if fn := permissionCheck.Load(); fn != nil {
		return (*fn)(ctx, q, req)
	}
	return DefaultPermission(req), nil
}

// ---- groups (@group mentions) ------------------------------------------------------

// Group is a set of people an app defines (teams, departments) that can be
// mentioned as @handle. Members of the channel in the group are notified.
type Group struct {
	ID          string `json:"id"`     // stable id; letters, digits, _ . : -
	Handle      string `json:"handle"` // what people type after @
	Name        string `json:"name"`
	MemberCount int    `json:"member_count"`
}

// GroupResolver connects @group mentions to the app's groups.
type GroupResolver interface {
	// SearchGroups returns groups in orgID that viewerID may mention, matching
	// query (prefix of handle or name; "" = any), at most limit.
	SearchGroups(ctx context.Context, orgID, viewerID uuid.UUID, query string, limit int) ([]Group, error)
	// GroupMembers returns the user ids in a group, or an error wrapping
	// pgx.ErrNoRows when the group isn't in orgID.
	GroupMembers(ctx context.Context, orgID uuid.UUID, groupID string) ([]uuid.UUID, error)
}

var groupResolver atomic.Pointer[GroupResolver]

// SetGroupResolver turns on @group mentions (stitch: the access feature's teams).
func SetGroupResolver(r GroupResolver) {
	if r != nil {
		groupResolver.Store(&r)
	}
}

func currentGroupResolver() GroupResolver {
	if r := groupResolver.Load(); r != nil {
		return *r
	}
	return nil
}

// ---- calls ---------------------------------------------------------------------------

// CallTarget is the channel or DM a call starts from.
type CallTarget struct {
	ChannelID uuid.UUID
	OrgID     uuid.UUID
	Kind      string // public | private | dm | group_dm
	Name      string // channel name, or the DM's people
	MemberIDs []uuid.UUID
}

// CallStarter starts (or joins the running) call for target as caller and
// returns the app path clients open, e.g. "/calls/<id>". Return an
// *httpx.Error to refuse with a message the person can act on.
type CallStarter func(ctx context.Context, caller *auth.Identity, target CallTarget) (path string, err error)

var callStarter atomic.Pointer[CallStarter]

// SetCallStarter shows "Start call" in channel and DM headers (stitch: the
// calls feature). Without it the button is hidden and the route answers 404.
func SetCallStarter(fn CallStarter) {
	if fn != nil {
		callStarter.Store(&fn)
	}
}

func currentCallStarter() CallStarter {
	if fn := callStarter.Load(); fn != nil {
		return *fn
	}
	return nil
}
