import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type { LeaderboardBoard, LeaderboardEntry, LeaderboardProfile, LeaderboardStanding, LeaderboardVisibility } from "./types";

/** Leaderboard endpoints (`/v1/leaderboard`). */
export function leaderboardEndpoints(http: HttpClient) {
  const L = "/v1/leaderboard";
  return {
    boards: (o?: CallOptions) => http.get<{ items: LeaderboardBoard[] }>(`${L}/boards`, o),
    /** Ranked entries, best first. */
    entries: (board: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<LeaderboardEntry>>(`${L}/boards/${seg(board)}`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    standing: (board: string, o?: CallOptions) => http.get<LeaderboardStanding>(`${L}/boards/${seg(board)}/me`, o),
    profile: (o?: CallOptions) => http.get<LeaderboardProfile>(`${L}/profile`, o),
    setVisibility: (visibility: LeaderboardVisibility, o?: CallOptions) => http.put<LeaderboardProfile>(`${L}/profile`, { visibility }, o),
  };
}

export type LeaderboardEndpoints = ReturnType<typeof leaderboardEndpoints>;
