import { Share2, ShieldCheck, Users } from "lucide-react";
import { useState } from "react";
import type { ResourceMenuContext, ResourceMenuProvider, SettingsSection } from "../registry";
import { ShareDialog } from "./ShareDialog";

/** Settings → Teams: the active org's teams, members and leads. */
export const accessTeamsSettings: SettingsSection = { href: "/settings/teams", label: "Teams", icon: Users };

/** Settings → Permissions: who holds each org permission (admins change it; others see theirs). */
export const accessPermissionsSettings: SettingsSection = { href: "/settings/permissions", label: "Permissions", icon: ShieldCheck };

function useShareActions(ctx: ResourceMenuContext) {
  const [open, setOpen] = useState(false);
  if (!ctx.kind || !ctx.id) return { actions: [] };
  return {
    actions: [{ id: "access-share", label: "Share…", icon: Share2, onSelect: () => setOpen(true) }],
    element: <ShareDialog kind={ctx.kind} id={ctx.id} {...(ctx.title ? { title: ctx.title } : {})} open={open} onOpenChange={setOpen} />,
  };
}

/** "Share…" in every ⋮ menu that names one access resource (kind and id). */
export const accessShareMenu: ResourceMenuProvider = { id: "access-share", useActions: useShareActions };
