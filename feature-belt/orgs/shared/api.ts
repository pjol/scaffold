import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminOrg,
  AdminOrgDetail,
  AdminOrgListParams,
  CreateInviteLinkRequest,
  CreateInviteRequest,
  CreateOrgRequest,
  InvitePreview,
  MyInvite,
  Org,
  OrgInvite,
  OrgInviteLink,
  OrgMember,
  OrgRole,
  UpdateOrgRequest,
} from "./types";

type PageParams = { cursor?: string | undefined; limit?: number };

/** Organization endpoints: orgs, members, invites and links, accepting, and the staff list. */
export function orgsEndpoints(http: HttpClient) {
  const O = "/v1/orgs";
  const q = (p: PageParams) => ({ cursor: p.cursor, limit: p.limit });
  return {
    /** `GET /v1/orgs`: your orgs in the order you joined. */
    list: (params: PageParams = {}, o?: CallOptions) => http.get<Page<Org>>(O, { ...o, query: q(params) }),
    /** `POST /v1/orgs`: you become the owner. */
    create: (body: CreateOrgRequest, o?: CallOptions) => http.post<Org>(O, body, o),
    get: (orgId: string, o?: CallOptions) => http.get<Org>(`${O}/${seg(orgId)}`, o),
    /** Admins and owners. */
    update: (orgId: string, body: UpdateOrgRequest, o?: CallOptions) => http.patch<Org>(`${O}/${seg(orgId)}`, body, o),
    /** Owners. Deletes members, invites and links. */
    remove: (orgId: string, o?: CallOptions) => http.delete<void>(`${O}/${seg(orgId)}`, o),

    members: (orgId: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<OrgMember>>(`${O}/${seg(orgId)}/members`, { ...o, query: q(params) }),
    updateMember: (orgId: string, userId: string, role: OrgRole, o?: CallOptions) =>
      http.patch<OrgMember>(`${O}/${seg(orgId)}/members/${seg(userId)}`, { role }, o),
    /** Your own id leaves the org. */
    removeMember: (orgId: string, userId: string, o?: CallOptions) => http.delete<void>(`${O}/${seg(orgId)}/members/${seg(userId)}`, o),
    /** Owners: the member becomes an owner and you become an admin. */
    transfer: (orgId: string, userId: string, o?: CallOptions) => http.post<Org>(`${O}/${seg(orgId)}/transfer`, { user_id: userId }, o),

    invites: (orgId: string, params: PageParams & { status?: "pending" | "all" } = {}, o?: CallOptions) =>
      http.get<Page<OrgInvite>>(`${O}/${seg(orgId)}/invites`, { ...o, query: { ...q(params), status: params.status } }),
    createInvite: (orgId: string, body: CreateInviteRequest, o?: CallOptions) => http.post<OrgInvite>(`${O}/${seg(orgId)}/invites`, body, o),
    resendInvite: (orgId: string, inviteId: string, o?: CallOptions) => http.post<OrgInvite>(`${O}/${seg(orgId)}/invites/${seg(inviteId)}/resend`, undefined, o),
    revokeInvite: (orgId: string, inviteId: string, o?: CallOptions) => http.delete<void>(`${O}/${seg(orgId)}/invites/${seg(inviteId)}`, o),

    links: (orgId: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<OrgInviteLink>>(`${O}/${seg(orgId)}/links`, { ...o, query: q(params) }),
    createLink: (orgId: string, body: CreateInviteLinkRequest, o?: CallOptions) => http.post<OrgInviteLink>(`${O}/${seg(orgId)}/links`, body, o),
    revokeLink: (orgId: string, linkId: string, o?: CallOptions) => http.delete<void>(`${O}/${seg(orgId)}/links/${seg(linkId)}`, o),

    /** Public: what an invite link opens. */
    preview: (token: string, o?: CallOptions) => http.get<InvitePreview>(`${O}/invites/${seg(token)}`, { ...o, auth: false }),
    /** Join with an email invite (the email must match yours) or a link. */
    accept: (token: string, o?: CallOptions) => http.post<Org>(`${O}/invites/${seg(token)}/accept`, undefined, o),
    myInvites: (o?: CallOptions) => http.get<Page<MyInvite>>(`${O}/my-invites`, o),
    acceptMyInvite: (inviteId: string, o?: CallOptions) => http.post<Org>(`${O}/my-invites/${seg(inviteId)}/accept`, undefined, o),
    declineMyInvite: (inviteId: string, o?: CallOptions) => http.post<void>(`${O}/my-invites/${seg(inviteId)}/decline`, undefined, o),

    admin: {
      /** `GET /v1/admin/orgs?q=&member_id=`, newest first. */
      list: (params: AdminOrgListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminOrg>>("/v1/admin/orgs", { ...o, query: { q: params.q, member_id: params.member_id, cursor: params.cursor, limit: params.limit } }),
      get: (orgId: string, o?: CallOptions) => http.get<AdminOrgDetail>(`/v1/admin/orgs/${seg(orgId)}`, o),
    },
  };
}

export type OrgsEndpoints = ReturnType<typeof orgsEndpoints>;
