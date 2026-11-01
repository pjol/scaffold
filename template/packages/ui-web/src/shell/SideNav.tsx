"use client";

import { motion } from "motion/react";
import { useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { badgeText, isNavItemActive, type LinkComponent, type ShellNavItem } from "./nav";

export interface SideNavProps {
  items: ShellNavItem[];
  /** Current pathname (`usePathname()`). */
  activeHref?: string | null;
  /** Router link (`Link`). @default "a" */
  linkAs?: LinkComponent;
  /** Top slot, as tall as the top bar (the logo). */
  header?: ReactNode;
  /** A second group under a divider (settings, admin). */
  secondary?: ShellNavItem[];
  /** Bottom slot. */
  footer?: ReactNode;
  /** Accessible name. @default "Main" */
  label?: string;
  className?: string;
}

function Anchor(props: { href: string; className?: string; children?: ReactNode; "aria-current"?: "page" | undefined }) {
  return <a {...props} />;
}

/** Desktop navigation column. One active indicator glides between items. */
export function SideNav({ items, activeHref, linkAs, header, secondary, footer, label = "Main", className }: SideNavProps) {
  const t = useMotionTokens();
  const layoutId = useId();
  const L = linkAs ?? Anchor;
  const renderItem = (item: ShellNavItem) => {
    const active = isNavItemActive(item, activeHref);
    return (
      <li key={item.href}>
        <L
          href={item.href}
          aria-current={active ? "page" : undefined}
          className={cn(
            "relative z-0 flex min-h-control items-center gap-3 rounded-md px-3 type-label font-semibold transition-colors [&_svg]:size-5",
            active ? "text-primary-strong" : "text-fg-muted hover:bg-sunken hover:text-fg",
          )}
        >
          {active && <motion.span layoutId={layoutId} className="absolute inset-0 -z-10 rounded-md bg-primary-soft" transition={t.spring.snappy} />}
          <span className="flex shrink-0">{item.icon}</span>
          <span className="flex-1 truncate">{item.label}</span>
          {!!item.badge && <span className="min-w-6 rounded-sm bg-primary px-1.5 text-center type-caption font-semibold tabular-nums text-primary-fg">{badgeText(item.badge)}</span>}
        </L>
      </li>
    );
  };
  return (
    <aside className={cn("sticky top-0 flex h-dvh w-64 shrink-0 flex-col bg-surface", className)}>
      {header && <div className="flex h-topbar shrink-0 items-center border-b border-transparent px-5 pt-safe">{header}</div>}
      <nav className="flex flex-1 flex-col gap-4 overflow-y-auto px-2 py-4" aria-label={label}>
        <ul className="flex flex-col gap-1">{items.map(renderItem)}</ul>
        {secondary && secondary.length > 0 && (
          <>
            <hr className="border-0 border-t border-border" />
            <ul className="flex flex-col gap-1">{secondary.map(renderItem)}</ul>
          </>
        )}
      </nav>
      {footer && <div className="border-t border-border px-2 py-3">{footer}</div>}
    </aside>
  );
}
