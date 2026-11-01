# Organizations

## What it does

- Anyone with an active account creates an organization (name, URL name, picture) and becomes its owner.
- Owners and admins invite people by email (the link works for 7 days and only for that verified email) or make a shareable link anyone signed in can join with, optionally limited in uses or days.
- Roles: owner, admin, member. Admins manage members and invites; only owners add or remove owners, transfer ownership or delete the org. There is always at least 1 owner.
- Invites to your email show in the app (Settings → Organizations, with a badge on mobile); accept or decline there or from the email link.
- An active organization is kept per device (`useActiveOrg()`), so other features can scope screens to it; the org-scoped API guard is `orgs.Require`.

## Data

| Table | Purpose |
|---|---|
| `orgs` | name, unique slug, avatar storage key, creator |
| `org_members` | (org, user) with role owner, admin or member; cascades with the org |
| `org_invites` | email invites: lowercase email, role, sha256 token hash, status, expiry, resend count |
| `org_invite_links` | shareable links: token hash, max uses, uses, expiry, revoked |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET / POST | `/v1/orgs` | user / active, 10/h | your orgs / create `{name, slug?, avatar_key?}` |
| GET / PATCH / DELETE | `/v1/orgs/{orgID}` | member / admin / owner | read / `{name?, slug?, avatar_key?}` (`""` clears) / delete with members, invites, links |
| GET | `/v1/orgs/{orgID}/members` | member | owners first; emails only for admins and owners |
| PATCH | `/v1/orgs/{orgID}/members/{userID}` | admin | `{role}`; owner changes need an owner; the last owner stays |
| DELETE | `/v1/orgs/{orgID}/members/{userID}` | member | your own id leaves; others need a higher role |
| POST | `/v1/orgs/{orgID}/transfer` | owner | `{user_id}` becomes owner, you become admin |
| GET / POST | `/v1/orgs/{orgID}/invites` | admin / admin, 20/h | pending (`?status=all`) / `{email, role}`: emails the link, notifies an existing account |
| POST | `/v1/orgs/{orgID}/invites/{inviteID}/resend` | admin, 20/h | new token, 7 more days, 1-minute cooldown |
| DELETE | `/v1/orgs/{orgID}/invites/{inviteID}` | admin | revoke |
| GET / POST | `/v1/orgs/{orgID}/links` | admin / admin, 20/h | links / `{max_uses?, expires_in_days?}`: `url` + `token` returned only here |
| DELETE | `/v1/orgs/{orgID}/links/{linkID}` | admin | revoke |
| GET | `/v1/orgs/invites/{token}` | public, 30/min per IP | preview: org, role, masked email, status |
| POST | `/v1/orgs/invites/{token}/accept` | active | join (email must match for email invites) |
| GET | `/v1/orgs/my-invites` | user | pending invites to your email |
| POST | `/v1/orgs/my-invites/{id}/accept`, `/decline` | active / user | from the app |
| GET | `/v1/admin/orgs`, `/v1/admin/orgs/{orgID}` | support | `?q=` name, slug or id, `?member_id=`; detail with owners and admins |

Realtime: `orgs:<id>` (members only): `orgs.updated`, `orgs.deleted`, `orgs.member_joined`, `orgs.member_updated`, `orgs.member_removed`; the affected person's `user:` topic gets `orgs.membership_changed`; removal revokes the topic. Notifications (registered): `orgs.invite` (link `/settings/orgs`), `orgs.role` (link `/settings/orgs/<id>`).

Agent tools (read-only): `orgs_list_mine`, `orgs_list_members`, `orgs_list_pending_invites`.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Settings → Organizations | `/settings/orgs`: invitations (Join/Decline), active organization switcher, your orgs with role, New | `settings/orgs`: invitations, active org (check), your orgs, New (sheet); settings row badge = pending invites |
| One org | `/settings/orgs/[orgID]?tab=general\|members\|invites`: picture, name, URL name (admins), leave, delete (owners, type DELETE); members with role menu, transfer, remove; invite by email, pending invites (resend, revoke), links (new link dialog shows the URL once, copy) | `settings/orgs/[orgID]`: General, Members (tap → action sheet → confirm), Invites (email + role, pending → resend/revoke, "Share a new link" opens the share sheet) |
| Invite link | `/invites/[token]` public preview → "Sign in to join"; signed in → `/orgs/join/[token]` (behind sign-in and consent) → Join | `invites/[token]` public route and deep link (`<scheme>://invites/<token>`): preview; signed out → "Sign in to join", which resumes here after sign-in and consent → Join |
| Admin → Organizations | `/admin/orgs` search table; `/admin/orgs/[id]` facts, owners and admins | none |

The switcher: the web account menu lists your orgs (active one checked) and links to its settings (`OrgMenuItems`, registry `accountMenuItems`); Settings → Organizations has it too on web and mobile. `useActiveOrg()` (shared) reads the choice. Every async view has skeleton, empty and error states.

## Config

No variables of its own: `PUBLIC_WEB_URL` (core) builds invite links; `org_avatar` upload purpose (public, images, 5 MB). No npm dependencies. Go: `golang.org/x/text` (already in the template's graph) becomes direct.

## Compliance

- Deletion: the user leaves every org. If they were the only owner and others remain, the oldest admin (else oldest member) becomes owner; empty orgs are deleted. Invites and links they made keep working without their name; org pictures they uploaded are cleared (their files go with the account's storage).
- Export: memberships, pending invites sent, invite links made.
- Invite emails go only to addresses an admin typed; 20 per hour per person, 50 pending per org, 1-minute resend cooldown. Tokens are stored hashed and never placed in notifications.
- Accepting an invite is a gated write: new accounts accept the Terms first (the web join page and mobile route sit behind the consent gate).

## Stitching

1. Name the concept (team, club, workspace) in copy; keep the API names.
2. Decide who may create orgs and whether a new account must join or create one first.
3. Scope app tables with `org_id … ON DELETE CASCADE` and guard routes with `orgs.Require` on an `{orgID}` param; read the active org with `useActiveOrg()`.
4. Set `PUBLIC_WEB_URL`. `/invites/(.*)` is wired into `web/proxy.ts` public paths and `invites/[token]` into mobile `publicRoutes`.
5. Mobile: configure universal links for `PUBLIC_WEB_URL/invites/*` if invite emails should open the app (scheme links already work and resume after sign-in).
6. Add `OrgSwitcher` to the top bar if the product is org-centric (default: account menu + Settings).

## Extending

```go
role, err := orgs.RoleOf(ctx, q, orgID, userID) // "owner" | "admin" | "member" | "" (not a member)
r.With(orgs.Require("admin")).Post("/projects/orgs/{orgID}/things", h) // 404 non-members, 403 lower roles
m, _ := orgs.FromContext(r.Context())          // m.OrgID, m.Role in a guarded handler
```

- Client: `useActiveOrg()` → `{ org, orgs, setActive }`; `orgRoleAtLeast(role, "admin")`; web `OrgSwitcher`, `OrgMenuItems` and `CreateOrgDialog` from `@/features/orgs`.
- More roles: extend the CHECK in an app migration, `rank` in `orgs.go` and `ORG_ROLES` in shared; keep owner highest.
- Don't store invite tokens in plain text or notifications; link to `/settings/orgs` instead.

## Provenance

- DOGPATCH `internal/business` (team invites): one pending invite per email, 7-day expiry, pending cap, per-hour rate limit, email match on accept, expiry worker. Fixed: invites are hashed links instead of id-only.
- ASSEMBLY chapters and allowlist: membership with roles from an email. Fixed: it sent no email and had no tokens; this adds real invites and links.
- OLEARY/crm orgs and teams: owner/admin/member roles and the org-scoped guard.

Unverified: mobile screens were typechecked and bundled (`expo export`), not driven on a device.

## Changes

- 1.0.2: shared hook renamed to `useRemoveOrgMember` (the generic name collided with another feature's export).
- 1.0.0: first release (backend, shared hooks, web and mobile UI).
- 1.0.1: org switcher in the web account menu; `/invites/(.*)` public in `web/proxy.ts`; mobile `invites/[token]` is a public route with "Sign in to join" that resumes after sign-in.
