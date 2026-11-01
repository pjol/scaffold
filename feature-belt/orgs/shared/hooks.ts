"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { orgsEndpoints } from "./api";
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

/** Query keys. `orgsKeys.all()` invalidates everything this feature caches. */
export const orgsKeys = {
  all: () => ["orgs"] as const,
  list: () => ["orgs", "list"] as const,
  detail: (id: string) => ["orgs", "detail", id] as const,
  scoped: (id: string) => ["orgs", "org", id] as const,
  members: (id: string) => ["orgs", "org", id, "members"] as const,
  invites: (id: string, status = "pending") => ["orgs", "org", id, "invites", status] as const,
  links: (id: string) => ["orgs", "org", id, "links"] as const,
  myInvites: () => ["orgs", "my-invites"] as const,
  domains: (id: string) => ["orgs", "org", id, "domains"] as const,
  joinRequests: (id: string) => ["orgs", "org", id, "join-requests"] as const,
  joinable: () => ["orgs", "joinable"] as const,
  preview: (token: string) => ["orgs", "preview", token] as const,
  admin: {
    all: () => ["orgs", "admin"] as const,
    list: (params: Record<string, unknown> = {}) => ["orgs", "admin", "list", params] as const,
    detail: (id: string) => ["orgs", "admin", "detail", id] as const,
  },
};

// ---- active org --------------------------------------------------------------

/** Where the chosen org id is kept between launches (web: localStorage; mobile: user storage). */
export interface ActiveOrgStorage {
  load: () => string | null | Promise<string | null>;
  save: (id: string | null) => void | Promise<void>;
}

let activeId: string | null = null;
let storage: ActiveOrgStorage | null = null;
const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

/** Call once at startup with the platform's storage; the saved choice is restored. */
export function configureActiveOrgStorage(next: ActiveOrgStorage): void {
  storage = next;
  void Promise.resolve(next.load())
    .then((id) => {
      if (id && activeId === null) {
        activeId = id;
        emit();
      }
    })
    .catch(() => undefined);
}

/** Choose the active org (null clears it). Saved through the configured storage. */
export function setActiveOrgId(id: string | null): void {
  if (activeId === id) return;
  activeId = id;
  emit();
  void Promise.resolve(storage?.save(id)).catch(() => undefined);
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** The stored active org id (may name an org you have since left; prefer `useActiveOrg`). */
export function useActiveOrgId(): string | null {
  return useSyncExternalStore(
    subscribe,
    () => activeId,
    () => null,
  );
}

/**
 * The org someone works in when they haven't chosen one (or left the one they chose): the
 * largest they belong to, then the one they joined most recently. A side group created
 * later never displaces the company everyone is in.
 */
export function defaultOrg(orgs: readonly Org[]): Org | null {
  let best: Org | null = null;
  let bestJoined = 0;
  for (const o of orgs) {
    const joined = Date.parse(o.joined_at) || 0;
    if (!best || o.member_count > best.member_count || (o.member_count === best.member_count && joined > bestJoined)) {
      best = o;
      bestJoined = joined;
    }
  }
  return best;
}

/**
 * The org the user is working in: the stored choice when they still belong to it, else
 * `defaultOrg` (largest, then most recently joined), else null. Features scope their
 * screens with `org?.id`.
 */
export function useActiveOrg(): { org: Org | null; orgs: Org[]; isPending: boolean; setActive: (id: string | null) => void } {
  const id = useActiveOrgId();
  const list = useOrgs();
  const orgs = list.data?.items ?? [];
  const org = orgs.find((o) => o.id === id) ?? defaultOrg(orgs);
  return { org, orgs, isPending: list.isPending, setActive: setActiveOrgId };
}

// ---- shell switcher ------------------------------------------------------------

let shellSwitcher = false;
const shellListeners = new Set<() => void>();

/**
 * Tell features the app shell has its own org switcher (a top-bar picker), so screens
 * drop their in-page one (`useOrgPickerVisible`). Call once at startup in stitching.
 */
export function setOrgsShellSwitcher(on: boolean): void {
  if (shellSwitcher === on) return;
  shellSwitcher = on;
  for (const l of shellListeners) l();
}

/**
 * Whether a screen should show its own org picker: false when the shell has one
 * (`setOrgsShellSwitcher(true)`). Screens still decide when one helps (more than 1 org).
 */
export function useOrgPickerVisible(): boolean {
  const shell = useSyncExternalStore(
    (fn) => {
      shellListeners.add(fn);
      return () => {
        shellListeners.delete(fn);
      };
    },
    () => shellSwitcher,
    () => shellSwitcher,
  );
  return !shell;
}

// ---- orgs --------------------------------------------------------------------

/** Your orgs (first 100 pages' worth loaded on demand; most people belong to a few). */
export function useOrgs(options?: InfiniteOverrides<Org>): PagedQueryResult<Org> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<Org>(orgsKeys.list(), (cursor, signal) => o.list({ cursor, limit: 100 }, { signal }), options);
}

export function useOrg(orgId: string | null | undefined, options?: QueryOverrides<Org>): UseQueryResult<Org, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.detail(orgId ?? ""), ({ signal }) => o.get(orgId ?? "", { signal }), options, !!orgId);
}

function putOrg(qc: QueryClient, org: Org) {
  qc.setQueryData(orgsKeys.detail(org.id), org);
  qc.setQueryData<InfiniteData<Page<Org>>>(orgsKeys.list(), (data) =>
    data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map((x) => (x.id === org.id ? org : x)) })) } : data,
  );
}

function dropOrg(qc: QueryClient, orgId: string) {
  qc.removeQueries({ queryKey: orgsKeys.detail(orgId) });
  qc.removeQueries({ queryKey: orgsKeys.scoped(orgId) });
  qc.setQueryData<InfiniteData<Page<Org>>>(orgsKeys.list(), (data) =>
    data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((x) => x.id !== orgId) })) } : data,
  );
}

export function useCreateOrg(): UseMutationResult<Org, ApiError, CreateOrgRequest> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, CreateOrgRequest>({
    mutationFn: (body) => o.create(body),
    onSuccess: (org) => qc.setQueryData(orgsKeys.detail(org.id), org),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.list() }),
  });
}

export function useUpdateOrg(orgId: string): UseMutationResult<Org, ApiError, UpdateOrgRequest> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, UpdateOrgRequest>({ mutationFn: (body) => o.update(orgId, body), onSuccess: (org) => putOrg(qc, org) });
}

export function useDeleteOrg(): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (orgId) => o.remove(orgId), onSuccess: (_d, orgId) => dropOrg(qc, orgId) });
}

// ---- members -----------------------------------------------------------------

export function useOrgMembers(orgId: string | null | undefined, options?: InfiniteOverrides<OrgMember>): PagedQueryResult<OrgMember> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<OrgMember>(orgsKeys.members(orgId ?? ""), (cursor, signal) => o.members(orgId ?? "", { cursor }, { signal }), options, !!orgId);
}

/** Change a member's role. Owner changes need an owner; the last owner stays. */
export function useUpdateMember(orgId: string): UseMutationResult<OrgMember, ApiError, { userId: string; role: OrgRole }> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgMember, ApiError, { userId: string; role: OrgRole }>({
    mutationFn: ({ userId, role }) => o.updateMember(orgId, userId, role),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: orgsKeys.members(orgId) }), qc.invalidateQueries({ queryKey: orgsKeys.detail(orgId) }), qc.invalidateQueries({ queryKey: orgsKeys.list() })]),
  });
}

/** Remove a member, or leave with your own id (`{ leave: true }` drops the org from your caches). */
export function useRemoveOrgMember(orgId: string): UseMutationResult<void, ApiError, { userId: string; leave?: boolean }> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { userId: string; leave?: boolean }>({
    mutationFn: ({ userId }) => o.removeMember(orgId, userId),
    onMutate: ({ userId, leave }) => {
      if (leave) return;
      qc.setQueryData<InfiniteData<Page<OrgMember>>>(orgsKeys.members(orgId), (data) =>
        data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((m) => m.user_id !== userId) })) } : data,
      );
    },
    onSuccess: (_d, { leave }) => {
      if (leave) dropOrg(qc, orgId);
    },
    onSettled: (_d, _e, { leave }) => (leave ? qc.invalidateQueries({ queryKey: orgsKeys.list() }) : qc.invalidateQueries({ queryKey: orgsKeys.scoped(orgId) })),
  });
}

export function useTransferOwnership(orgId: string): UseMutationResult<Org, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, string>({
    mutationFn: (userId) => o.transfer(orgId, userId),
    onSuccess: (org) => putOrg(qc, org),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.members(orgId) }),
  });
}

// ---- invites and links -------------------------------------------------------

export function useOrgInvites(orgId: string | null | undefined, status: "pending" | "all" = "pending", options?: InfiniteOverrides<OrgInvite>): PagedQueryResult<OrgInvite> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<OrgInvite>(orgsKeys.invites(orgId ?? "", status), (cursor, signal) => o.invites(orgId ?? "", { cursor, status }, { signal }), options, !!orgId);
}

function invalidateInvites(qc: QueryClient, orgId: string) {
  return qc.invalidateQueries({ queryKey: ["orgs", "org", orgId, "invites"] });
}

export function useCreateInvite(orgId: string): UseMutationResult<OrgInvite, ApiError, CreateInviteRequest> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgInvite, ApiError, CreateInviteRequest>({ mutationFn: (body) => o.createInvite(orgId, body), onSettled: () => invalidateInvites(qc, orgId) });
}

export function useResendInvite(orgId: string): UseMutationResult<OrgInvite, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgInvite, ApiError, string>({ mutationFn: (inviteId) => o.resendInvite(orgId, inviteId), onSettled: () => invalidateInvites(qc, orgId) });
}

export function useRevokeInvite(orgId: string): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (inviteId) => o.revokeInvite(orgId, inviteId),
    onMutate: (inviteId) => {
      qc.setQueryData<InfiniteData<Page<OrgInvite>>>(orgsKeys.invites(orgId), (data) =>
        data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((i) => i.id !== inviteId) })) } : data,
      );
    },
    onSettled: () => invalidateInvites(qc, orgId),
  });
}

export function useOrgLinks(orgId: string | null | undefined, options?: InfiniteOverrides<OrgInviteLink>): PagedQueryResult<OrgInviteLink> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<OrgInviteLink>(orgsKeys.links(orgId ?? ""), (cursor, signal) => o.links(orgId ?? "", { cursor }, { signal }), options, !!orgId);
}

/** Resolves to the new link with `url` and `token` (shown only now). */
export function useCreateLink(orgId: string): UseMutationResult<OrgInviteLink, ApiError, CreateInviteLinkRequest> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgInviteLink, ApiError, CreateInviteLinkRequest>({ mutationFn: (body) => o.createLink(orgId, body), onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.links(orgId) }) });
}

export function useRevokeLink(orgId: string): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (linkId) => o.revokeLink(orgId, linkId), onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.links(orgId) }) });
}

// ---- accepting ---------------------------------------------------------------

/** Public preview of an invite link (works signed out). */
export function useInvitePreview(token: string | null | undefined, options?: QueryOverrides<InvitePreview>): UseQueryResult<InvitePreview, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.preview(token ?? ""), ({ signal }) => o.preview(token ?? "", { signal }), { retry: false, ...options }, !!token);
}

function afterJoin(qc: QueryClient, org: Org) {
  qc.setQueryData(orgsKeys.detail(org.id), org);
  return Promise.all([
    qc.invalidateQueries({ queryKey: orgsKeys.list() }),
    qc.invalidateQueries({ queryKey: orgsKeys.myInvites() }),
    qc.invalidateQueries({ queryKey: orgsKeys.joinable() }),
  ]);
}

/** Accept by token (email invite or link). Resolves to the org you joined. */
export function useAcceptInvite(): UseMutationResult<Org, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, string>({
    mutationFn: (token) => o.accept(token),
    onSuccess: (org) => afterJoin(qc, org),
    onSettled: (_d, _e, token) => qc.invalidateQueries({ queryKey: orgsKeys.preview(token) }),
  });
}

/** Pending invites to your email. */
export function useMyInvites(options?: QueryOverrides<Page<MyInvite>>): UseQueryResult<Page<MyInvite>, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.myInvites(), ({ signal }) => o.myInvites({ signal }), options);
}

/** Number of pending invites to your email (a cheap cache read for badges). */
export function useMyInviteCount(): number | undefined {
  const q = useMyInvites({ staleTime: 60_000 });
  const n = q.data?.items.length ?? 0;
  return n > 0 ? n : undefined;
}

export function useAcceptMyInvite(): UseMutationResult<Org, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, string>({ mutationFn: (inviteId) => o.acceptMyInvite(inviteId), onSuccess: (org) => afterJoin(qc, org) });
}

export function useDeclineMyInvite(): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (inviteId) => o.declineMyInvite(inviteId),
    onMutate: (inviteId) => {
      qc.setQueryData<Page<MyInvite>>(orgsKeys.myInvites(), (data) => (data ? { ...data, items: data.items.filter((i) => i.id !== inviteId) } : data));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.myInvites() }),
  });
}

// ---- email domains (admins) ----------------------------------------------------

/** The org's claimed email domains (admins). */
export function useOrgDomains(orgId: string | null | undefined, options?: QueryOverrides<Page<OrgDomain>>): UseQueryResult<Page<OrgDomain>, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.domains(orgId ?? ""), ({ signal }) => o.domains(orgId ?? "", { signal }), options, !!orgId);
}

function putDomain(qc: QueryClient, d: OrgDomain) {
  qc.setQueryData<Page<OrgDomain>>(orgsKeys.domains(d.org_id), (data) =>
    data ? { ...data, items: data.items.some((x) => x.id === d.id) ? data.items.map((x) => (x.id === d.id ? d : x)) : [...data.items, d] } : data,
  );
}

/** Add a domain. Resolves verified when your own email is on it. */
export function useAddOrgDomain(orgId: string): UseMutationResult<OrgDomain, ApiError, AddOrgDomainRequest> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgDomain, ApiError, AddOrgDomainRequest>({
    mutationFn: (body) => o.addDomain(orgId, body),
    onSuccess: (d) => putDomain(qc, d),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.domains(orgId) }),
  });
}

/** Check a domain now (your email, then its TXT record). A failed check rejects with `verification_failed`. */
export function useVerifyOrgDomain(orgId: string): UseMutationResult<OrgDomain, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgDomain, ApiError, string>({
    mutationFn: (domainId) => o.verifyDomain(orgId, domainId),
    onSuccess: (d) => putDomain(qc, d),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.domains(orgId) }),
  });
}

/** Change a domain's policy (optimistic). */
export function useUpdateOrgDomain(orgId: string): UseMutationResult<OrgDomain, ApiError, { domainId: string; policy: OrgDomainPolicy }, { previous: Page<OrgDomain> | undefined }> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  const key = orgsKeys.domains(orgId);
  return useMutation<OrgDomain, ApiError, { domainId: string; policy: OrgDomainPolicy }, { previous: Page<OrgDomain> | undefined }>({
    mutationFn: ({ domainId, policy }) => o.updateDomain(orgId, domainId, policy),
    onMutate: async ({ domainId, policy }) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<Page<OrgDomain>>(key);
      qc.setQueryData<Page<OrgDomain>>(key, (data) => (data ? { ...data, items: data.items.map((d) => (d.id === domainId ? { ...d, policy } : d)) } : data));
      return { previous };
    },
    onError: (_e, _v, ctx) => qc.setQueryData(key, ctx?.previous),
    onSuccess: (d) => putDomain(qc, d),
  });
}

export function useRemoveOrgDomain(orgId: string): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (domainId) => o.removeDomain(orgId, domainId),
    onSuccess: (_d, domainId) =>
      qc.setQueryData<Page<OrgDomain>>(orgsKeys.domains(orgId), (data) => (data ? { ...data, items: data.items.filter((d) => d.id !== domainId) } : data)),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.domains(orgId) }),
  });
}

// ---- join requests (admins) ----------------------------------------------------

/** Pending requests to join, oldest first (admins). */
export function useOrgJoinRequests(orgId: string | null | undefined, options?: InfiniteOverrides<OrgJoinRequest>): PagedQueryResult<OrgJoinRequest> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<OrgJoinRequest>(orgsKeys.joinRequests(orgId ?? ""), (cursor, signal) => o.joinRequests(orgId ?? "", { cursor }, { signal }), options, !!orgId);
}

function dropJoinRequest(qc: QueryClient, orgId: string, requestId: string) {
  qc.setQueryData<InfiniteData<Page<OrgJoinRequest>>>(orgsKeys.joinRequests(orgId), (data) =>
    data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((j) => j.id !== requestId) })) } : data,
  );
}

/** Approve: they join as a member. Resolves to the new member. */
export function useApproveOrgJoinRequest(orgId: string): UseMutationResult<OrgMember, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgMember, ApiError, string>({
    mutationFn: (requestId) => o.approveJoinRequest(orgId, requestId),
    onSuccess: (_m, requestId) => dropJoinRequest(qc, orgId, requestId),
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: orgsKeys.joinRequests(orgId) }),
        qc.invalidateQueries({ queryKey: orgsKeys.members(orgId) }),
        qc.invalidateQueries({ queryKey: orgsKeys.detail(orgId) }),
      ]),
  });
}

/** Decline: they can ask again after 7 days. */
export function useDenyOrgJoinRequest(orgId: string): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (requestId) => o.denyJoinRequest(orgId, requestId),
    onSuccess: (_d, requestId) => dropJoinRequest(qc, orgId, requestId),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.joinRequests(orgId) }),
  });
}

// ---- joining by email domain -------------------------------------------------

/** Orgs your verified email domain lets you join or ask to join (usually 0 or 1). */
export function useJoinableOrgs(options?: QueryOverrides<Page<JoinableOrg>>): UseQueryResult<Page<JoinableOrg>, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.joinable(), ({ signal }) => o.joinable({ signal }), options);
}

/** Join through an auto-join domain. Resolves to the org. */
export function useJoinOrgByDomain(): UseMutationResult<Org, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<Org, ApiError, string>({ mutationFn: (orgId) => o.joinByDomain(orgId), onSuccess: (org) => afterJoin(qc, org) });
}

/** Ask to join through a request domain. */
export function useRequestToJoinOrg(): UseMutationResult<MyOrgJoinRequest, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<MyOrgJoinRequest, ApiError, string>({
    mutationFn: (orgId) => o.requestToJoin(orgId),
    onSuccess: (req) =>
      qc.setQueryData<Page<JoinableOrg>>(orgsKeys.joinable(), (data) =>
        data ? { ...data, items: data.items.map((j) => (j.org.id === req.org.id ? { ...j, action: "requested" as const, request_id: req.id, requested_at: req.created_at } : j)) } : data,
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.joinable() }),
  });
}

/** Withdraw your pending request (takes the request id). */
export function useCancelOrgJoinRequest(): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({ mutationFn: (requestId) => o.cancelJoinRequest(requestId), onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.joinable() }) });
}

/** Joins every auto-join org your email qualifies for. The apps' session mounts call it once per session. */
export function useOrgsAutoJoin(): UseMutationResult<OrgsAutoJoined, ApiError, void> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<OrgsAutoJoined, ApiError, void>({
    mutationFn: () => o.autoJoin(),
    onSuccess: (r) => {
      if (r.joined.length === 0) return;
      for (const org of r.joined) qc.setQueryData(orgsKeys.detail(org.id), org);
      return Promise.all([qc.invalidateQueries({ queryKey: orgsKeys.list() }), qc.invalidateQueries({ queryKey: orgsKeys.joinable() })]);
    },
  });
}

// ---- staff -------------------------------------------------------------------

export function useAdminOrgs(params: AdminOrgListParams = {}, options?: InfiniteOverrides<AdminOrg>): PagedQueryResult<AdminOrg> {
  const o = useEndpoints(orgsEndpoints);
  return usePagedQuery<AdminOrg>(orgsKeys.admin.list(listKeyParams(params)), (cursor, signal) => o.admin.list({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

export function useAdminOrg(orgId: string | null | undefined, options?: QueryOverrides<AdminOrgDetail>): UseQueryResult<AdminOrgDetail, ApiError> {
  const o = useEndpoints(orgsEndpoints);
  return useApiQuery(orgsKeys.admin.detail(orgId ?? ""), ({ signal }) => o.admin.get(orgId ?? "", { signal }), options, !!orgId);
}

/** Admin role: free a claimed domain (audited). */
export function useAdminReleaseOrgDomain(orgId: string): UseMutationResult<void, ApiError, string> {
  const o = useEndpoints(orgsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (domainId) => o.admin.releaseDomain(orgId, domainId),
    onSettled: () => qc.invalidateQueries({ queryKey: orgsKeys.admin.detail(orgId) }),
  });
}

// ---- realtime ----------------------------------------------------------------

registerRealtimeHandler("orgs.membership_changed", (qc) => {
  void qc.invalidateQueries({ queryKey: orgsKeys.list() });
  void qc.invalidateQueries({ queryKey: orgsKeys.myInvites() });
  void qc.invalidateQueries({ queryKey: orgsKeys.joinable() });
});
registerRealtimeHandler("orgs.domains_changed", (qc, e) => void qc.invalidateQueries({ queryKey: orgsKeys.domains(e.data.org_id) }));
registerRealtimeHandler("orgs.join_requests_changed", (qc, e) => void qc.invalidateQueries({ queryKey: orgsKeys.joinRequests(e.data.org_id) }));
registerRealtimeHandler("orgs.updated", (qc, e) => {
  void qc.invalidateQueries({ queryKey: orgsKeys.detail(e.data.org_id) });
  void qc.invalidateQueries({ queryKey: orgsKeys.list() });
});
registerRealtimeHandler("orgs.deleted", (qc, e) => dropOrg(qc, e.data.org_id));
for (const type of ["orgs.member_joined", "orgs.member_updated", "orgs.member_removed"] as const) {
  registerRealtimeHandler(type, (qc, e) => {
    void qc.invalidateQueries({ queryKey: orgsKeys.members(e.data.org_id) });
    void qc.invalidateQueries({ queryKey: orgsKeys.detail(e.data.org_id) });
  });
}
registerReconnectRefetch(orgsKeys.all());
