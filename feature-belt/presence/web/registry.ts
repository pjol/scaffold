import type { AccountMenuItem, SessionMount } from "../registry";
import { PresenceMenuItems } from "./PresenceMenuItems";
import { PresenceSession } from "./PresenceSession";

/** Account menu: your status, pause notifications, appear away. */
export const presenceAccountMenu: AccountMenuItem = { id: "presence", Component: PresenceMenuItems };

/** Heartbeats, live presence topics and the status dialogs, while signed in. */
export const presenceSession: SessionMount = PresenceSession;
