"use client";

import { badgeText, cn, motion, useMotionTokens } from "@scaffoldapp/ui-web";
import type { LucideIcon } from "lucide-react";
import Link from "next/link";
import { useId } from "react";

export interface SectionLink {
  href: string;
  label: string;
  icon: LucideIcon;
  /** Custom active test (defaults to exact match or a nested path). */
  active?: (pathname: string) => boolean;
  /** Hook returning a count shown after the label (same contract as NavItem.useBadge). */
  useBadge?: () => number | undefined;
}

/** Calls the link's badge hook; one component per link keeps the rules of hooks. */
function TabBadge({ useCount }: { useCount: () => number | undefined }) {
  const n = useCount();
  return n ? <span className="ml-auto min-w-5 rounded-sm bg-primary px-1.5 text-center type-caption font-semibold tabular-nums text-primary-fg">{badgeText(n)}</span> : null;
}

export function isSectionActive(link: SectionLink, pathname: string): boolean {
  if (link.active) return link.active(pathname);
  return pathname === link.href || pathname.startsWith(`${link.href}/`);
}

/** Vertical side tabs (≥ 1024 px) for a section such as Settings or Admin; the indicator glides between tabs. */
export function SideTabs({ links, pathname, label, className }: { links: readonly SectionLink[]; pathname: string; label: string; className?: string }) {
  const t = useMotionTokens();
  const layoutId = useId();
  return (
    <nav aria-label={label} className={className}>
      <ul className="flex flex-col gap-1">
        {links.map((link) => {
          const active = isSectionActive(link, pathname);
          const Icon = link.icon;
          return (
            <li key={link.href}>
              <Link
                href={link.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "relative z-0 flex min-h-control items-center gap-3 rounded-md px-3 type-label font-semibold transition-colors",
                  active ? "text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
                )}
              >
                {active && <motion.span layoutId={layoutId} className="absolute inset-0 -z-10 rounded-md bg-sunken" transition={t.spring.snappy} />}
                <Icon className="size-4 shrink-0" aria-hidden />
                <span className="truncate">{link.label}</span>
                {link.useBadge && <TabBadge useCount={link.useBadge} />}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

/** Horizontal tabs for the same links below 1024 px (admin), scrolling when they overflow. */
export function ScrollTabs({ links, pathname, label, className }: { links: readonly SectionLink[]; pathname: string; label: string; className?: string }) {
  const t = useMotionTokens();
  const layoutId = useId();
  return (
    <nav aria-label={label} className={cn("overflow-x-auto scrollbar-none", className)}>
      <ul className="flex w-max gap-1 rounded-md bg-sunken p-1">
        {links.map((link) => {
          const active = isSectionActive(link, pathname);
          return (
            <li key={link.href}>
              <Link
                href={link.href}
                aria-current={active ? "page" : undefined}
                className={cn("relative z-0 flex h-control items-center gap-2 rounded-sm px-3 type-label font-semibold whitespace-nowrap transition-colors pointer-coarse:h-tap", active ? "text-fg" : "text-fg-muted")}
              >
                {active && <motion.span layoutId={layoutId} className="absolute inset-0 -z-10 rounded-sm border border-border bg-surface" transition={t.spring.snappy} />}
                {link.label}
                {link.useBadge && <TabBadge useCount={link.useBadge} />}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
