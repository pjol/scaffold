import type { CallOptions, HttpClient } from "../api/http";
import type {
  Presence,
  PresenceCreatePresetRequest,
  PresenceHeartbeatRequest,
  PresenceHeartbeatResult,
  PresenceMine,
  PresenceSchedule,
  PresenceSetStatusRequest,
  PresenceStatusPreset,
} from "./types";

/** `/v1/presence` endpoints (signed in). */
export function presenceEndpoints(http: HttpClient) {
  const P = "/v1/presence";
  return {
    /** The people you may see among `userIds` (≤ 100); others are left out. */
    list: (userIds: string[], o?: CallOptions) => http.get<{ items: Presence[] }>(P, { ...o, query: { user_ids: userIds.join(",") } }),
    me: (o?: CallOptions) => http.get<PresenceMine>(`${P}/me`, o),
    heartbeat: (body: PresenceHeartbeatRequest, o?: CallOptions) => http.post<PresenceHeartbeatResult>(`${P}/heartbeat`, body, o),
    /** This device is going away (tab closed, signed out). */
    offline: (deviceId: string, o?: CallOptions) => http.post<void>(`${P}/offline`, { device_id: deviceId }, o),
    setStatus: (body: PresenceSetStatusRequest, o?: CallOptions) => http.put<PresenceMine>(`${P}/me/status`, body, o),
    clearStatus: (o?: CallOptions) => http.delete<PresenceMine>(`${P}/me/status`, o),
    setDND: (until: string, o?: CallOptions) => http.put<PresenceMine>(`${P}/me/dnd`, { until }, o),
    clearDND: (o?: CallOptions) => http.delete<PresenceMine>(`${P}/me/dnd`, o),
    setAway: (away: boolean, o?: CallOptions) => http.put<PresenceMine>(`${P}/me/away`, { away }, o),
    /** Quiet hours per weekday; pushes and emails hold during them like any Do Not Disturb. */
    setSchedule: (body: PresenceSchedule, o?: CallOptions) => http.put<PresenceMine>(`${P}/me/schedule`, body, o),
    clearSchedule: (o?: CallOptions) => http.delete<PresenceMine>(`${P}/me/schedule`, o),
    presets: (o?: CallOptions) => http.get<{ items: PresenceStatusPreset[] }>(`${P}/me/presets`, o),
    createPreset: (body: PresenceCreatePresetRequest, o?: CallOptions) => http.post<PresenceStatusPreset>(`${P}/me/presets`, body, o),
    deletePreset: (id: string, o?: CallOptions) => http.delete<void>(`${P}/me/presets/${encodeURIComponent(id)}`, o),
  };
}

export type PresenceEndpoints = ReturnType<typeof presenceEndpoints>;
