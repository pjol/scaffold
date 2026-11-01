import { GitBranch, KeyRound } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "../registry";

/** Repositories on Home (browse, read and review; editing is on the web). */
export const reposHomeEntry: HomeEntry = { id: "repos", title: "Repositories", icon: GitBranch, href: "/repos" };

/** Settings → Git credentials. */
export const reposCredentialsInfo: SettingsSection = {
  id: "repos",
  items: [{ id: "repos-credentials", title: "Git credentials", icon: KeyRound, href: "/settings/git-credentials" }],
};
