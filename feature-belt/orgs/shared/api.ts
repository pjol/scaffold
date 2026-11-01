import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AddOrgDomainRequest,
  AdminOrg,
  AdminOrgDetail,
  AdminOrgListParams,
  CreateInviteLinkRequest,
  CreateInviteRequest,
  CreateOrgRequest,
  InvitePreview,
  JoinableOrg,
  MyInvite,
  MyOrgJoinRequest,
  Org,
  OrgDomain,
  OrgDomainPolicy,
  OrgInvite,
  OrgInviteLink,
  OrgJoinRequest,
  OrgMember,
  OrgRole,
  OrgsAutoJoined,
  UpdateOrgRequest,
} from "./types";

type PageParams = { cursor?: string | undefined; limit?: number };

/** Organization endpoints: orgs, members, invites and links, accepting, email domains and join requests, and the staff list. */
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

    /** Admins: claimed email domains (one page; 20 at most). */
    domains: (orgId: string, o?: CallOptions) => http.get<Page<OrgDomain>>(`${O}/${seg(orgId)}/domains`, o),
    /** Admins. Verified at once when your own email is on the domain; otherwise add the TXT record and verify. */
    addDomain: (orgId: string, body: AddOrgDomainRequest, o?: CallOptions) => http.post<OrgDomain>(`${O}/${seg(orgId)}/domains`, body, o),
    /** Admins: checks your email, then DNS (422 `verification_failed` with the reason). */
    verifyDomain: (orgId: string, domainId: string, o?: CallOptions) => http.post<OrgDomain>(`${O}/${seg(orgId)}/domains/${seg(domainId)}/verify`, undefined, o),
    updateDomain: (orgId: string, domainId: string, policy: OrgDomainPolicy, o?: CallOptions) =>
      http.patch<OrgDomain>(`${O}/${seg(orgId)}/domains/${seg(domainId)}`, { policy }, o),
    removeDomain: (orgId: string, domainId: string, o?: CallOptions) => http.delete<void>(`${O}/${seg(orgId)}/domains/${seg(domainId)}`, o),

    /** Admins: pending requests to join, oldest first. */
    joinRequests: (orgId: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<OrgJoinRequest>>(`${O}/${seg(orgId)}/join-requests`, { ...o, query: q(params) }),
    approveJoinRequest: (orgId: string, requestId: string, o?: CallOptions) =>
      http.post<OrgMember>(`${O}/${seg(orgId)}/join-requests/${seg(requestId)}/approve`, undefined, o),
    denyJoinRequest: (orgId: string, requestId: string, o?: CallOptions) => http.post<void>(`${O}/${seg(orgId)}/join-requests/${seg(requestId)}/deny`, undefined, o),

    /** Orgs your verified email domain lets you join or ask to join. */
    joinable: (o?: CallOptions) => http.get<Page<JoinableOrg>>(`${O}/joinable`, o),
    /** Joins every auto-join org your email qualifies for (not ones you left). Idempotent. */
    autoJoin: (o?: CallOptions) => http.post<OrgsAutoJoined>(`${O}/auto-join`, undefined, o),
    /** Join through an auto-join domain. */
    joinByDomain: (orgId: string, o?: CallOptions) => http.post<Org>(`${O}/${seg(orgId)}/join`, undefined, o),
    /** Ask to join through a request domain (returns the pending request if there is one). */
    requestToJoin: (orgId: string, o?: CallOptions) => http.post<MyOrgJoinRequest>(`${O}/${seg(orgId)}/join-requests`, undefined, o),
    cancelJoinRequest: (requestId: string, o?: CallOptions) => http.delete<void>(`${O}/my-join-requests/${seg(requestId)}`, o),

    admin: {
      /** `GET /v1/admin/orgs?q=&member_id=`, newest first. */
      list: (params: AdminOrgListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminOrg>>("/v1/admin/orgs", { ...o, query: { q: params.q, member_id: params.member_id, cursor: params.cursor, limit: params.limit } }),
      get: (orgId: string, o?: CallOptions) => http.get<AdminOrgDetail>(`/v1/admin/orgs/${seg(orgId)}`, o),
      /** Admin role: free a disputed domain (audited). */
      releaseDomain: (orgId: string, domainId: string, o?: CallOptions) => http.delete<void>(`/v1/admin/orgs/${seg(orgId)}/domains/${seg(domainId)}`, o),
    },
  };
}

export type OrgsEndpoints = ReturnType<typeof orgsEndpoints>;
