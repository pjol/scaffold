# Compliance

Store review and privacy law are product requirements. The template ships the core; features add their part (each FEATURE.md has a Compliance section). Facts as of October 2026; re-check the store guidelines before a first submission.

## What the template already does

| Requirement | Where |
|---|---|
| Versioned Terms + Privacy, acceptance before any write, acceptance history | `internal/legal`, `legal_acceptances`, `packages/shared/src/legal`, consent screen on web + mobile |
| In-app account deletion (Apple 5.1.1(v), Play) | `DELETE /v1/me`; Settings → Account → Delete account; every module's `AccountDeleter`; provider user + stored files removed by a retrying worker |
| Web deletion URL (Play) | public `/delete-account` page on web: signed in → same flow; signed out → sign in first, plus the support email |
| Data export (GDPR Art. 15/20, CCPA) | `GET /v1/me/export`, Settings → Account → Download my data |
| Published contact (Apple 1.2, 5.1.1) | `SUPPORT_EMAIL` in `/v1/config`, shown in Settings and the web footer |
| Forced update for broken old builds | `MOBILE_MIN_VERSION_IOS/ANDROID` in `/v1/config`; mobile blocks with an update screen |
| Provider-side deletions | Clerk `user.deleted` webhook runs the same deletion |

## Account deletion rules

- Reachable in at most 3 taps from Settings, on web and mobile. Confirm once (type-to-confirm or a destructive dialog). No email, call or chat required.
- Deletion is real: the users row is anonymised, personal content is deleted, and the user can never sign back into it. Deactivation does not count.
- Every module that stores user data implements `AccountDeleter`:
  - **Delete**: profile data, the user's own posts, media, device tokens, preferences, AI conversations.
  - **Anonymise** (keep the row, drop the link to the person): content other people still see (messages in their conversations show "Deleted user"), moderation records.
  - **Retain, pseudonymised**: orders, invoices, payouts, refunds, ledgers and fraud evidence for 7 years (US default; UK 6, DE/FR 10). Keep `user_id` on those rows; the users row no longer identifies anyone.
- The confirmation screen says what is deleted, what is retained and why, and that it is immediate.
- Sign in with Apple: revoke the Apple token on deletion if Clerk does not (verify per project).
- Active store subscriptions: warn that store billing continues and link to the store's subscription page.

## Legal documents

- Terms and Privacy live in `packages/shared/src/legal/*.ts` with a version string equal to `backend/internal/legal/legal.go`. `pnpm check` fails when they differ.
- To change a document: edit the TS, bump both versions to today's date, run `pnpm --filter @scaffoldapp/shared legal export` (writes `docs/legal/*.md`). Every user re-accepts on next write.
- The privacy policy names every processor (Clerk, AWS/S3 host, Postgres host, Resend, Stripe, Expo push, OpenAI/Anthropic…). Adding an SDK that sees user data means updating it in the same change.
- Before launch the placeholders (company name, address, contact) must be filled and the documents read by the owner. Never ship placeholder legal text.

## User-generated content (Apple 1.2, Play UGC)

Any feature where users post content others see (chat, feed, comments, profiles with photos, AI generation) requires the `moderation` feature and:
- filtering before posting (word list + size/type limits; AI moderation where configured),
- report content and report user, with an admin queue someone works (target under 24 hours),
- block user: hides their content and stops their messages both ways,
- Terms accepted before first post (the consent gate covers it),
- published contact info,
- generative AI: in-app reporting of offensive output.
Random or anonymous chat with strangers is not allowed in the stores.

## Sign-in (Apple 4.8)

Clerk email/passkey/OTP is your own account system: Sign in with Apple is not required. Adding any social login (Google, etc.) makes Sign in with Apple mandatory on iOS (`@clerk/expo` Apple sign-in with `expo-apple-authentication`, native sheet).

## Payments (Apple 3.1.1/3.1.3, Play Billing)

- Digital content or features used in the app: Apple IAP / Play Billing. Stripe inside the app for digital goods is rejected.
- Physical goods and services used outside the app (delivery, bookings, in-person services, donations to registered charities): Stripe, never IAP.
- US storefront: a link out to web checkout for digital goods is allowed; fees are contested in court, re-check before relying on it. Other regions need their own entitlements.
- The payments feature documents which case a project is in.

## Privacy surfaces

- Privacy policy linked in-app (Settings, sign-up), on web (footer) and in both store consoles.
- Apple privacy labels and the Play Data safety form match the policy and every SDK.
- iOS privacy manifest: declare required-reason APIs in `app.config.ts` → `ios.privacyManifests` (UserDefaults `CA92.1` at minimum; add what ITMS-91053 emails list). `NSPrivacyTracking: false` unless the app tracks.
- Every permission has a specific purpose string ("Allow Scaffold App to use your camera to scan receipts"). The template's camera/photos strings cover sharing photos the user picks; a feature that uses a permission for a narrower job (scanning, nearby places, voice notes) adds its own string naming that job. Ask at the moment of use, never at launch. A denied permission leaves a working fallback. Never require push or location to use the app.
- Android: use the system photo picker; block `READ_MEDIA_*` and anything else unneeded with `android.blockedPermissions`.
- ATT only if the app tracks across apps (it should not by default).
- Sending user content to a third-party AI requires explicit, prior permission (Apple 5.1.2(i)): the assistant feature asks once and records consent.
- Web: no non-essential cookies or trackers by default, so no cookie banner. Adding analytics or ads means consent before they load.

## Age

- General apps: no child-directed content, no third-party ads. Answer the store age questionnaires honestly (UGC, messaging and AI raise the rating).
- Apps likely to have under-13 users, or creator/UGC apps needing age limits: add a neutral date-of-birth screen at sign-up, block under-13s (COPPA, amended rule in force since April 2026), and wire Apple's Declared Age Range API (Texas law since June 2026) and Google's age signals.

## Before each store submission

- [ ] Toolchain current (Xcode 26+/iOS 26 SDK now, iOS 27 SDK from April 2027; Android targetSdk 36).
- [ ] Privacy policy current, linked in app and consoles; labels / Data safety match it.
- [ ] Privacy manifest clean (no ITMS-91053); purpose strings specific; merged Android manifest has no stray permissions.
- [ ] Deletion: ≤3 taps, real, retention stated, web URL works, Apple token revoked, subscription warning if any.
- [ ] Export works; support email visible.
- [ ] Social login present → Sign in with Apple present.
- [ ] UGC: filter, report, block, queue staffed, contact published, AI output reportable.
- [ ] Payments follow the digital/physical rule.
- [ ] Age questionnaire re-answered if features changed.
- [ ] Play: Financial features declaration, content rating, target audience, ads, FGS declarations current. New personal Play accounts: 12 testers × 14 days closed test first.
- [ ] Review notes: a demo account that skips 2FA, and a plain description of moderation and payment flows.
