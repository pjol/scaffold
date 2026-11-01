import { configureActiveOrgStorage, useMyInviteCount } from "@scaffoldapp/shared";
import { Building2 } from "lucide-react-native";
import { userStorage } from "@/lib/storage";
import type { SettingsSection } from "../registry";

// The active org is per user and wiped on sign-out with the rest of userStorage.
configureActiveOrgStorage({
  load: () => userStorage.get<string>("active-org"),
  save: (id) => (id ? userStorage.set("active-org", id) : userStorage.remove("active-org")),
});

/** Settings → Organizations (badge: pending invites to you). */
export const orgsSettings: SettingsSection = {
  id: "orgs",
  items: [{ id: "orgs", title: "Organizations", icon: Building2, href: "/settings/orgs", useBadge: useMyInviteCount }],
};
