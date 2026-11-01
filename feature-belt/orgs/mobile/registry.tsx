import { configureActiveOrgStorage, orgsNounTitle, useMyInviteCount } from "@scaffoldapp/shared";
import { Building2 } from "lucide-react-native";
import type { ComponentType } from "react";
import { userStorage } from "@/lib/storage";
import type { SettingsSection } from "../registry";
import { OrgsAutoJoin } from "./OrgsAutoJoin";

// The active org is per user and wiped on sign-out with the rest of userStorage.
configureActiveOrgStorage({
  load: () => userStorage.get<string>("active-org"),
  save: (id) => (id ? userStorage.set("active-org", id) : userStorage.remove("active-org")),
});

/** Settings → Organizations (badge: pending invites to you). */
export const orgsSettings: SettingsSection = {
  id: "orgs",
  items: [
    {
      id: "orgs",
      // A getter, so the row reads the app's noun (configureOrgsNoun) when Settings renders.
      get title() {
        return orgsNounTitle("many");
      },
      icon: Building2,
      href: "/settings/orgs",
      useBadge: useMyInviteCount,
    },
  ],
};

/** Session mount: joins orgs whose verified auto-join domain matches your email. */
export const orgsAutoJoin: ComponentType = OrgsAutoJoin;
