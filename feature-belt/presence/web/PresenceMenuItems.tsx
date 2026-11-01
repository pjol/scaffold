"use client";

import { activeCustomStatus, errorMessage, isPresenceDND, presenceStatusText, useMe, useMyPresence, useSetPresenceAway, useSetPresenceDND } from "@scaffoldapp/shared";
import { MenuItem, MenuSeparator, toast } from "@scaffoldapp/ui-web";
import { BellOff, BellRing, Moon, Smile, Sun } from "lucide-react";
import { PresenceDot } from "./PresenceDot";
import { openPresenceDialog } from "./store";

/**
 * Account menu slot (registry `accountMenuItems`): your status (opens the picker), pause or
 * resume notifications, appear away or active.
 */
export function PresenceMenuItems() {
  const me = useMe();
  const mine = useMyPresence();
  const away = useSetPresenceAway();
  const dnd = useSetPresenceDND();
  if (!mine.data || me.data?.consent.required) return null;
  const status = activeCustomStatus(mine.data);
  const paused = isPresenceDND(mine.data);
  return (
    <>
      <MenuItem icon={status?.emoji ? <span aria-hidden>{status.emoji}</span> : <Smile aria-hidden />} onSelect={() => openPresenceDialog("status")}>
        <span className="min-w-0 truncate">{status ? presenceStatusText({ ...status, emoji: "" }) || "Update your status" : "Set a status"}</span>
      </MenuItem>
      {paused ? (
        <MenuItem
          icon={<BellRing aria-hidden />}
          onSelect={() => dnd.mutate(null, { onSuccess: () => toast.success("Notifications resumed."), onError: (e) => toast.error(errorMessage(e)) })}
        >
          <span className="min-w-0 truncate">Resume notifications</span>
        </MenuItem>
      ) : (
        <MenuItem icon={<BellOff aria-hidden />} onSelect={() => openPresenceDialog("dnd")}>
          Pause notifications
        </MenuItem>
      )}
      <MenuItem
        icon={mine.data.away ? <Sun aria-hidden /> : <Moon aria-hidden />}
        onSelect={() => away.mutate(!mine.data?.away, { onError: (e) => toast.error(errorMessage(e)) })}
      >
        <span className="flex min-w-0 flex-1 items-center justify-between gap-2">
          Set yourself as {mine.data.away ? "active" : "away"}
          <PresenceDot presence={mine.data} showOffline />
        </span>
      </MenuItem>
      <MenuSeparator />
    </>
  );
}
