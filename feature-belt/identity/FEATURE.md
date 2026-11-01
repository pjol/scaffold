# Identity verification

## What it does

- From Settings → Verification a user starts verification: with Persona configured they complete Persona's hosted flow and return to the app; otherwise they send a photo of their ID and a selfie for staff review.
- Status moves none → pending → verified, rejected (with a reason the user sees) or expired; the screen shows whether their part is done and offers the next step ("Continue", "Try again", "Contact support" after the last attempt).
- Staff work a review queue: the photos open through 5-minute links (each view is audited), approve or reject; the photos are deleted right after the decision. Staff can revoke a verification and can't review their own.
- A verified badge (`VerifiedBadge` on web and mobile) shows next to people; other modules gate actions with `identity.IsVerified`.
- Optional expiry (`IDENTITY_VERIFICATION_TTL`) lapses verifications and tells the user to verify again.

## Data

| Table | Purpose |
|---|---|
| `identity_verifications` | one row per attempt: provider (`persona`/`manual`), provider inquiry id, status, reason, review photo keys (until deleted), submitted/decided/expiry timestamps, reviewer |
| `identity_redactions` | provider inquiries to erase after an account deletion (provider + inquiry id only) |

A user's status is their verified, unexpired attempt if any, else their newest attempt. The app stores no names, ID numbers, birth dates or images.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/identity/status` | user | `{status, provider, submitted, reason, verified_at, expires_at, attempts_left, updated_at}` |
| POST | `/v1/identity/start` | active, 10/h, detached | `{redirect_uri?}` → `{status, flow}`; `flow` = Persona `{inquiry_id, session_token, hosted_url, environment_id}`, null in manual mode |
| POST | `/v1/identity/documents` | active, 10/h | manual mode: `{document_key, selfie_key}` (uploads with purpose `identity_document`, private) |
| GET | `/v1/identity/users/{id}/badge` | public | `{user_id, verified}`; unknown ids read as false |
| GET | `/v1/admin/identity/verifications?status=` | support | no status = review queue (submitted, oldest first) |
| GET | `/v1/admin/identity/verifications/{id}` | support | detail with 5-minute presigned photo links (audited `identity.documents.view`) |
| POST | `/v1/admin/identity/verifications/{id}/approve` | support | submitted → verified (audited) |
| POST | `/v1/admin/identity/verifications/{id}/reject` | support | `{reason}`: submitted → rejected, or revoke a verified attempt (audited) |
| POST | `/webhooks/persona` | Persona signature | verified (`t=,v1=` HMAC, 5 min), deduped with `webhooks.Once`, inquiry re-fetched before applying |

Realtime: `identity.status` (status) on `user:<id>`; `identity.submitted {id}` on `admin:live`. Notification kind `identity.status`. Agent tool `identity_status` (read-only).

Go exports for stitching: `IsVerified(ctx, q, userID) (bool, error)`, `VerifiedSet(ctx, q, ids)`, `ErrVerificationRequired` (403 `verification_required`), `Provider` and `Redactor` interfaces.

## Screens

| Surface | Route | Notes |
|---|---|---|
| Web | `/settings/identity` (Settings section "Verification") | status card; Persona → hosted flow returning to this page; manual → ID photo + selfie uploaders and "Send for review"; rejected reason in a banner |
| Web | `VerifiedBadge` (`@/features/identity/VerifiedBadge`) | `verified` or `userId` (public badge, cached 5 min); renders nothing when unverified |
| Web admin | `/admin/identity`, `/admin/identity/[id]` | queue with a status filter; review page with both photos (presigned, never through `next/image`), approve, reject/revoke with a reason, "Open in Persona" |
| Mobile | `/settings/identity` (Settings row "Verification") | same states; Persona in an auth session returning to `scaffoldapp://settings/identity`; manual photos through the system picker or camera |
| Mobile | `VerifiedBadge` (`@/features/identity/VerifiedBadge`) | same props |

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

No npm or Go dependencies beyond the template (`expo-web-browser`, `expo-linking`, `expo-image-picker` are already there).

## Compliance

- Data minimisation: status, provider inquiry id, timestamps, reviewer. Manual-review photos are private objects, deleted right after the decision with every other `identity_document` upload of the user (`Storage.DeleteOwnerPurpose`; only the decided keys while a newer attempt is open); housekeeping retries every 10 minutes.
- Abandoned uploads: a manual attempt without submitted photos after 24 hours is expired and the user's `identity_document` uploads deleted (the user starts again); retiring a manual attempt when Persona is switched on deletes them too.
- Deletion: attempts deleted; review photos go with core's storage cleanup of the user's prefix; Persona inquiries are queued in `identity_redactions` and redacted after the deletion commits (`AccountDeleted` hook), then by housekeeping (up to 20 tries).
- Export: current status and every attempt.
- Privacy policy: name Persona when configured (ID images, selfies, biometrics); in manual mode say staff view the photos once and delete them.
- Mobile camera and photos use the template's generic `expo-image-picker` purpose strings ("…take photos you choose to share"), which cover ID and selfie photos.

## Stitching

1. Decide which actions require verification; gate them with `identity.IsVerified` and return `identity.ErrVerificationRequired`; on clients send people to `/settings/identity` on `verification_required`.
2. Place `VerifiedBadge` next to names (profiles, listings, chat headers); use `identity.VerifiedSet` server-side for lists.
3. Persona: create an inquiry template, point a webhook at `https://<api>/webhooks/persona` (inquiry events), set the 4 `PERSONA_*` variables, name Persona in the privacy policy. Hosted-flow returns are allowed to `CORS_ALLOWED_ORIGINS`/`PUBLIC_WEB_URL` and `scaffoldapp://`.
4. Manual mode: someone works the queue daily.
5. With notifications installed, register kind `identity.status` ("Identity verification").

## Extending

- Another vendor: implement `Provider` (and `Redactor`) and set it in `New`.
- Keep the attempt model and `IsVerified` semantics; app columns go in an `app` migration, never in `0500_identity_init.sql`.

## Provenance

Distilled from DOGPATCH `backend/internal/identity` (Persona client, `t=,v1=` signature verifier, webhook re-fetch, attempt limit, manual fallback). Stripped KYB, licence matching and verified names.
Fixed on the way: manual mode gave reviewers nothing to look at (now ID photo + selfie through audited presigned links, deleted after the decision); verified PII was stored (now none); no provider-side erasure on account deletion (now queued redaction).

Unverified against the live Persona API (exercised only against a local mock): the pinned `Persona-Version: 2026-09-29` (copied from DOGPATCH) and the redaction call `DELETE /api/v1/inquiries/{id}`. Check both in a Persona sandbox before launch.

## Changes

- 1.0.0: first release.
- 1.0.1: unsubmitted `identity_document` uploads are deleted (after each decision, when a manual attempt is retired, and 24 hours after an abandoned manual start); dropped the camera purpose-string stitch (the template's string is generic).
