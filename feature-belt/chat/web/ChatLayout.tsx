"use client";

import { cn } from "@scaffoldapp/ui-web";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { InboxPanel } from "./InboxPanel";

/**
 * Fills the viewport under the top bar (and above the phone tab bar) so the inbox and
 * the thread scroll on their own and the composer stays put. Token sizes only.
 */
const PANE_HEIGHT =
  "h-[calc(100dvh_-_var(--app-size-topbar)_-_var(--spacing)*16_-_env(safe-area-inset-bottom))] lg:h-[calc(100dvh_-_var(--app-size-topbar))]";

/**
 * /chat: split view at ≥ 1024 px (inbox list + thread), stacked below (the inbox at
 * /chat, the thread at /chat/<id> with a back button).
 */
export function ChatLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname() ?? "/chat";
  const atInbox = pathname === "/chat";
  const activeId = pathname.startsWith("/chat/") ? (pathname.split("/")[2] ?? null) : null;
  return (
    <div className={cn("mx-auto flex w-full max-w-content", PANE_HEIGHT, "lg:px-gutter lg:py-6")}>
      <div className="flex min-h-0 w-full overflow-hidden bg-canvas lg:rounded-lg lg:border lg:border-border lg:bg-surface">
        <aside aria-label="Conversations" className={cn("min-h-0 w-full flex-col lg:flex lg:w-80 lg:shrink-0 lg:border-r lg:border-border", atInbox ? "flex" : "hidden")}>
          <InboxPanel activeId={activeId} />
        </aside>
        <section className={cn("min-h-0 min-w-0 flex-1 flex-col", atInbox ? "hidden lg:flex" : "flex")}>{children}</section>
      </div>
    </div>
  );
}
