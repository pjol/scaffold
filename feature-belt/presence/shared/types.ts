/**
 * Presence DTOs, snake_case, matching `backend/internal/presence` (status.go) exactly.
 * Change both sides together.
 */

export type PresenceStatus = "online" | "away" | "offline";
export type PresenceClearAfter = "30m" | "1h" | "4h" | "today" | "week" | "never";
export type PresencePlatform = "web" | "ios" | "android";

/** What a person says about themselves ("🌴 Back on Monday"). */
export interface PresenceCustomStatus {
  emoji: string;
  text: string;
  clear_after: PresenceClearAfter;
  /** Null = never clears. */
  expires_at: string | null;
}

/** One person's state as others see it. */
export interface Presence {
  user_id: string;
  status: PresenceStatus;
  custom_status: PresenceCustomStatus | null;
  /** Do Not Disturb until; null = off. */
  dnd_until: string | null;
}

/** `GET /v1/presence/me`: your presence plus what the client needs to keep it live. */
export interface PresenceMine extends Presence {
  /** "Appear away" is on. */
  away: boolean;
  /** Subscribe to `presence:<id>` for each to see others change live. */
  audiences: string[];
  heartbeat_seconds: number;
  idle_after_seconds: number;
}

export interface PresenceHeartbeatRequest {
  device_id: string;
  platform: PresencePlatform;
  /** False when the person is idle on this device. */
  active: boolean;
}

export interface PresenceHeartbeatResult {
  status: PresenceStatus;
  heartbeat_seconds: number;
  idle_after_seconds: number;
}

export interface PresenceSetStatusRequest {
  emoji: string;
  text: string;
  clear_after: PresenceClearAfter;
  /** Required for `today` and `week`: see `presenceClearAfterExpiry`. */
  expires_at?: string | null;
}

declare module "../types" {
  interface RealtimeEventMap {
    "presence.updated": Presence;
  }
}
