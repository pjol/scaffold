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

/** One weekday's quiet hours ("22:00" → "08:00" runs overnight; "00:00" → "24:00" is all day). */
export interface PresenceQuietHours {
  start: string;
  end: string;
}

/** Do Not Disturb schedule: quiet hours per weekday (Sunday first), read in `time_zone`. */
export interface PresenceSchedule {
  enabled: boolean;
  /** IANA zone, e.g. `Europe/Berlin`. */
  time_zone: string;
  days: (PresenceQuietHours | null)[];
}

/** A status you saved to set again in one tap (`/presence/me/presets`). */
export interface PresenceStatusPreset {
  id: string;
  emoji: string;
  text: string;
  clear_after: PresenceClearAfter;
  created_at: string;
}

/** `GET /v1/presence/me`: your presence plus what the client needs to keep it live. */
export interface PresenceMine extends Presence {
  /** "Appear away" is on. */
  away: boolean;
  /** Subscribe to `presence:<id>` for each to see others change live. */
  audiences: string[];
  heartbeat_seconds: number;
  idle_after_seconds: number;
  /** Do Not Disturb set by hand or by a feature (calendar focus time). */
  manual_dnd_until: string | null;
  /** End of the schedule's quiet hours you are in; null outside them or after resuming. */
  quiet_until: string | null;
  schedule: PresenceSchedule | null;
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

export interface PresenceCreatePresetRequest {
  emoji: string;
  text: string;
  clear_after: PresenceClearAfter;
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
