import { cn } from "@scaffoldapp/ui-web";
import type { ReactNode } from "react";
import { AppMark } from "./AppMark";
import { LegalLinks } from "./LegalLinks";

/** Full-screen frame for steps outside the shell (consent, blocked account): the app's name, one column, legal links. */
export function CenteredScreen({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className="flex min-h-dvh flex-col items-center bg-canvas px-gutter pt-safe pb-safe">
      <main className={cn("flex w-full max-w-md flex-1 flex-col justify-center gap-8 py-section", className)}>
        <AppMark className="self-start" />
        {children}
      </main>
      <LegalLinks className="justify-center pb-4" />
    </div>
  );
}
