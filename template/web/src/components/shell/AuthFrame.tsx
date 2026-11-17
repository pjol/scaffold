import type { ReactNode } from "react";
import { AppMark } from "./AppMark";
import { LegalLinks } from "./LegalLinks";

/** Frame for sign-in: the name centred at the top, one column, legal links at the foot. No header bar, nav or other actions. */
export function AuthFrame({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col bg-canvas">
      <header className="pt-safe">
        <div className="flex h-topbar items-center justify-center px-gutter">
          <AppMark />
        </div>
      </header>
      <main id="main" className="flex flex-1 flex-col items-center px-gutter pt-section pb-band">
        {children}
      </main>
      <footer className="pb-safe">
        <LegalLinks className="justify-center px-gutter py-4" />
      </footer>
    </div>
  );
}
