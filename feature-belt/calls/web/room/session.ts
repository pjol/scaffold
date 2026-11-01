import type { CallsJoinToken, CallsKind, CallsRole } from "@scaffoldapp/shared";
import { createContext, useContext } from "react";
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
}

/** Why the call screen closed. */
export type CallEnd = "left" | "ended" | "removed" | "elsewhere" | "failed";

export const CallSessionContext = createContext<CallSession | null>(null);

export function useCallSession(): CallSession {
  const s = useContext(CallSessionContext);
  if (!s) throw new Error("useCallSession outside a call");
  return s;
}

/** Panels that open beside (desktop) or over (phones) the stage. */
export type CallPanel = "people" | "chat" | null;
export type CallLayout = "auto" | "grid" | "speaker";
