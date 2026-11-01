/** What someone may do with a resource; each level includes the ones before it. */
export type AccessLevel = "view" | "comment" | "edit" | "manage";

/** Levels lowest to highest. */
export const ACCESS_LEVELS: readonly AccessLevel[] = ["view", "comment", "edit", "manage"];

/** Whether `level` is at least `min` (`"none"`, null or undefined is never enough). */
export function accessLevelAtLeast(level: AccessLevel | "none" | null | undefined, min: AccessLevel): boolean {
  if (!level || level === "none") return false;
  return ACCESS_LEVELS.indexOf(level) >= ACCESS_LEVELS.indexOf(min);
}

/** Who a grant is for. */
export type AccessSubjectType = "user" | "team" | "org";

export interface AccessSubject {
  type: AccessSubjectType;
  id: string;
  name: string;
  /** Teams: the handle without "@". */
  handle: string | null;
  avatar_url: string | null;
  /** People, shown to org admins only. */
  email: string | null;
  /** Teams and the org. */
  member_count: number | null;
}

/** A resource access comes from. `title` and `link` are null when you can't open it. */
export interface AccessSource {
  kind: string;
  id: string;
  label: string;
  title: string | null;
  link: string | null;
}

export interface AccessGrant {
  id: string;
  subject: AccessSubject;
  level: AccessLevel;
  /** Null: set on this resource. Otherwise change it on that ancestor. */
  inherited_from: AccessSource | null;
  created_at: string;
}

/** Org-wide access: restricted, or everyone in the org at a level (set here or inherited). */
export interface AccessGeneral {
  access: "restricted" | "org";
  /** Effective org-wide level when `access` is `org`. */
  level: AccessLevel | null;
  /** The org-wide level set on this resource itself. */
  own_level: AccessLevel | null;
  inherited_from: AccessSource | null;
}

export interface AccessPerson {
  id: string;
  name: string;
  avatar_url: string | null;
}

/** A public link. `url` and `token` appear only in the response that creates it. */
export interface AccessLink {
  id: string;
  level: "view" | "comment";
  expires_at: string | null;
  created_by: AccessPerson | null;
  last_used_at: string | null;
  created_at: string;
  /** False while its creator lacks the public-sharing permission (or the kind's link permission). */
  working: boolean;
  url?: string;
  token?: string;
}

export interface AccessPublicLinks {
  /** The kind can have public links. */
  supported: boolean;
  /** Your org lets you make them (`access.share_public` and the kind's own link permission, e.g. `docs.publish`). */
  allowed: boolean;
  levels: ("view" | "comment")[];
  /** The org permission you lack to make one (null when `allowed`). */
  missing_permission: { key: string; title: string } | null;
  /** A public link on an ancestor also opens this resource. */
  inherited: AccessSource | null;
}

/**
 * `GET /v1/access/resources/{kind}/{id}`: everyone who can view sees their level, the
 * owner, general access and the parent; `grants` and `links` are filled only for people
 * who can manage it.
 */
export interface AccessSharing {
  kind: string;
  id: string;
  org_id: string | null;
  label: string;
  title: string;
  link: string;
  my_level: AccessLevel;
  is_owner: boolean;
  can_manage: boolean;
  /** You manage it only because you're an org admin. */
  admin_override: boolean;
  owner: AccessPerson | null;
  inherits: boolean;
  parent: AccessSource | null;
  general: AccessGeneral;
  grants: AccessGrant[];
  links: AccessLink[];
  /** Levels this kind lets you grant. */
  levels: AccessLevel[];
  public_links: AccessPublicLinks;
  /** You manage it and may make a public link to it now. */
  can_create_link: boolean;
}

export interface AccessSubjectRef {
  type: "user" | "team";
  id: string;
}

export interface AccessAddGrantsRequest {
  subjects: AccessSubjectRef[];
  level: AccessLevel;
  /** Default true: notify people newly added. */
  notify?: boolean;
}

export interface AccessGeneralRequest {
  access: "restricted" | "org";
  /** Required for `org`: view, comment or edit. */
  level?: Exclude<AccessLevel, "manage">;
}

export interface AccessCreateLinkRequest {
  level?: "view" | "comment";
  /** ISO time, within a year. Omit for a link that lasts until revoked. */
  expires_at?: string;
}

/** `GET /v1/access/links/{token}` (public): what a link opens. */
export interface AccessLinkTarget {
  kind: string;
  id: string;
  label: string;
  title: string;
  level: "view" | "comment";
  /** The kind's public page for this token. */
  path: string | null;
  expires_at: string | null;
}

/** A person or team a picker offers. */
export interface AccessDirectoryEntry {
  type: "user" | "team";
  id: string;
  name: string;
  handle: string | null;
  avatar_url: string | null;
  email: string | null;
  member_count: number | null;
}

// ---- teams ---------------------------------------------------------------------

export type AccessTeamRole = "lead" | "member";

export interface AccessTeam {
  id: string;
  org_id: string;
  name: string;
  handle: string;
  description: string;
  avatar_url: string | null;
  member_count: number;
  /** Your role on the team, or null. */
  my_role: AccessTeamRole | null;
  /** Rename, change the picture, delete. */
  can_manage: boolean;
  /** Add and remove members, make leads. */
  can_manage_members: boolean;
  created_at: string;
}

/** `GET /v1/access/orgs/{orgID}/teams` adds whether you may create teams. */
export interface AccessTeamPage {
  items: AccessTeam[];
  next_cursor: string | null;
  can_create: boolean;
}

export interface AccessTeamMember {
  user_id: string;
  display_name: string;
  /** Org admins only. */
  email: string | null;
  avatar_url: string | null;
  role: AccessTeamRole;
  joined_at: string;
}

export interface AccessCreateTeamRequest {
  name: string;
  /** Omitted: made from the name. */
  handle?: string;
  description?: string;
  /** Storage key from an `access_team_avatar` upload. */
  avatar_key?: string;
}

export interface AccessUpdateTeamRequest {
  name?: string;
  handle?: string;
  description?: string;
  /** `""` removes the picture. */
  avatar_key?: string;
}

/** The upload purpose for team pictures (public, images, 5 MB). */
export const ACCESS_TEAM_AVATAR_PURPOSE = "access_team_avatar";

// ---- permissions ---------------------------------------------------------------

export type AccessOrgRole = "owner" | "admin" | "member";

export interface AccessPolicySubject {
  type: "team" | "user";
  id: string;
  name: string;
  handle: string | null;
  avatar_url: string | null;
  added_at?: string;
}

/** One row of the permissions matrix (org admins). */
export interface AccessPermissionPolicy {
  key: string;
  title: string;
  info: string;
  default_roles: AccessOrgRole[];
  roles: AccessOrgRole[];
  teams: AccessPolicySubject[];
  users: AccessPolicySubject[];
  /** Differs from the registered defaults. */
  customized: boolean;
  updated_at: string | null;
}

export interface AccessSetPermissionRequest {
  roles: AccessOrgRole[];
  team_ids: string[];
  user_ids: string[];
}

/** `GET /v1/access/orgs/{orgID}/my-permissions`. */
export interface AccessMyPermissions {
  org_id: string;
  /** Key → whether you hold it (for gating). */
  permissions: Record<string, boolean>;
  /** The same, with titles, sorted by key (for listing). */
  items: { key: string; title: string; info: string; allowed: boolean }[];
}

/** Built-in permission keys. */
export const ACCESS_PERMISSION_SHARE_PUBLIC = "access.share_public";
export const ACCESS_PERMISSION_MANAGE_TEAMS = "access.manage_teams";

declare module "../types" {
  interface RealtimeEventMap {
    "access.changed": { kind: string; id: string; org_id: string | null };
    "access.teams_changed": { org_id: string; team_id: string };
    "access.team_members_changed": { org_id: string; team_id: string; user_id?: string };
    "access.permissions_changed": { org_id: string; key: string };
  }
}
