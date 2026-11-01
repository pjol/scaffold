import { GitBranch, KeyRound } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Repositories in the main navigation (they also open from the drive). */
export const reposNav: NavItem = { href: "/repos", label: "Repositories", icon: GitBranch };

/** Settings → Git credentials: passwords for git clone, pull and push over HTTPS. */
export const reposCredentialsSettings: SettingsSection = { href: "/settings/git-credentials", label: "Git credentials", icon: KeyRound };

/** The same entry as a Home tile (its own constant: the CLI skips a line already in the file). */
export const reposHomeTile: HomeTile = { href: "/repos", label: "Repositories", icon: GitBranch };
