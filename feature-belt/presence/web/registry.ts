import { BellOff, CalendarClock, Smile } from "lucide-react";
import type { AccountMenuItem, QuickAction, SessionMount } from "../registry";
import { PresenceMenuItems } from "./PresenceMenuItems";
import { PresenceSession } from "./PresenceSession";
import { openPresenceDialog } from "./store";

/** Account menu: your status, pause notifications, appear away. */
export const presenceAccountMenu: AccountMenuItem = { id: "presence", Component: PresenceMenuItems };

/** Heartbeats, live presence topics and the status dialogs, while signed in. */
export const presenceSession: SessionMount = PresenceSession;

/** ⌘K: set a status, pause or resume notifications, the notification schedule. */
export const presenceStatusAction: QuickAction = { id: "presence.status", label: "Set a status", icon: Smile, keywords: ["status", "away", "emoji"], run: () => openPresenceDialog("status") };
export const presencePauseAction: QuickAction = {
  id: "presence.pause",
  label: "Pause or resume notifications",
  icon: BellOff,
  keywords: ["do not disturb", "dnd", "snooze", "mute", "quiet"],
  run: () => openPresenceDialog("dnd"),
};
export const presenceScheduleAction: QuickAction = {
  id: "presence.schedule",
  label: "Notification schedule",
  icon: CalendarClock,
  keywords: ["quiet hours", "do not disturb", "dnd", "night"],
  run: () => openPresenceDialog("schedule"),
};
