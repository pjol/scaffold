"use client";

import { useEffect, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";

export interface TopBarProps {
  /** Left: logo (phones) or a back button. */
  leading?: ReactNode;
  /** Middle content (search, the page title on phones). */
  children?: ReactNode;
  /** Right: the primary action and the account menu. */
  actions?: ReactNode;
  className?: string;
}

/**
 * Sticky translucent top bar, 56 px on phones and 64 from 640 px; a hairline appears once
 * the page scrolls. Its row uses the page container (`max-w-content px-gutter`), so it
 * lines up with screen content.
 */
export function TopBar({ leading, children, actions, className }: TopBarProps) {
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 4);
    on();
    window.addEventListener("scroll", on, { passive: true });
    return () => window.removeEventListener("scroll", on);
  }, []);
  return (
    <header
      className={cn(
        "sticky top-0 z-(--app-z-sticky) w-full border-b pt-safe backdrop-blur-xl transition-colors duration-(--app-duration-base)",
        "bg-[color-mix(in_oklab,var(--app-canvas)_88%,transparent)]",
        scrolled ? "border-border" : "border-transparent",
        className,
      )}
    >
      <div className="mx-auto flex h-topbar w-full max-w-content items-center gap-3 px-gutter">
        {leading && <div className="flex shrink-0 items-center">{leading}</div>}
        <div className="flex min-w-0 flex-1 items-center">{children}</div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
    </header>
  );
}
