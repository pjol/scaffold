/** Roles in an organization, lowest to highest. */
export type OrgRole = "member" | "admin" | "owner";

export const ORG_ROLES: readonly OrgRole[] = ["member", "admin", "owner"];

/** Whether `role` is at least `min` ("" or undefined is never enough). */
export function orgRoleAtLeast(role: OrgRole | "" | null | undefined, min: OrgRole): boolean {
  if (!role) return false;
  return ORG_ROLES.indexOf(role) >= ORG_ROLES.indexOf(min);
}

/** An organization as one of its members sees it. */
export interface Org {
  id: string;
  name: string;
  slug: string;
  avatar_url: string | null;
  /** The caller's role. */
  role: OrgRole;
  member_count: number;
  created_at: string;
  joined_at: string;
}

export interface CreateOrgRequest {
  name: string;
  /** Omitted: made from the name. */
  slug?: string;
  /** Storage key from an `org_avatar` upload. */
  avatar_key?: string;
}

export interface UpdateOrgRequest {
  name?: string;
  slug?: string;
  /** `""` removes the picture. */
  avatar_key?: string;
}

/** One person in an org. `email` is null unless the caller is an admin or owner. */
export interface OrgMember {
  user_id: string;
  display_name: string;
  email: string | null;
  avatar_url: string | null;
  role: OrgRole;
  joined_at: string;
}

export type InviteStatus = "pending" | "accepted" | "declined" | "revoked" | "expired";

/** An email invite as org admins see it. */
export interface OrgInvite {
  id: string;
  org_id: string;
  email: string;
  role: Exclude<OrgRole, "owner">;
  status: InviteStatus;
  invited_by_name: string | null;
  sent_count: number;
  last_sent_at: string;
  expires_at: string;
  created_at: string;
}

export interface CreateInviteRequest {
  email: string;
  role?: Exclude<OrgRole, "owner">;
}

/** A shareable link. `url` and `token` appear only in the response that creates it. */
export interface OrgInviteLink {
  id: string;
  org_id: string;
  role: "member";
  max_uses: number | null;
  uses: number;
  expires_at: string | null;
  revoked_at: string | null;
  created_by_name: string | null;
  created_at: string;
  active: boolean;
  url?: string;
  token?: string;
}

export interface CreateInviteLinkRequest {
  max_uses?: number;
  expires_in_days?: number;
}

export interface OrgRef {
  id: string;
  name: string;
  slug: string;
  avatar_url: string | null;
}

export type InvitePreviewStatus = "valid" | "expired" | "revoked" | "used" | "accepted" | "declined";

/** `GET /v1/orgs/invites/{token}` (public). */
export interface InvitePreview {
  kind: "email" | "link";
  status: InvitePreviewStatus;
  org: OrgRef;
  role: Exclude<OrgRole, "owner">;
  /** Masked address an email invite was sent to ("s***@example.com"). */
  email_hint: string | null;
  invited_by_name: string | null;
  expires_at: string | null;
}

/** A pending invite addressed to the caller's email. */
export interface MyInvite {
  id: string;
  org: OrgRef;
  role: Exclude<OrgRole, "owner">;
  invited_by_name: string | null;
  expires_at: string;
  created_at: string;
}

/** An org as staff see it. */
export interface AdminOrg {
  id: string;
  name: string;
  slug: string;
  avatar_url: string | null;
  member_count: number;
  pending_invites: number;
  created_at: string;
}

export interface AdminOrgDetail extends AdminOrg {
  /** Owners and admins, emails included. */
  leaders: OrgMember[];
  /** Claimed email domains. */
  domains: AdminOrgDomain[];
}

/** A claimed email domain as staff see it. */
export interface AdminOrgDomain {
  id: string;
  domain: string;
  policy: OrgDomainPolicy;
  verify_method: OrgDomainVerifyMethod | null;
  verified_at: string | null;
  created_at: string;
}

export interface AdminOrgListParams {
  q?: string;
  member_id?: string;
  limit?: number;
  cursor?: string;
}

// ---- email domains -------------------------------------------------------------

/** What a verified domain lets people with an email on it do: join on their own, or ask an admin. */
export type OrgDomainPolicy = "auto_join" | "request";
export type OrgDomainVerifyMethod = "email" | "dns";
/** `taken`: another organization verified this domain with its DNS record. One it holds only through an admin's email stays `pending` here: this org's DNS record moves it over. */
export type OrgDomainStatus = "verified" | "pending" | "taken";

/** An email domain an org claims, as its admins see it. */
export interface OrgDomain {
  id: string;
  org_id: string;
  domain: string;
  status: OrgDomainStatus;
  policy: OrgDomainPolicy;
  /** What people join as. */
  role: "member";
  verify_method: OrgDomainVerifyMethod | null;
  verified_at: string | null;
  verified_by_name: string | null;
  /** The DNS TXT record that proves the domain: name and value. */
  txt_name: string;
  txt_value: string;
  last_checked_at: string | null;
  /** Why the last DNS check failed (user-facing copy). */
  last_check_error: string | null;
  added_by_name: string | null;
  created_at: string;
}

export interface AddOrgDomainRequest {
  /** "example.com" ("@example.com" and URLs are accepted). */
  domain: string;
  /** Default `request`. */
  policy?: OrgDomainPolicy;
}

/** `join`: join now · `request`: ask an admin · `requested`: waiting for an admin. */
export type JoinableOrgAction = "join" | "request" | "requested";

/** An org your verified email domain lets you join or ask to join, or one you already asked. */
export interface JoinableOrg {
  org: OrgRef;
  member_count: number;
  domain: string;
  action: JoinableOrgAction;
  request_id: string | null;
  requested_at: string | null;
}

/** `POST /v1/orgs/auto-join`: orgs you joined just now. */
export interface OrgsAutoJoined {
  joined: Org[];
}

/** Your own pending request. */
export interface MyOrgJoinRequest {
  id: string;
  org: OrgRef;
  domain: string;
  status: "pending";
  created_at: string;
}

/** A pending request as org admins see it. */
export interface OrgJoinRequest {
  id: string;
  org_id: string;
  user_id: string;
  display_name: string;
  email: string;
  avatar_url: string | null;
  domain: string;
  created_at: string;
}

/** The upload purpose for org pictures (public, images, 5 MB). */
export const ORG_AVATAR_PURPOSE = "org_avatar";

declare module "../types" {
  interface RealtimeEventMap {
    "orgs.updated": { org_id: string };
    "orgs.deleted": { org_id: string };
    "orgs.member_joined": { org_id: string; user_id: string; role: OrgRole };
    "orgs.member_updated": { org_id: string; user_id: string; role: OrgRole };
    "orgs.member_removed": { org_id: string; user_id: string };
    "orgs.membership_changed": { org_id: string };
    "orgs.domains_changed": { org_id: string };
    "orgs.join_requests_changed": { org_id: string };
  }
}
