import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AccessAddGrantsRequest,
  AccessCreateLinkRequest,
  AccessCreateTeamRequest,
  AccessDirectoryEntry,
  AccessGeneralRequest,
  AccessLevel,
  AccessLink,
  AccessLinkTarget,
  AccessMyPermissions,
  AccessPermissionPolicy,
  AccessSetPermissionRequest,
  AccessSharing,
  AccessTeam,
  AccessTeamMember,
  AccessTeamPage,
  AccessTeamRole,
  AccessUpdateTeamRequest,
} from "./types";

type PageParams = { cursor?: string | undefined; limit?: number };

/** Access endpoints: sharing over registered kinds, public links, teams, directory, permissions. */
export function accessEndpoints(http: HttpClient) {
  const A = "/v1/access";
  const res = (kind: string, id: string) => `${A}/resources/${seg(kind)}/${seg(id)}`;
  return {
    // ---- sharing
    /** Your level, owner, general access, parent; grants and links when you manage it. */
    sharing: (kind: string, id: string, o?: CallOptions) => http.get<AccessSharing>(res(kind, id), o),
    setGeneral: (kind: string, id: string, body: AccessGeneralRequest, o?: CallOptions) => http.put<AccessSharing>(`${res(kind, id)}/general`, body, o),
    setInherit: (kind: string, id: string, inherits: boolean, o?: CallOptions) => http.put<AccessSharing>(`${res(kind, id)}/inherit`, { inherits }, o),
    /** Adds people and teams, or changes their level. Notifies the newly added unless `notify: false`. */
    addGrants: (kind: string, id: string, body: AccessAddGrantsRequest, o?: CallOptions) => http.post<AccessSharing>(`${res(kind, id)}/grants`, body, o),
    updateGrant: (kind: string, id: string, grantId: string, level: AccessLevel, o?: CallOptions) =>
      http.patch<AccessSharing>(`${res(kind, id)}/grants/${seg(grantId)}`, { level }, o),
    /** Managers remove anyone; anyone removes their own grant (resolves to undefined if that ends their access). */
    removeGrant: (kind: string, id: string, grantId: string, o?: CallOptions) => http.delete<AccessSharing | undefined>(`${res(kind, id)}/grants/${seg(grantId)}`, o),
    transferOwner: (kind: string, id: string, userId: string, o?: CallOptions) => http.put<AccessSharing>(`${res(kind, id)}/owner`, { user_id: userId }, o),
    /** Resolves to the link with `url` and `token` (shown only now). */
    createLink: (kind: string, id: string, body: AccessCreateLinkRequest, o?: CallOptions) => http.post<AccessLink>(`${res(kind, id)}/links`, body, o),
    revokeLink: (kind: string, id: string, linkId: string, o?: CallOptions) => http.delete<void>(`${res(kind, id)}/links/${seg(linkId)}`, o),
    /** Public: what a link opens. */
    resolveLink: (token: string, o?: CallOptions) => http.get<AccessLinkTarget>(`${A}/links/${seg(token)}`, { ...o, auth: false }),

    // ---- directory
    /** Teams then people in an org matching `q` ("@des" matches team handles only). */
    directory: (orgId: string, params: { q?: string; types?: "user" | "team" | "user,team"; limit?: number } = {}, o?: CallOptions) =>
      http.get<{ items: AccessDirectoryEntry[] }>(`${A}/orgs/${seg(orgId)}/directory`, { ...o, query: { q: params.q, types: params.types, limit: params.limit } }),

    // ---- teams
    teams: (orgId: string, params: PageParams & { q?: string; mine?: boolean } = {}, o?: CallOptions) =>
      http.get<AccessTeamPage>(`${A}/orgs/${seg(orgId)}/teams`, {
        ...o,
        query: { cursor: params.cursor, limit: params.limit, q: params.q, mine: params.mine ? "true" : undefined },
      }),
    createTeam: (orgId: string, body: AccessCreateTeamRequest, o?: CallOptions) => http.post<AccessTeam>(`${A}/orgs/${seg(orgId)}/teams`, body, o),
    team: (teamId: string, o?: CallOptions) => http.get<AccessTeam>(`${A}/teams/${seg(teamId)}`, o),
    updateTeam: (teamId: string, body: AccessUpdateTeamRequest, o?: CallOptions) => http.patch<AccessTeam>(`${A}/teams/${seg(teamId)}`, body, o),
    deleteTeam: (teamId: string, o?: CallOptions) => http.delete<void>(`${A}/teams/${seg(teamId)}`, o),
    teamMembers: (teamId: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<AccessTeamMember>>(`${A}/teams/${seg(teamId)}/members`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    addTeamMembers: (teamId: string, userIds: string[], role: AccessTeamRole = "member", o?: CallOptions) =>
      http.post<{ added: number; items: AccessTeamMember[] }>(`${A}/teams/${seg(teamId)}/members`, { user_ids: userIds, role }, o),
    updateTeamMember: (teamId: string, userId: string, role: AccessTeamRole, o?: CallOptions) =>
      http.patch<AccessTeamMember>(`${A}/teams/${seg(teamId)}/members/${seg(userId)}`, { role }, o),
    /** Your own id leaves the team. */
    removeTeamMember: (teamId: string, userId: string, o?: CallOptions) => http.delete<void>(`${A}/teams/${seg(teamId)}/members/${seg(userId)}`, o),

    // ---- permissions
    myPermissions: (orgId: string, o?: CallOptions) => http.get<AccessMyPermissions>(`${A}/orgs/${seg(orgId)}/my-permissions`, o),
    /** Org admins: every permission with roles, teams and people. */
    permissions: (orgId: string, o?: CallOptions) => http.get<{ items: AccessPermissionPolicy[] }>(`${A}/orgs/${seg(orgId)}/permissions`, o),
    setPermission: (orgId: string, key: string, body: AccessSetPermissionRequest, o?: CallOptions) =>
      http.put<AccessPermissionPolicy>(`${A}/orgs/${seg(orgId)}/permissions/${seg(key)}`, body, o),
    /** Back to the defaults the feature registered. */
    resetPermission: (orgId: string, key: string, o?: CallOptions) => http.delete<AccessPermissionPolicy>(`${A}/orgs/${seg(orgId)}/permissions/${seg(key)}`, o),
  };
}

export type AccessEndpoints = ReturnType<typeof accessEndpoints>;
