import { ShoppingBasket } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** App paths (the backend and agents link to the same ones). */
export const basketsPaths = {
  list: "/baskets",
  create: "/baskets/new",
  /** Start a new basket from an existing one's lines. */
  copy: (symbol: string) => `/baskets/new?from=${encodeURIComponent(symbol)}`,
  basket: (symbol: string) => `/baskets/${encodeURIComponent(symbol)}`,
  /** The exchange's trade screen for the basket's market. */
  market: (symbol: string) => `/markets/${encodeURIComponent(symbol)}`,
  admin: "/admin/baskets",
} as const;

/** Main nav: discover baskets and create one. */
export const basketsNav: NavItem = { href: basketsPaths.list, label: "Baskets", icon: ShoppingBasket };

/** Home tile (a separate name: `scaffold add` skips lines already in the registry). */
export const basketsHomeTile: HomeTile = { href: basketsPaths.list, label: "Baskets", icon: ShoppingBasket };

/** Admin → Baskets: reports, hide, rename, archive. */
export const basketsAdminNav: NavItem = { href: basketsPaths.admin, label: "Baskets", icon: ShoppingBasket };
