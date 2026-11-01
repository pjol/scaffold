import { activeCustomStatus, isPresenceDND, useMyPresence } from "@scaffoldapp/shared";
import { Smile } from "lucide-react-native";
import type { ComponentType } from "react";
import type { SettingsSection } from "../registry";
import { PresenceSession } from "./PresenceSession";

/** The settings row shows your status emoji (or a pause mark) as its badge. */
function useStatusBadge(): string | undefined {
  const mine = useMyPresence().data;
  if (isPresenceDND(mine)) return "Paused";
  return activeCustomStatus(mine)?.emoji || undefined;
}

/** Settings → Status: custom status, pause notifications, appear away. */
export const presenceSettings: SettingsSection = {
  id: "presence",
  items: [{ id: "presence", title: "Status", icon: Smile, href: "/presence/status", useBadge: useStatusBadge }],
};

/** Heartbeats while the app is in the foreground. */
export const presenceSession: ComponentType = PresenceSession;
