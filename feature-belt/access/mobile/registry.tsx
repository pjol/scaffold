import { Share2, Users } from "lucide-react-native";
import { useState } from "react";
import type { ResourceMenuContext, ResourceMenuProvider, SettingsSection } from "../registry";
import { ShareSheet } from "./ShareSheet";

/** Settings → Teams. The permissions matrix is web-only (an admin desk task). */
export const accessTeamsSettings: SettingsSection = {
  id: "access",
  items: [{ id: "access-teams", title: "Teams", icon: Users, href: "/settings/teams" }],
};

function useShareActions(ctx: ResourceMenuContext) {
  const [open, setOpen] = useState(false);
  if (!ctx.kind || !ctx.id) return { actions: [] };
  return {
    actions: [{ id: "access-share", label: "Share…", icon: Share2, onSelect: () => setOpen(true) }],
    element: <ShareSheet kind={ctx.kind} id={ctx.id} {...(ctx.title ? { title: ctx.title } : {})} open={open} onClose={() => setOpen(false)} />,
  };
}

/** "Share…" in every ⋮ menu that names one access resource (kind and id). */
export const accessShareMenu: ResourceMenuProvider = { id: "access-share", useActions: useShareActions };
