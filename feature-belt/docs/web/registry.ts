import { FileText } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Main navigation: the org's pages and databases. */
export const docsNav: NavItem = { href: "/docs", label: "Docs", icon: FileText };

/** Home tile (its own constant: the CLI skips a line already in the file). */
export const docsHome: HomeTile = { href: "/docs", label: "Docs", icon: FileText };
