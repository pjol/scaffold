"use client";

import { motion } from "motion/react";
import { useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { badgeText, isNavItemActive, type LinkComponent, type ShellNavItem } from "./nav";

export interface BottomNavProps {
  /** 2–5 items, icon and label always both. Overflow goes into the account menu, never a scrolling bar. */
  items: ShellNavItem[];
  activeHref?: string | null;
  linkAs?: LinkComponent;
  className?: string;
}

function Anchor(props: { href: string; className?: string; children?: ReactNode; "aria-current"?: "page" | undefined }) {
  return <a {...props} />;
}

/**
 * Phone tab bar: labelled icons, a gliding active pill, safe-area padding. Renders a
 * spacer of its own height so the last item on the page is never covered.
 */
export function BottomNav({ items, activeHref, linkAs, className }: BottomNavProps) {
  const t = useMotionTokens();
  const layoutId = useId();
  const L = linkAs ?? Anchor;
  return (
    <>
      <div aria-hidden className="box-content h-16 pb-safe" />
      <nav
        aria-label="Main"
        className={cn("fixed inset-x-0 bottom-0 z-(--app-z-sticky) border-t border-border pb-safe backdrop-blur-xl", "bg-[color-mix(in_oklab,var(--app-surface)_92%,transparent)]", className)}
      >
        <ul className="mx-auto grid h-16 max-w-xl" style={{ gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))` }}>
          {items.map((item) => {
            const active = isNavItemActive(item, activeHref);
            return (
              <li key={item.href} className="flex">
                <L
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={cn("flex flex-1 flex-col items-center justify-center gap-1 type-caption font-semibold transition-colors", active ? "text-primary-strong" : "text-fg-muted")}
                >
                  <span className="relative flex h-8 w-14 items-center justify-center [&_svg]:size-5">
                    {active && <motion.span layoutId={layoutId} className="absolute inset-0 rounded-pill bg-primary-soft" transition={t.spring.snappy} />}
                    <span className="relative flex">{item.icon}</span>
                    {!!item.badge && (
                      <span className="absolute -top-1 right-1 min-w-5 rounded-pill bg-danger px-1 text-center type-caption font-semibold text-danger-fg ring-2 ring-surface">
                        {badgeText(item.badge)}
                      </span>
                    )}
                  </span>
                  <span className="max-w-full truncate px-1">{item.label}</span>
                </L>
              </li>
            );
          })}
        </ul>
      </nav>
    </>
  );
}
