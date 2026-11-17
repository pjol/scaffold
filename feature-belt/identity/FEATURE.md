# Identity verification

## What it does

- From Settings → Verification a user starts verification: with Persona configured they complete Persona's hosted flow and return to the app; otherwise they send a photo of their ID and a selfie for staff review.
- Status moves none → pending → verified, rejected (with a reason the user sees) or expired; the screen shows whether their part is done and offers the next step ("Continue", "Try again", "Contact support" after the last attempt).
- Staff work a review queue: the photos open through 5-minute links (each view is audited), approve or reject; the photos are deleted right after the decision. Staff can revoke a verification and can't review their own.
- A verified badge (`VerifiedBadge` on web and mobile) shows next to people; other modules gate actions with `identity.IsVerified`.
- Optional expiry (`IDENTITY_VERIFICATION_TTL`) lapses verifications and tells the user to verify again.
- Verification badges beside ID checks, each with its own mark: **official** (admins grant), **org** and **notable** (people apply in a category with evidence: links, domains proved by DNS TXT, linked accounts, notes; staff approve with an expiry, ask for more, or reject), **paid** (a payments entitlement grants and ends it). A person shows their highest: official > org > notable > paid.
- The paid badge needs an account `IDENTITY_PAID_MIN_ACCOUNT_DAYS` old, in good standing (and ID-verified with `IDENTITY_PAID_REQUIRES_VERIFIED`); one staff removed stays off, and its plan stops renewing, until an admin lifts the removal.

## Data

| Table | Purpose |
|---|---|
| `identity_verifications` | one row per attempt: provider (`persona`/`manual`), provider inquiry id, status, reason, review photo keys (until deleted), submitted/decided/expiry timestamps, reviewer |
| `identity_redactions` | provider inquiries to erase after an account deletion (provider + inquiry id only) |
| `identity_badges` | one row per person and kind: label, category, source (`review`/`admin`/`billing`/`staff_revoked` = a held paid badge), granted/expires/revoked, `paid_active` (what billing last said, kept while held) |
| `identity_badge_applications` | category, name to show, evidence (`[{kind, ref, verified}]`, verified computed by the app), notes, status `submitted`/`needs_info`/`approved`/`rejected`/`withdrawn`; one open per person and category |
| `identity_domains` | domain proofs: token, verified/checked times, last error |

A user's status is their verified, unexpired attempt if any, else their newest attempt. The app stores no names, ID numbers, birth dates or images.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/identity/status` | user | `{status, provider, submitted, reason, verified_at, expires_at, attempts_left, updated_at}` |
| POST | `/v1/identity/start` | active, 10/h, detached | `{redirect_uri?}` → `{status, flow}`; `flow` = Persona `{inquiry_id, session_token, hosted_url, environment_id}`, null in manual mode |
| POST | `/v1/identity/documents` | active, 10/h | manual mode: `{document_key, selfie_key}` (uploads with purpose `identity_document`, private) |
| GET | `/v1/identity/users/{id}/badge` | public | `{user_id, verified, badge}`: `badge` is the highest (`{kind, label, kind_label, since}`) or null; unknown ids read as nothing |
| GET | `/v1/admin/identity/verifications?status=` | support | no status = review queue (submitted, oldest first) |
| GET | `/v1/admin/identity/verifications/{id}` | support | detail with 5-minute presigned photo links (audited `identity.documents.view`) |
| POST | `/v1/admin/identity/verifications/{id}/approve` | support | submitted → verified (audited) |
| POST | `/v1/admin/identity/verifications/{id}/reject` | support | `{reason}`: submitted → rejected, or revoke a verified attempt (audited) |
| POST | `/webhooks/persona` | Persona signature | verified (`t=,v1=` HMAC, 5 min), deduped with `webhooks.Once`, inquiry re-fetched before applying |
| GET | `/v1/identity/badges` | user | badges, paid status with requirements, applications, domains with their TXT record, categories, kind labels |
| POST | `/v1/identity/badges/applications` · `…/{id}/resubmit` · `…/{id}/withdraw` | active, 10/h | `{category, display_label, evidence[], note}`; 3 per 30 days, 409 `application_open`; resubmit answers `needs_info` |
| POST · DELETE | `/v1/identity/badges/domains` · `…/{domain}/check` · `…/{domain}` | active, 20/h · 30/h | add (≤ 10, no public email/hosting domains), check (5 s DNS, 10 s apart, 422 `domain_unverified` with what was found), remove |
| GET | `/v1/admin/identity/badge-applications?status=` · `…/{id}` | support | queue (submitted, oldest first) / detail with ID verification and current badges |
| POST | `/v1/admin/identity/badge-applications/{id}/decision` | support, not own | `{decision: approve\|needs_info\|reject, kind, label, expires_in_days (1–730), note}`; audited |
| GET | `/v1/admin/identity/badges?kind=&state=active\|ended\|held&user_id=` | support | badge rows, newest change first |
| POST | `/v1/admin/identity/badges` | admin, not self | `{user_id\|email, kind, label, expires_in_days}`; kind `paid` lifts a staff removal; audited |
| POST | `/v1/admin/identity/badges/{user_id}/{kind}/revoke` | support (official: admin) | `{reason}`; a paid badge is held; audited |

Realtime: `identity.status` and `identity.badges {user_id}` on `user:<id>`; `identity.submitted` and `identity.badge_application {id}` on `admin:live`. Notification kinds `identity.status`, `identity.badge` (decisions, removals, expiry, paid start/end). Agent tools `identity_status`, `identity_badges` (read-only).

Go exports: `IsVerified`, `VerifiedSet`, `ErrVerificationRequired` (403 `verification_required`), `Provider`, `Redactor`; badges: `BadgeOf(ctx, q, userID) (*HeldBadge, error)` and `BadgeSet(ctx, q, ids)` (the reader isn't `Badge`: that name is the public route's type), `SetPaidBadge(ctx, q, userID, active, until) (PaidBadgeChange, error)`, `AnnouncePaidBadge`, `CheckPaidEligible` (422 `not_eligible`), `PaidEntitlement()`, `OnPaidBadgeRevoked`, `ErrStaffRevoked`, `SetLinkedAccounts`, `(*Service).SetResolver`.

## Screens

| Surface | Route | Notes |
|---|---|---|
| Web | `/settings/identity` (Settings section "Verification") | status card; Persona → hosted flow returning to this page; manual → ID photo + selfie uploaders and "Send for review"; rejected reason in a banner |
| Web | `VerifiedBadge` (`@/features/identity/VerifiedBadge`) | `badge` and/or `verified`, or `userId` (public badge, cached 5 min): the highest badge's mark with a tooltip saying what it means, else the ID check; nothing when neither; `showLabel`. `IdentityBadgeMark` draws one kind |
| Web | `/settings/identity`, below ID verification | your badges; the paid badge (requirements and "Get the … badge" through `setIdentityPaidBadgeLink`; shown only when set or held); Apply (dialog: category, name, evidence rows, verified domains as quick picks, note); applications with status, staff notes, withdraw, update and send; domains with the TXT record to copy and Check |
| Web admin | `/admin/identity`, `/admin/identity/[id]` | tabs ID checks · Badge applications · Badges; queue with a status filter; review page with both photos (presigned, never through `next/image`), approve, reject/revoke with a reason, "Open in Persona" |
| Web admin | `/admin/identity/applications`, `…/[id]`, `/admin/identity/badges` | review page with evidence and approve / ask for more / reject dialogs; badges by state and kind, Grant (admins), Remove, Lift removal |
| Mobile | `/settings/identity` (Settings row "Verification") | same states; Persona in an auth session returning to `scaffoldapp://settings/identity`; manual photos through the system picker or camera |
| Mobile | `VerifiedBadge` (`@/features/identity/VerifiedBadge`) | same props (size in points, no `showLabel`) |
| Mobile | `/settings/identity`, below ID verification | the same badge sections; Apply in a sheet; TXT values as selectable text |

No admin on mobile (concrete/mobile.md). The status screen updates live from `identity.status`.

## Config

| Variable | Default | Notes |
|---|---|---|
| `PERSONA_API_KEY` | — | secret; set → Persona, empty → manual review |
| `PERSONA_TEMPLATE_ID` | — | required with the key |
| `PERSONA_WEBHOOK_SECRET` | — | secret; required with the key |
| `PERSONA_ENVIRONMENT_ID` | — | passed to the hosted flow and clients |
| `IDENTITY_MAX_ATTEMPTS` | `3` | rejections before "contact support" (403 `attempts_exhausted`) |
| `IDENTITY_VERIFICATION_TTL` | `0` | Go duration; 0 = never expires |
| `IDENTITY_BADGE_LABELS` | `official=Official,org=Organization,notable=Notable,paid=Supporter` | kind names (overrides merge) |
| `IDENTITY_BADGE_CATEGORIES` | `business:org:Business,organization:org:Organization,public_figure:notable:Public figure,creator:notable:Creator` | `key:kind:Label`; empty = no applications |
| `IDENTITY_BADGE_TTL_DAYS` | `365` | default life of a reviewed badge (1–730) |
| `IDENTITY_PAID_ENTITLEMENT` | `supporter` | the payments entitlement that grants the paid badge |
| `IDENTITY_PAID_MIN_ACCOUNT_DAYS` | `30` (`0` in development) | account age before the paid badge shows |
| `IDENTITY_PAID_REQUIRES_VERIFIED` | `false` | paid badge only after ID verification |

No npm or Go dependencies beyond the template (`expo-web-browser`, `expo-linking`, `expo-image-picker` are already there).

## Compliance

- Data minimisation: status, provider inquiry id, timestamps, reviewer. Manual-review photos are private objects, deleted right after the decision with every other `identity_document` upload of the user (`Storage.DeleteOwnerPurpose`; only the decided keys while a newer attempt is open); housekeeping retries every 10 minutes.
- Abandoned uploads: a manual attempt without submitted photos after 24 hours is expired and the user's `identity_document` uploads deleted (the user starts again); retiring a manual attempt when Persona is switched on deletes them too.
- Deletion: attempts, badges, applications and domain proofs deleted; review photos go with core's storage cleanup of the user's prefix; Persona inquiries are queued in `identity_redactions` and redacted after the deletion commits (`AccountDeleted` hook), then by housekeeping (up to 20 tries).
- Export: current status, every attempt, every badge (ended ones too), application and domain proof.
- Badges are public (the badge route is unauthenticated); applications and evidence reach only staff, whose links open in a new tab with `noopener noreferrer`; the app never fetches them. Domain checks make DNS queries only.
- Privacy policy: name Persona when configured (ID images, selfies, biometrics); in manual mode say staff view the photos once and delete them.
- Mobile camera and photos use the template's generic `expo-image-picker` purpose strings ("…take photos you choose to share"), which cover ID and selfie photos.

## Stitching

1. Decide which actions require verification; gate them with `identity.IsVerified` and return `identity.ErrVerificationRequired`; on clients send people to `/settings/identity` on `verification_required`.
2. Place `VerifiedBadge` next to names (profiles, listings, chat headers); for lists pass `badge`/`verified` from `identity.BadgeSet`/`VerifiedSet`.
3. Persona: create an inquiry template, point a webhook at `https://<api>/webhooks/persona` (inquiry events), set the 4 `PERSONA_*` variables, name Persona in the privacy policy. Hosted-flow returns are allowed to `CORS_ALLOWED_ORIGINS`/`PUBLIC_WEB_URL` and `scaffoldapp://`.
4. Manual mode: someone works the queue daily.
5. With notifications installed, register kinds `identity.status` ("Identity verification") and `identity.badge` ("Verification badges").
6. Badges: set `IDENTITY_BADGE_CATEGORIES` (your community's categories, or empty) and `IDENTITY_BADGE_LABELS`; decide who reviews applications; gate on a badge with `BadgeOf` (e.g. only `org` may create events).
7. Paid badge with payments: create a plan whose entitlement is `IDENTITY_PAID_ENTITLEMENT`, then add `backend/internal/api/stitch_identity_payments.go` (imports `context`, `time`, uuid, pgx, `app`, `db`, `identity`, `payments`):
   ```go
   func init() {
   	RegisterModule(func(d *app.Deps) app.Module {
   		payments.OnEntitlementChange(func(ctx context.Context, tx pgx.Tx, ch payments.EntitlementChange) error {
   			if ch.Key != identity.PaidEntitlement() { return nil }
   			var until time.Time
   			if ch.Until != nil { until = *ch.Until }
   			res, err := identity.SetPaidBadge(ctx, tx, ch.UserID, ch.Active, until)
   			if err != nil { return err }
   			payments.AfterCommit(ctx, "identity.paid_badge", func(ctx context.Context) error {
   				if res.Outcome == identity.PaidHeld { // staff removed it: stop renewing the plan that pays for it
   					return payments.From(d).StopRenewal(ctx, ch.UserID, ch.Key)
   				}
   				return identity.AnnouncePaidBadge(ctx, res)
   			})
   			return nil
   		})
   		identity.OnPaidBadgeRevoked(func(ctx context.Context, userID uuid.UUID) error {
   			return payments.From(d).StopRenewal(ctx, userID, identity.PaidEntitlement())
   		})
   		payments.GuardCheckout(func(ctx context.Context, q db.Querier, userID uuid.UUID, p payments.GuardedPrice) error {
   			if p.Entitlement != identity.PaidEntitlement() { return nil }
   			return identity.CheckPaidEligible(ctx, q, userID) // 422 not_eligible: nobody pays for a badge they can't hold
   		})
   		return stitchIdentityPayments{}
   	})
   }
   type stitchIdentityPayments struct{}
   func (stitchIdentityPayments) Name() string { return "stitch_identity_payments" }
   ```
   Clients, once at startup (e.g. top of each `features/registry.tsx`): web `setIdentityPaidBadgeLink((e) => `/payments?entitlement=${e}`)` from `@/features/identity/seams`; mobile the same with `/payments/plans?entitlement=${e}` (store rules apply: keep `EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT` off for a digital perk, so the mobile plan screen offers no purchase). Supporters who paid before the stitch: run `payments.From(d).ReplayEntitlements(ctx, identity.PaidEntitlement())` once.

## Extending

- Another vendor: implement `Provider` (and `Redactor`) and set it in `New`.
- Keep the attempt model and `IsVerified` semantics; app columns go in an `app` migration, never in a shipped `05xx` file.
- Account evidence: `identity.SetLinkedAccounts` with a checker of refs like `github:octocat` against accounts the person connected (e.g. the connectors feature); without one it reaches reviewers unverified. DNS: `(*Service).SetResolver` (tests, pinned resolvers).
- The paid badge can follow any source, not only payments: call `SetPaidBadge` inside the transaction that changed it and `AnnouncePaidBadge` after commit. Don't write `identity_badges` elsewhere: the hold, the withheld-until-eligible retry and the expiry job depend on its columns.

## Provenance

Distilled from DOGPATCH `backend/internal/identity` (Persona client, `t=,v1=` signature verifier, webhook re-fetch, attempt limit, manual fallback). Stripped KYB, licence matching and verified names.
Fixed on the way: manual mode gave reviewers nothing to look at (now ID photo + selfie through audited presigned links, deleted after the decision); verified PII was stored (now none); no provider-side erasure on account deletion (now queued redaction).

1.1.0 badges are distilled from MOX (`internal/verification`: `service.go`, `applications.go`, `http.go`; `internal/billing` for the paid badge; `VerificationSettings`, `VerificationQueue`, `VerifiedBadge`): badge kinds and precedence, applications with evidence and computed `verified`, the public-domain block list, DNS TXT proofs, staff decisions with expiry, no self-review, `official` for admins only, the staff hold on the paid badge (`ErrStaffRevoked`, `RestorePaid`) with its renewal stopped, eligibility before checkout, distinct marks. Generalised: categories and labels from env, users by uuid, `<slug>-verify` records, account evidence through a seam (MOX: Privy socials, Moxfield), no handle rules or feed activity, paid badge through `SetPaidBadge` instead of importing billing. Fixed: MOX's grants and decisions weren't transactional with their audit rows (now one transaction); a `needs_info` application couldn't be answered (now resubmit); a paid badge bought by someone ineligible was silently lost (now withheld and shown once eligible while paid for); the TXT record is accepted at the domain too.

Verified for 1.1.0 in a generated identity+payments project: `go build`/`vet`; `go test` with `TEST_DATABASE_URL` (precedence, expiry, paid rules incl. hold/restore and withheld grants, application lifecycle, domain proofs with a fake resolver, deletion); the stitch above compiled and driven through real payments syncs (subscribe → badge, cancel → gone, suspended → checkout 422), and the client seam calls typechecked; `pnpm typecheck`/`check`; `scaffold doctor`; the seeded backend booted and its badge routes exercised with dev tokens (apply, 409, needs_info → resubmit → approve, admin grant by email, self-grant 403, revoke, audit rows, export, a real DNS "not found"). Not verified: the screens in a browser or on a device (no Playwright, `next build` or `expo export`), a TXT record found by a real resolver, realtime events and notification delivery.

Unverified against the live Persona API (exercised only against a local mock): the pinned `Persona-Version: 2026-09-29` (copied from DOGPATCH) and the redaction call `DELETE /api/v1/inquiries/{id}`. Check both in a Persona sandbox before launch.

## Changes

- 1.0.0: first release.
- 1.0.1: unsubmitted `identity_document` uploads are deleted (after each decision, when a manual attempt is retired, and 24 hours after an abandoned manual start); dropped the camera purpose-string stitch (the template's string is generic).
- 1.1.0: verification badges (official, org, notable, paid) with applications, evidence, DNS domain proofs, staff review and grants, expiry, the paid badge through `SetPaidBadge` with a staff hold; migration `0501_identity_badges.sql`; the public badge route returns `badge`; `VerifiedBadge` takes `badge`; Settings → Verification and Admin → Verification gain badge sections and tabs.
