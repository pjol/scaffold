import { Activity, LineChart } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** App paths (the backend and agents link to the same ones). */
export const oraclePaths = {
  feeds: "/oracle",
  feed: (key: string) => `/oracle/${encodeURIComponent(key)}`,
  admin: "/admin/oracle",
} as const;

/** Prices: every listed feed with its published price, live. */
export const oracleNav: NavItem = { href: oraclePaths.feeds, label: "Prices", icon: LineChart };

/** Home tile for Prices. */
export const oracleHomeTile: HomeTile = { href: oraclePaths.feeds, label: "Prices", icon: LineChart };

/** Admin → Oracle: feeds, source health and the observation stream. */
export const oracleAdminNav: NavItem = { href: oraclePaths.admin, label: "Oracle", icon: Activity };
