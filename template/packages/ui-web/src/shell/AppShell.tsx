import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export interface AppShellProps {
  /** Side navigation, shown at ≥ 1024 px. */
  sideNav?: ReactNode;
  /** Bottom tab bar, shown below 1024 px. */
  bottomNav?: ReactNode;
  /** Top bar (logo, primary action, account menu). */
  topBar?: ReactNode;
  /** Above the content, under the top bar (a status banner). */
  notice?: ReactNode;
  children: ReactNode;
  className?: string;
}

/**
 * App frame: side nav at ≥ 1024 px, bottom tab bar below, top bar over the content.
 * The content area is a `<main>`; pages set their own container
 * (`mx-auto w-full max-w-content px-gutter py-section`). Server-component safe.
 */
export function AppShell({ sideNav, bottomNav, topBar, notice, children, className }: AppShellProps) {
  return (
    <div className={cn("min-h-dvh bg-canvas lg:flex", className)}>
      {sideNav && <div className="hidden shrink-0 border-r border-border bg-surface lg:block">{sideNav}</div>}
      <div className="flex min-w-0 flex-1 flex-col">
        {topBar}
        {notice}
        <main id="main" className="flex min-w-0 flex-1 flex-col">
          {children}
        </main>
      </div>
      {bottomNav && <div className="lg:hidden">{bottomNav}</div>}
    </div>
  );
}
