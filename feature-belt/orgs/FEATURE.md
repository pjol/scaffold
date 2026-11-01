# Organizations

## What it does

- Anyone with an active account creates an organization (name, URL name, picture) and becomes its owner.
- Owners and admins invite people by email (the link works for 7 days and only for that verified email) or make a shareable link anyone signed in can join with, optionally limited in uses or days.
- Roles: owner, admin, member. Admins manage members and invites; only owners add or remove owners, transfer ownership or delete the org. There is always at least 1 owner.
- Invites to your email show in the app (Settings → Organizations, with a badge on mobile); accept or decline there or from the email link.
- Join by email domain: admins claim their company's domain (verified by their own email on it, or a DNS TXT record; public providers like gmail.com never). People with a verified email on it see the org under "Organizations you can join" and either join on their own (`auto_join`; automatic when they open the app once the domain is DNS-verified) or ask and wait for an admin (`request`). An email proves one mailbox, not the domain, so a DNS record outranks it: it upgrades the org's own email verification and takes the domain from an org that holds it only by email.
- An active organization is kept per device (`useActiveOrg()`), so other features can scope screens to it; until someone picks one it is the largest org they belong to, then the one they joined last. The org-scoped API guard is `orgs.Require`.
- The product's word for an organization ("workspace", "team", "club") is one setting (`configureOrgsNoun`) that every orgs screen and the features built on orgs read.

## Data

| Table | Purpose |
|---|---|
| `orgs` | name, unique slug, avatar storage key, creator |
| `org_members` | (org, user) with role owner, admin or member; cascades with the org |
| `org_invites` | email invites: lowercase email, role, sha256 token hash, status, expiry, resend count |
| `org_invite_links` | shareable links: token hash, max uses, uses, expiry, revoked |
| `org_domains` | claimed email domain, policy `auto_join`/`request`, TXT token, verified at/by/method; a verified domain belongs to 1 org |
| `org_join_requests` | pending/approved/denied/cancelled requests through a domain; 1 pending per person and org |
| `org_departures` | who left or was removed, so auto-join never brings them back; cleared when they join again |

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
| GET / POST | `/v1/orgs/{orgID}/domains` | admin / admin, 20/h | domains (≤ 20) / `{domain, policy?}`: verified at once when your email is on it; public providers refused; another org's DNS-verified domain 409 (`taken`); one another org holds by email is added `pending` |
| POST | `/v1/orgs/{orgID}/domains/{domainID}/verify` | admin, 30/h | your email, else the TXT lookup (5 s timeout, once per 15 s per domain); failure 422 `verification_failed`, kept as `last_check_error`. On an email-verified domain, or one another org holds by email, only the TXT lookup counts: it upgrades to `dns`, or moves the domain here (theirs becomes unverified; audit `orgs.domain.superseded`) |
| PATCH / DELETE | `/v1/orgs/{orgID}/domains/{domainID}` | admin | `{policy}` / remove (members stay) |
| GET | `/v1/orgs/{orgID}/join-requests` | admin | pending, oldest first |
| POST | `/v1/orgs/{orgID}/join-requests/{requestID}/approve`, `/deny` | admin | approve: joins as member (active accounts only); deny: no new request for 7 days |
| GET | `/v1/orgs/joinable` | user | orgs your verified email's domain qualifies for (and ones you asked): `action` join, request or requested; empty when suspended |
| POST | `/v1/orgs/auto-join` | active; 60/h counted only on calls that join | joins every DNS-verified (any verified in development) `auto_join` org you qualify for and never left → `{joined}`; nothing to join answers `{joined: []}` from one indexed read; the session mounts call it |
| POST | `/v1/orgs/{orgID}/join` | active, 20/h | join through an `auto_join` domain (removed people get 403 `approval_required`) |
| POST | `/v1/orgs/{orgID}/join-requests` | active, 10/h | ask to join; admins notified once a day per person |
| DELETE | `/v1/orgs/my-join-requests/{id}` | user | withdraw |
| GET | `/v1/admin/orgs`, `/v1/admin/orgs/{orgID}` | support | `?q=` name, slug, id or domain, `?member_id=`; detail with owners, admins and domains |
| DELETE | `/v1/admin/orgs/{orgID}/domains/{domainID}` | admin role, audited | release a disputed domain |

Org routes with `{orgID}` that a non-member may call (`join`, `join-requests` POST) answer 404 unless the caller's email domain is verified for that org. Domain adds, verifications, policy changes and removals write `audit.Log` (`orgs.domain.*`).

Realtime: `orgs:<id>` (members only): `orgs.updated`, `orgs.deleted`, `orgs.member_joined`, `orgs.member_updated`, `orgs.member_removed`, `orgs.domains_changed`, `orgs.join_requests_changed`; the affected person's `user:` topic gets `orgs.membership_changed`; removal revokes the topic. Notifications (registered): `orgs.invite` (link `/settings/orgs`), `orgs.role` (link `/settings/orgs/<id>`), `orgs.join_request` (admins: link `/settings/orgs/<id>?tab=members`; the requester: the answer).

Agent tools (read-only, same checks as the routes): `orgs_list_mine`, `orgs_list_members`, `orgs_list_pending_invites`, `orgs_list_joinable`, `orgs_list_domains` (admins), `orgs_list_join_requests` (admins).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Settings → Organizations | `/settings/orgs`: invitations (Join/Decline), organizations you can join (Join, Ask to join, Requested + Withdraw), active organization switcher, your orgs with role, New | `settings/orgs`: invitations, organizations you can join, active org (check), your orgs, New (sheet); settings row badge = pending invites |
| One org | `/settings/orgs/[orgID]?tab=general\|members\|invites\|domains`: picture, name, URL name (admins), leave, delete (owners, type DELETE); requests to join (admins: Approve/Decline) above members with role menu, transfer, remove; invite by email, pending invites (resend, revoke), links (new link dialog shows the URL once, copy); Domains: add, status badge, policy select, TXT name/value with copy + Check now + last error, remove | `settings/orgs/[orgID]` (`?tab=` picks the segment): General, Members (requests to join on top; tap → action sheet → confirm), Invites (email + role, pending → resend/revoke, "Share a new link" opens the share sheet), Domains (add; tap → sheet: policy, TXT record selectable + Share, Check now, remove) |
| Invite link | `/invites/[token]` public preview → "Sign in to join"; signed in → `/orgs/join/[token]` (behind sign-in and consent) → Join | `invites/[token]` public route and deep link (`<scheme>://invites/<token>`): preview; signed out → "Sign in to join", which resumes here after sign-in and consent → Join |
| Admin → Organizations | `/admin/orgs` search table (also by domain); `/admin/orgs/[id]` facts, owners and admins, domains (Release for admins) | none |

The switcher: the web account menu lists your orgs (active one checked) and links to its settings (`OrgMenuItems`, registry `accountMenuItems`); Settings → Organizations has it too on web and mobile. `useActiveOrg()` (shared) reads the choice. Session mount `orgsAutoJoin` (web and mobile `sessionMounts`) calls `/v1/orgs/auto-join` once per identity per tab (web, module state + `sessionStorage`) or launch (mobile), however often it remounts, after consent, toasts "You joined …" and never shows an error (a failed call retries on a later mount, a 429 doesn't). The Domains tab shows the TXT record for pending domains and for email-verified `auto_join` ones. `JoinableOrgs` (web and mobile `@/features/orgs`) is the reusable "Organizations you can join" list for onboarding. Every async view has skeleton, empty and error states.

## Config

| Variable | Default | Doc |
|---|---|---|
| `ORGS_DNS_RESOLVER` | system | DNS server `host:port` for TXT lookups (split-horizon networks, tests) |
| `ORGS_PUBLIC_EMAIL_DOMAINS` | — | extra public providers no org may claim, added to the built-in list |

`PUBLIC_WEB_URL` (core) builds invite links; `org_avatar` upload purpose (public, images, 5 MB). No npm dependencies. Go: `golang.org/x/text` (already in the template's graph) becomes direct.

Domains locally: dev identities live on `<slug>.test`, which can't be published in DNS. Sign in as an org admin on that domain (sam owns the seeded Riverside Climbing Club) and add `<slug>.test`: it verifies by email at once. `.test` is claimable only with `APP_ENV=development`. To exercise the DNS path, point `ORGS_DNS_RESOLVER` at a local DNS server that answers the TXT record.

## Compliance

- Deletion: the user leaves every org. If they were the only owner and others remain, the oldest admin (else oldest member) becomes owner; empty orgs are deleted. Invites and links they made keep working without their name; org pictures they uploaded are cleared (their files go with the account's storage).
- Export: memberships, pending invites sent, invite links made.
- Invite emails go only to addresses an admin typed; 20 per hour per person, 50 pending per org, 1-minute resend cooldown. Tokens are stored hashed and never placed in notifications.
- Accepting an invite is a gated write: new accounts accept the Terms first (the web join page and mobile route sit behind the consent gate). So are joining, asking and auto-join (the session mount waits for consent); suspended accounts never auto-join or ask, and approval refuses accounts that aren't active.
- Domains: deletion removes the person's join requests and departure records; domains they added or verified stay with the org without their name. Export adds join requests, departures and domains they added or verified. Admins see a requester's email (the reason they qualify).

## Stitching

1. Name the concept once, at module load in the client stitching (web and mobile), before anything renders: `configureOrgsNoun({ one: "workspace", many: "workspaces" })` (`One`/`Many` for other capitals, `article` when "a"/"an" by first letter is wrong). Every orgs screen, its settings and admin entries and page titles, and the access, files and docs copy read it through `orgsNoun()` ("workspace"), `orgsNoun("many")`, `orgsNounTitle()` ("Workspace"), `orgsNounTitle("many")` and `orgsNounA()` ("a workspace"); use them in your own copy too. Web page titles render on the server, so also call it from a module the root layout imports. On the backend, set the same word from an `init` in the stitching package (before any module is built): `orgs.SetNoun("workspace", "workspaces")`; errors, notification kinds, permission descriptions and agent tool descriptions here and in the features built on orgs read it. API names and routes keep `org`.
2. Decide who may create orgs and whether a new account must join or create one first.
3. Scope app tables with `org_id … ON DELETE CASCADE` and guard routes with `orgs.Require` on an `{orgID}` param; read the active org with `useActiveOrg()`.
4. Set `PUBLIC_WEB_URL`. `/invites/(.*)` is wired into `web/proxy.ts` public paths and `invites/[token]` into mobile `publicRoutes`.
5. Mobile: configure universal links for `PUBLIC_WEB_URL/invites/*` if invite emails should open the app (scheme links already work and resume after sign-in).
6. Add `OrgSwitcher` to the top bar if the product is org-centric (default: account menu + Settings). If the shell has its own switcher, call `setOrgsShellSwitcher(true)` at startup: screens that offer an in-page picker (Settings → Organizations' "Active organization", access's Teams and Permissions; any screen using `useOrgPickerVisible()`) drop theirs.
7. Email domains: admins claim them by default (switch the domain routes to `Require(RoleOwner)` for owners only). If new accounts must join an org first, render `JoinableOrgs` on that onboarding screen. Add regional free-mail providers to `ORGS_PUBLIC_EMAIL_DOMAINS`.

## Extending

```go
role, err := orgs.RoleOf(ctx, q, orgID, userID) // "owner" | "admin" | "member" | "" (not a member)
r.With(orgs.Require("admin")).Post("/projects/orgs/{orgID}/things", h) // 404 non-members, 403 lower roles
m, _ := orgs.FromContext(r.Context())          // m.OrgID, m.Role in a guarded handler
msg := "Only members of this " + orgs.Noun() + " can join." // NounTitle, Nouns, NounsTitle, NounA ("a workspace")
```

- Server copy: build messages with `orgs.Noun()`… when they are sent (or in `New`), never in package-level variables, which are set before `SetNoun` runs. Features that don't require orgs word it neutrally ("org").

- Client: `useActiveOrg()` → `{ org, orgs, setActive }` (no stored choice: `defaultOrg(orgs)`, largest then most recently joined); `orgRoleAtLeast(role, "admin")`; `orgsNoun`/`orgsNounTitle`/`orgsNounA` for copy; `useOrgPickerVisible()` before rendering an in-page org picker; web `OrgSwitcher`, `OrgMenuItems` and `CreateOrgDialog` from `@/features/orgs`.
- More roles: extend the CHECK in an app migration, `rank` in `orgs.go` and `ORG_ROLES` in shared; keep owner highest.
- Don't store invite tokens in plain text or notifications; link to `/settings/orgs` instead.
- `orgs.OnMemberRemoved(func(ctx, orgID, userID uuid.UUID))` (from your `New`): runs after the commit, in the background (`bg.Go`, 30 s each, panics logged), for every way someone stops being a member: removed, left, org deleted (each member), account deleted (each org). Revoke what membership gave them there (realtime topics: `realtime.Revoke` per topic; the hub matches exact topic names). The `access` feature uses it.
- `orgs.OnRoleChanged(func(ctx, orgID, userID uuid.UUID, from, to string))`: same rules, after an admin changes someone's role or ownership moves (once for the new owner, once for the one stepping down). The `access` feature turns it into a permission-change hook. Account deletion promoting a new owner doesn't fire it.
- Domains match the email's exact domain (no subdomains) and are verified once; nothing re-checks the TXT record later, and auto-join adds people lazily when they open the app, never in bulk. Subdomain matching, periodic re-verification, a role other than member (`org_domains.role` CHECK) and SSO/SCIM provisioning are out of scope.

## Provenance

- DOGPATCH `internal/business` (team invites): one pending invite per email, 7-day expiry, pending cap, per-hour rate limit, email match on accept, expiry worker. Fixed: invites are hashed links instead of id-only.
- ASSEMBLY chapters and allowlist: membership with roles from an email. Fixed: it sent no email and had no tokens; this adds real invites and links.
- OLEARY/crm orgs and teams: owner/admin/member roles and the org-scoped guard.

- Email-domain joining (1.1.0) is new, not distilled: modelled on approved-domain joining in workspace products, with fixes they need here (removed people never auto-rejoin, public providers and `.test` outside development refused, verification rate-limited and audited).

Unverified: mobile screens were typechecked and bundled (`expo export`), not driven on a device.

## Changes

- 1.3.0: server copy names organizations with the app's word too: `orgs.SetNoun(one, many)` (call it from an `init` in the backend stitching), read by `Noun`/`Nouns`/`NounTitle`/`NounsTitle`/`NounA` in errors, notification kinds and agent tool descriptions here and in the features built on orgs.
- 1.2.0: `configureOrgsNoun` + `orgsNoun`/`orgsNounTitle`/`orgsNounA`: every orgs string (web, mobile, settings and admin entries, page titles) reads the app's word for an organization. Default active org is the largest one you belong to, then the one you joined last (was first by name; `defaultOrg`). `setOrgsShellSwitcher` / `useOrgPickerVisible` hide in-page pickers when the shell has a switcher. `OnRoleChanged` hook. Member-removed hooks run under `bg.Go`.

- 1.0.2: shared hook renamed to `useRemoveOrgMember` (the generic name collided with another feature's export).
- 1.0.0: first release (backend, shared hooks, web and mobile UI).
- 1.0.1: org switcher in the web account menu; `/invites/(.*)` public in `web/proxy.ts`; mobile `invites/[token]` is a public route with "Sign in to join" that resumes after sign-in.
- 1.1.2: `OnMemberRemoved` hooks (removed, left, org deleted, account deleted; after commit), so features revoke membership-scoped realtime topics.
- 1.1.1: security review: silent auto-join needs a DNS-verified domain outside development (one mailbox on a shared or unlisted free-mail domain let anyone claim it by email and enrol everyone on it, exposing their names and emails to the claimant); a DNS record takes a domain from an org holding it only by email, and upgrades the org's own email verification (TXT record shown for email-verified `auto_join` domains on web and mobile). `POST /v1/orgs/auto-join` answers a no-op without touching its rate limit (repeated sign-ins hit 429 before); the session mounts call it once per identity per tab/launch across remounts and stay silent on errors.
- 1.1.0: join by verified email domain: `org_domains` (email or DNS TXT verification, public providers refused, 1 org per domain, `auto_join`/`request`), join requests with admin approval and notifications, auto-join session mount, "Organizations you can join" on web and mobile, Domains tab, staff release, 3 read tools, `ORGS_DNS_RESOLVER` and `ORGS_PUBLIC_EMAIL_DOMAINS`.
