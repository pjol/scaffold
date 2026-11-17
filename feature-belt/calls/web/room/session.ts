import type { CallsJoinToken, CallsKind, CallsRole, CallsRoomTarget } from "@scaffoldapp/shared";
import { createContext, useContext, useMemo } from "react";
import type { GuestPass } from "../storage";

/** Everything the in-call UI needs about the meeting and who you are in it. */
export interface CallSession {
  token: CallsJoinToken;
  meetingId: string;
  code: string;
  title: string;
  kind: CallsKind;
  role: CallsRole;
  joinUrl: string;
  /** Guests act through their lobby pass instead of an account. */
  guest: GuestPass | null;
  /** Move to another room of this meeting (a breakout room, or back to the main room). */
  switchRoom: (token: CallsJoinToken) => void;
  /** A fresh token for the meeting's main room. */
  mainToken: () => Promise<CallsJoinToken>;
}

/** Why the call screen closed. */
export type CallEnd = "left" | "ended" | "removed" | "elsewhere" | "failed";

export const CallSessionContext = createContext<CallSession | null>(null);

export function useCallSession(): CallSession {
  const s = useContext(CallSessionContext);
  if (!s) throw new Error("useCallSession outside a call");
  return s;
}

/** Who acts in this call for in-call requests (polls, questions, captions, breakout rooms). */
export function useRoomTarget(): CallsRoomTarget {
  const s = useCallSession();
  const lobbyId = s.guest?.lobbyId;
  const key = s.guest?.key;
  return useMemo(() => ({ meetingId: s.meetingId, guest: lobbyId && key ? { lobbyId, key } : null }), [s.meetingId, lobbyId, key]);
}

/** Whether this connection is to a breakout room rather than the main room. */
export function inBreakoutRoom(s: CallSession): boolean {
  return s.token.room !== s.meetingId;
}

/** Panels that open beside (desktop) or over (phones) the stage. */
export type CallPanel = "people" | "chat" | "activities" | null;
export type CallActivity = "polls" | "questions" | "breakouts";
export type CallLayout = "auto" | "grid" | "speaker";
