# Access and teams

## What it does

- Teams inside an org (name, @handle, description, picture) with leads and members. Holders of `access.manage_teams` (default owners and admins) manage every team; leads manage their own team's members.
- One sharing model for every content feature: grants to a person, a team or the whole org at **view < comment < edit < manage**; the owner has full control. Items inherit from their parent (page → page, file → folder); grants add to what is inherited; an item can be restricted to stop inheriting.
- General access like Drive: restricted, or everyone in the org at view, comment or edit. Public links (no account): view or comment, optional expiry, revocable, stored hashed, shown once; they work only while their creator holds `access.share_public` (and the kind's link permission, e.g. `docs.publish`), so admins switch public sharing off at once.
- Org permissions features register (`crm.export`) with role defaults; admins choose roles, teams and people per permission (audited). Everyone sees what they hold.
- A Share dialog (web) / sheet (mobile) every feature reuses, also offered as "Share…" in every ⋮ `ResourceMenu`. Shares notify people (never whole-org access). AI agents acting for a user get at most the user's level capped by their agent scope, and never change sharing.

## Data

| Table | Purpose |
|---|---|
| `access_teams`, `access_team_members` | teams (handle unique per org) and members with role lead/member |
| `access_resources` | registry: (kind, id), org, owner, parent, `inherits`, `admins_manage`; deleting a row removes its grants, links and descendants' rows |
| `access_grants` | (resource, subject user/team/org, level 1–4); the org grant is general access (≤ 3) |
| `access_links` | public links: sha256 token hash, level, expiry, revoked, creator, last use |
| `access_permission_policies`, `access_permission_subjects` | an org's roles for a permission, plus teams and people |

SQL: `access_level(user, kind, id, via_agent DEFAULT false) → int` is the single source of truth: org membership required; owning the item or an ancestor it inherits from = 4; org owners/admins = 4 on admin-managed, ownerless or left-owner items; else the highest grant up the inheriting chain (32 rows max); `via_agent` caps at `least(level, access_agent_cap(…), 3)`. Also `access_visible(user, kind, via_agent) → (id, level)`, `access_agent_cap(user, kind, id) → int` (stub = 3), `access_permission_allowed`, `access_link_level`. A trigger on `org_members` delete removes the leaver's team memberships, own grants and permission overrides in that org.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/access/resources/{kind}/{id}` | view | level, owner, general, parent; grants, links, `can_create_link` and `public_links.missing_permission` for managers |
| PUT | `…/{kind}/{id}/general`, `…/inherit`, `…/owner` | manage, 300/h | `{access, level}` · `{inherits}` (you keep manage) · `{user_id}` (owner or org admin; old owner keeps manage) |
| POST / PATCH / DELETE | `…/{kind}/{id}/grants[/{grantID}]` | manage (adds 120/h) | `{subjects:[{type,id}], level, notify?}` · `{level}` · remove (your own grant needs only view) |
| POST / DELETE | `…/{kind}/{id}/links[/{linkID}]` | manage + `access.share_public` (+ the kind's `LinkPermission`), 30/h | `{level, expires_at?}` → `url`, `token` once · revoke |
| GET | `/v1/access/links/{token}` | public, 30/min per IP | `{kind, id, title, level, path, expires_at}`; 404 `link_not_found` |
| GET | `/v1/access/orgs/{orgID}/directory?q=&types=` | member | teams then people for pickers (`@des` = handles); emails for org admins |
| GET / POST | `/v1/access/orgs/{orgID}/teams` | member / `access.manage_teams` | `?q=&mine=true&cursor=` (+ `can_create`) · `{name, handle?, description?, avatar_key?}` (you lead it) |
| GET / PATCH / DELETE | `/v1/access/teams/{teamID}` | member / lead (description) or manage_teams / manage_teams | |
| GET / POST / PATCH / DELETE | `/v1/access/teams/{teamID}/members[/{userID}]` | member / lead or manage_teams (your own id: leave) | `{user_ids, role}` · `{role}` |
| GET | `/v1/access/orgs/{orgID}/my-permissions` | member | `{permissions: {key: bool}, items}` |
| GET / PUT / DELETE | `/v1/access/orgs/{orgID}/permissions[/{key}]` | org admin, audited | matrix · `{roles, team_ids, user_ids}` · reset |

Every write refuses AI agent calls (403 `agent_forbidden`). Errors: 404 without view (existence never leaks), 403 `access_level_required` `{level, required}`, 403 `permission_required` `{permission}`. Audited: permission set/reset, team create/update/delete, member changes made through manage_teams, and sharing done as an org-admin override (`access.share.*`).

Realtime: `access:<resource id>` (anyone who can view) and `access:<org id>` (members): `access.changed {kind,id,org_id}` (also on affected people's `user:` topic), `access.teams_changed`, `access.team_members_changed`, `access.permissions_changed`. Lost access drops subscriptions (`realtime.Revoke`, `RevokeTopic`). Notifications: `access.shared` (link from the kind), `access.team`. Agent tools (read-only, same checks, agent-capped): `access_list_my_teams`, `access_list_team_members`, `access_my_permissions`, `access_get_sharing`. Search: teams (`access.team`, `Context` = the org, scoped by `app.SearchOrg`).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Any resource | `ShareDialog` (`@/features/access`: `kind, id, title?, open, onOpenChange`), `ShareButton`; "Share…" in `ResourceMenu` | `ShareSheet` (`kind, id, title?, open, onClose`); "Share…" in `ResourceMenu` |
| Settings → Teams | `/settings/teams` (All/Yours in the URL, search, New team), `/settings/teams/[teamID]` (about, members with ⋮, add people, edit and picture, leave, delete by typing the handle) | `settings/teams`, `settings/teams/[teamID]` (same, with sheets) |
| Settings → Permissions | `/settings/permissions`: admins get the matrix (roles × permissions, teams and people, reset; cards under 768 px); others see theirs | web only (an admin desk task) |

The dialog: picker (combobox, arrow keys) → chips → level + Notify → Share; people with access (owner; direct grants with a level menu, Make owner, Remove; inherited rows read-only with their source); general access; parent with "Only people added here"; public links (expiry, copy once, revoke, paused state, explained when the org disallows them). Viewers see their level, owner, general access and parent. Skeleton, empty and error states throughout.

## Config

No variables. `PUBLIC_WEB_URL` (core) prefixes public link URLs. Upload purpose `access_team_avatar` (public, images, 5 MB). No new npm or Go dependencies.

## Compliance

- Deletion: team memberships, grants to the user and permission overrides naming them are deleted; links they made are revoked; resources stay with their features with no owner, so org owners and admins manage them (personal resources become unreachable: their features delete them). Attribution columns are cleared.
- Export: teams, items shared with them, items they own, links they made (no tokens), permissions naming them.
- Leaving an org ends access to all its items, including ones you own (ownership is kept; admins can reassign it).

## Stitching

1. Register each kind in the owning feature's `New` (below); decide `AdminsManage` (default false: private stays private from admins; CRM-style data usually true), `Permission` (the org permission that opens the feature at all) and `LinkPermission` (a publishing permission).
2. Write the registry row in the transaction that writes the feature's row; guard every id with `Require`; filter lists with the SQL helpers, passing `access.ViaAgent(ctx)`. Grants, owner changes, restriction and links your own flows make go through `AddGrant`/`SetGrant`/`RemoveGrant`, `SetOwnerTree`/`SetOwner`, `SetInherits` and `CreateLink`; never write access tables.
3. Put secondary actions in `ResourceMenu` with `kind` and `id` so "Share…" appears; add `ShareButton` where sharing is a primary action.
4. Kinds with public links ship a public page at `PublicPath` (`ResolveLink`, `LinkLevel`) and add it to `web/proxy.ts` public paths and mobile `publicRoutes`.
5. Register an `OnChange` hook that revokes your realtime topics, and the org permissions your product needs; if a permission opens realtime topics, register `OnPermissionChange` too and revoke them when it may be gone.
6. Copy names organizations with orgs' noun (`configureOrgsNoun`); with a shell org switcher (`setOrgsShellSwitcher(true)`), Teams and Permissions drop their in-page picker.

## Extending

```go
access.RegisterKind(access.Kind{Name: "docs.page", Label: "page",
    Permission: "",              // e.g. "crm.access": LevelOf/Require, access's endpoints, topics and tools answer 404 without it
    LinkPermission: "docs.publish", // needed besides access.share_public to make a link; checked live when it opens
    Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) { /* SELECT title */ },
    Link:  func(id uuid.UUID) string { return "/docs/" + id.String() },
    PublicPath: func(token string) string { return "/p/docs/" + token }, // nil: no public links
})
err := access.CreateResource(ctx, tx, access.NewResource{Kind: "docs.page", ID: p.ID, OrgID: orgID, OwnerID: me,
    Parent: &access.Ref{Kind: "docs.page", ID: parentID} /* nil: top level */, General: access.None})
err = access.MoveResource(ctx, tx, ref, &newParent) // after commit: access.Changed(ctx, ref, nil)
err = access.DeleteResource(ctx, tx, ref)           // its grants, links and descendants' rows go too
lvl, err := access.Require(ctx, pool, me, "docs.page", id, access.Edit) // 404 / 403; agent-capped via ctx
rows, err := pool.Query(ctx, `SELECT p.id, p.title FROM docs_pages p
    WHERE p.org_id = $2 AND access_level($1, 'docs.page', p.id, $3) >= 1 AND (p.updated_at, p.id) < ($4, $5)
    ORDER BY p.updated_at DESC, p.id DESC LIMIT $6`, me, orgID, access.ViaAgent(ctx), t, lastID, limit+1)
// Everything I can see / search: JOIN access_visible($1, 'docs.page', $2) v ON v.id = p.id
// A feature's own writes (creator grants, handovers, imports, seeds): the routes' checks, no notification or audit.
err = access.AddGrant(ctx, tx, ref, access.SubjectRef{Type: "user", ID: me}, access.Manage, me) // raises, never lowers
err = access.SetGrant(ctx, tx, ref, access.SubjectRef{Type: "org", ID: orgID}, access.View, uuid.Nil) // exactly; then Changed if lowered
err = access.RemoveGrant(ctx, tx, ref, access.SubjectRef{Type: "team", ID: teamID})          // then Changed
err = access.SetOwnerTree(ctx, tx, ref, newOwner) // ref and its same-kind subtree (uuid.Nil: org admins); then Changed
err = access.SetInherits(ctx, tx, ref, false)       // restrict (no grant added: AddGrant first); then Changed
l, err := access.CreateLink(ctx, tx, ref, access.View, nil, me) // *l.URL, *l.Token exist only now; by needs share_public (+ LinkPermission)
l, err := access.ResolveLink(ctx, pool, token)                  // l.Ref, l.Level, or ErrLinkNotFound
lv, err := access.LinkLevel(ctx, pool, token, "files.item", id) // a file inside a shared folder
access.OnChange(func(ctx context.Context, ref access.Ref, users []uuid.UUID) { /* Revoke; nil users: RevokeTopic */ })
// Also runs with users = [leaver] for each of an org's resources (newest 5,000) when someone leaves it.
access.OnPermissionChange(func(ctx context.Context, orgID uuid.UUID, key string, users []uuid.UUID) { /* re-check Allowed; Revoke */ })
// After commit, bg.Go, 30 s: set/reset a permission (key, users nil = anyone in the org), team member removed or
// team deleted (key "", the people who were on it), org role changed (key "", that person; orgs.OnRoleChanged).
// Leaving the org is orgs.OnMemberRemoved's.
access.RegisterPermission(access.Permission{Key: "crm.export", Title: "Export records", DefaultRoles: []string{"owner", "admin"}})
r.With(orgs.Require(orgs.RoleMember), access.RequirePermission("crm.export")).Get("/crm/orgs/{orgID}/export", h)
```

- Kind permissions: `Permission` gates the Go level (`LevelOf`, `Require`, so every route built on them) but not `access_level`/`access_visible`: guard the feature's own list and search routes with `RequirePermission`. Notifications from shares skip people without it. `LinkPermission` applies to links made on that kind (including the pages under it they open).
- Also: `LevelOf`, `Lookup`, `SetOwner`, `Allowed`, `ViaAgent`, `TeamByHandle` (mentions), `TeamMemberIDs`, `TeamsOf`, `KindOf`, `PermissionOf`. Client: `useAccessResource`, the `useAccess…` mutations, `useAccessDirectory`, `useAccessTeams`, `useAccessMyTeams`, `useAccessPermission(orgId, key)`; web `PeoplePicker`.
- Agent scopes: the agents feature replaces `access_agent_cap` with `CREATE OR REPLACE` in its own migration (same signature and parameter names; off 0, read and suggest 1, edit 3). access never redefines it after 0700.
- Performance (shared 8-core laptop under load, 10,000-row org): `access_level` over 1,000 rows 0.1–0.3 s; a page of a folder's children 3–13 ms. A filtered keyset page where the user sees ~1% of a big table scans until it finds `limit+1` (0.35–2.3 s); `JOIN access_visible` answers the same page in 7–11 ms, so use it for "everything I can see" lists and search.
- Out of scope: guests outside the org (use public links), deny entries, editors resharing, email on share, a cross-kind "Shared with me" list (build it on `access_grants`), custom levels. Keep `access_level` the only effective-level logic; never reimplement it in Go.

## Provenance

New in the forum belt. Teams and role rules follow `orgs` (OLEARY/crm teams). The model is distilled from Google Drive (general access, link sharing, limited-access folders) and Notion (page inheritance, restricting a subpage). Designed so one SQL function backs REST, tools, search and lists, which therefore can't drift.

## Changes

- 1.3.1: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.3.0: `OnPermissionChange(func(ctx, orgID, key, users))` after a permission is set or reset, people leave a team or it is deleted, or an org role changes (orgs ≥ 1.2.0 `OnRoleChanged`), so features revoke what a permission opened (crm's topics). Copy reads orgs' noun (`orgsNoun`…). Teams and Permissions hide their org picker when the shell has one (`useOrgPickerVisible`). Team search fills `OrgID`/`Context` and honours `app.SearchOrg`. Change hooks run under `bg.Go`.

- 1.2.1: `SetInherits` (restrict an item, or let it inherit again, from a feature's own flow), so imports that keep a folder's restriction don't write `access_resources`.
- 1.2.0: `SetGrant` (exact level, person, team or org) and `RemoveGrant`; `Kind.Permission` (an org permission needed to reach the kind at all: `LevelOf`/`Require` answer none/404 without it, and with them access's sharing endpoint, topics, tools and share notifications); `Kind.LinkPermission` (needed besides `access.share_public` to make a link and, live, for it to open); the sharing state adds `can_create_link` and `public_links.missing_permission`, and the Share dialog and sheet say which permission is missing.
- 1.1.0: exported write helpers for features' own flows, so no feature writes access tables: `AddGrant` (person, team or general access; raises, never lowers), `SetOwnerTree` (a subtree's owner) and `CreateLink` (returns the URL and token once). They apply the routes' validation; no notification or audit. The links route shares `CreateLink`'s rules.
- 1.0.1: leaving an org (removed, left, org deleted, account deleted; `orgs.OnMemberRemoved` ≥ orgs 1.1.2) revokes the person's `access:<org>` subscription and, newest first up to 5,000, each org resource's `access:<id>` topic and every feature's topics through `OnChange` (users = [leaver]); live sockets kept receiving until they resubscribed before.
- 1.0.0: first release (backend, SQL helpers, agent-cap seam, shared hooks, web and mobile UI).
