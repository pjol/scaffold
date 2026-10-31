import { configureActiveOrgStorage } from "@scaffoldapp/shared";
import { Building2 } from "lucide-react";
import type { AccountMenuItem, NavItem, SettingsSection } from "../registry";
import { OrgMenuItems } from "./OrgMenuItems";

const ACTIVE_ORG_KEY = "scaffoldapp.active-org";

// The active org survives reloads in this browser. Signed-out pages never read it.
if (typeof window !== "undefined") {
  configureActiveOrgStorage({
    load: () => {
      try {
        return window.localStorage.getItem(ACTIVE_ORG_KEY);
      } catch {
        return null;
      }
    },
    save: (id) => {
      try {
        if (id) window.localStorage.setItem(ACTIVE_ORG_KEY, id);
        else window.localStorage.removeItem(ACTIVE_ORG_KEY);
      } catch {
        // Storage blocked: the choice lasts for this tab only.
      }
    },
  });
}

/** Settings → Organizations: your orgs, invites to you, and each org's settings. */
export const orgsSettings: SettingsSection = { href: "/settings/orgs", label: "Organizations", icon: Building2 };

/** Admin tab: every organization. */
export const orgsAdminNav: NavItem = { href: "/admin/orgs", label: "Organizations", icon: Building2 };

/** Account menu: switch the active org, open its settings. */
export const orgsAccountMenu: AccountMenuItem = { id: "orgs", Component: OrgMenuItems };
