import type { ComponentType, ReactNode } from "react";

/** A navigation entry as the shell renders it. */
export interface ShellNavItem {
  href: string;
  /** One or two words, always shown with the icon. */
  label: string;
  icon: ReactNode;
  /** Count (unread, pending). 0 or undefined hides it. */
  badge?: number;
  /** `prefix` (default): also active on nested routes. */
  match?: "exact" | "prefix";
}

/** Router link component (Next's `Link`). */
export type LinkComponent = ComponentType<{
  href: string;
  className?: string;
  children?: ReactNode;
  "aria-current"?: "page" | undefined;
  "aria-label"?: string;
  onClick?: () => void;
}>;

/** Whether `item` is the active entry for `pathname`. */
export function isNavItemActive(item: Pick<ShellNavItem, "href" | "match">, pathname: string | null | undefined): boolean {
  if (!pathname) return false;
  if (item.match === "exact" || item.href === "/") return pathname === item.href;
  return pathname === item.href || pathname.startsWith(item.href.endsWith("/") ? item.href : `${item.href}/`);
}

/** Badge text: 1–99, then "99+". */
export function badgeText(n: number): string {
  return n > 99 ? "99+" : String(n);
}
