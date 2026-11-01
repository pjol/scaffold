// Development sign-ins created by `backend/cmd/seed`, mirroring
// `backend/internal/auth/dev.go` (keep the two in step). With the backend on
// AUTH_DEV_BYPASS=true (APP_ENV=development only) any of these signs in with
// `Authorization: Bearer dev:<subject>:<email>` (or `?token=` on the WebSocket).
// The .test domain is reserved, so dev mail never reaches a real inbox.

import type { AdminRole } from "./types";

export interface DevIdentity {
  /** Stored as `users.auth_subject`. */
  subject: string;
  email: string;
  name: string;
  admin_role: AdminRole | null;
  /** Shown as a tooltip in dev sign-in pickers. */
  info: string;
}

export const DEV_IDENTITIES: readonly DevIdentity[] = [
  { subject: "dev_admin", email: "admin@scaffoldapp.test", name: "Avery Admin", admin_role: "admin", info: "Full admin console access." },
  { subject: "dev_sam", email: "sam@scaffoldapp.test", name: "Sam Rivera", admin_role: null, info: "Member account." },
  { subject: "dev_jordan", email: "jordan@scaffoldapp.test", name: "Jordan Lee", admin_role: null, info: "Member account." },
  { subject: "dev_priya", email: "priya@scaffoldapp.test", name: "Priya Shah", admin_role: null, info: "Member account." },
];

/** The bearer token (without "Bearer ") for a dev identity: `dev:<subject>:<email>`. Matches Go `auth.DevToken`. */
export function devToken(identity: Pick<DevIdentity, "subject" | "email">): string {
  return `dev:${identity.subject}:${identity.email}`;
}
