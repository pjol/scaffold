import { configureActiveOrgStorage, orgsNounTitle } from "@scaffoldapp/shared";
import { Building2 } from "lucide-react";
import type { AccountMenuItem, NavItem, SessionMount, SettingsSection } from "../registry";
import { OrgMenuItems } from "./OrgMenuItems";
import { OrgsAutoJoin } from "./OrgsAutoJoin";

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

// Labels are getters so they read the app's noun (configureOrgsNoun in stitching) when the
// shell renders, not when this module loads.

/** Settings → Organizations: your orgs, invites to you, and each org's settings. */
export const orgsSettings: SettingsSection = {
  href: "/settings/orgs",
  get label() {
    return orgsNounTitle("many");
  },
  icon: Building2,
};

/** Admin tab: every organization. */
export const orgsAdminNav: NavItem = {
  href: "/admin/orgs",
  get label() {
    return orgsNounTitle("many");
  },
  icon: Building2,
};

/** Account menu: switch the active org, open its settings. */
export const orgsAccountMenu: AccountMenuItem = { id: "orgs", Component: OrgMenuItems };

/** Session mount: joins orgs whose verified auto-join domain matches your email. */
export const orgsAutoJoin: SessionMount = OrgsAutoJoin;
