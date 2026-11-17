import { Library, LibraryBig } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** App paths (global search and agents link to the same ones). */
export const catalogPaths = {
  list: "/catalog",
  item: (key: string) => `/catalog/${encodeURIComponent(key)}`,
  admin: "/admin/catalog",
} as const;

/** Catalog: search and browse every item. */
export const catalogNav: NavItem = { href: catalogPaths.list, label: "Catalog", icon: Library };

/** Home tile for the catalog. */
export const catalogHomeTile: HomeTile = { href: catalogPaths.list, label: "Catalog", icon: Library };

/** Admin → Catalog: sync status and log, sync now, retire and restore items. */
export const catalogAdminNav: NavItem = { href: catalogPaths.admin, label: "Catalog", icon: LibraryBig };
