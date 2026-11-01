import { FolderClosed, Globe } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Side nav / tab bar entry. */
export const filesNav: NavItem = { href: "/files", label: "Files", icon: FolderClosed };

/** Home tile (a separate constant so the CLI can wire both lines). */
export const filesHome: HomeTile = { href: "/files", label: "Files", icon: FolderClosed };

/** Admin tab: what is shared publicly, and what staff turned off. */
export const filesAdminNav: NavItem = { href: "/admin/files", label: "Public files", icon: Globe };
