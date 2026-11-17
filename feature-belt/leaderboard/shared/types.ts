/** Leaderboard DTOs, mirroring backend/internal/leaderboard. Scores are decimal strings. */

export type LeaderboardFormat = "number" | "money" | "percent";
/** How the caller appears: a generated alias (default), their name and photo, or not at all. */
export type LeaderboardVisibility = "alias" | "name" | "hidden";

export interface LeaderboardBoard {
  id: string;
  title: string;
  format: LeaderboardFormat;
  /** Money boards: the currency code. */
  currency?: string;
  /** Lower scores rank first. */
  ascending: boolean;
  /** People with a rank. */
  ranked: number;
  updated_at: string | null;
}

export interface LeaderboardEntry {
  rank: number;
  name: string;
  /** Only for people who show their name. */
  avatar_url: string | null;
  score: string;
  is_me: boolean;
}

/** The caller's place on one board. */
export interface LeaderboardStanding {
  board: string;
  /** Null: not ranked (no score yet, or hidden). */
  rank: number | null;
  score: string | null;
  ranked: number;
  visibility: LeaderboardVisibility;
}

export interface LeaderboardProfile {
  visibility: LeaderboardVisibility;
  alias: string;
  display_name: string;
}
