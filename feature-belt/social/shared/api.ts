import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  SocialActivityItem,
  SocialFollowState,
  SocialHandleCheck,
  SocialMyProfile,
  SocialPerson,
  SocialProfile,
  UpdateSocialProfileRequest,
} from "./types";

/** Handle rules (mirrors the backend): 3–20 of a–z, 0–9 and _, starting with a letter. */
export const SOCIAL_HANDLE_PATTERN = /^[a-z][a-z0-9_]{2,19}$/;

/** "@Sam_Lee " → "sam_lee", the form handles are stored in. */
export function normalizeSocialHandle(input: string): string {
  return input.trim().replace(/^@/, "").toLowerCase();
}

/** App path of a profile ("/u/sam"); `tab` opens followers or following. */
export function socialProfilePath(handle: string, tab?: "followers" | "following"): string {
  return `/u/${encodeURIComponent(handle)}${tab ? `?tab=${tab}` : ""}`;
}

/** Social endpoints (`/v1/social`). `{handle}` also takes "me". */
export function socialEndpoints(http: HttpClient) {
  const S = "/v1/social";
  return {
    me: (o?: CallOptions) => http.get<SocialMyProfile>(`${S}/me`, o),
    updateMe: (body: UpdateSocialProfileRequest, o?: CallOptions) => http.patch<SocialMyProfile>(`${S}/me`, body, o),
    /** Change the handle: 409 handle_taken, 422 handle_invalid/handle_reserved, 429 handle_cooldown (details.available_at). */
    changeHandle: (handle: string, o?: CallOptions) => http.put<SocialMyProfile>(`${S}/me/handle`, { handle }, o),
    checkHandle: (handle: string, o?: CallOptions) => http.get<SocialHandleCheck>(`${S}/handles/${seg(handle)}`, o),

    profile: (handle: string, o?: CallOptions) => http.get<SocialProfile>(`${S}/profiles/${seg(handle)}`, o),
    followers: (handle: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<SocialPerson>>(`${S}/profiles/${seg(handle)}/followers`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    following: (handle: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<SocialPerson>>(`${S}/profiles/${seg(handle)}/following`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    activity: (handle: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<SocialActivityItem>>(`${S}/profiles/${seg(handle)}/activity`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),

    /** Idempotent. 403 blocked, 422 self_follow or follow_limit. */
    follow: (userId: string, o?: CallOptions) => http.put<SocialFollowState>(`${S}/follows/${seg(userId)}`, undefined, o),
    unfollow: (userId: string, o?: CallOptions) => http.delete<SocialFollowState>(`${S}/follows/${seg(userId)}`, o),

    /** Activity of the people the caller follows, newest first. */
    feed: (params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<SocialActivityItem>>(`${S}/feed`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** Handle or name prefix; under 2 characters matches nobody. */
    people: (q: string, params: { limit?: number } = {}, o?: CallOptions) => http.get<{ items: SocialPerson[] }>(`${S}/people`, { ...o, query: { q, limit: params.limit } }),
    suggestions: (params: { limit?: number } = {}, o?: CallOptions) => http.get<{ items: SocialPerson[] }>(`${S}/suggestions`, { ...o, query: { limit: params.limit } }),
  };
}

export type SocialEndpoints = ReturnType<typeof socialEndpoints>;
