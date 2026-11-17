import type { ReactNode } from "react";

/** Booking pages embedded on other sites: no app frame, just the page. */
export default function CalendarEmbedLayout({ children }: { children: ReactNode }) {
  return (
    <main id="main" className="min-h-dvh bg-canvas">
      {children}
    </main>
  );
}
