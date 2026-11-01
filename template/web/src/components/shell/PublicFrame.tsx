"use client";

import { useAuthSession, useConfig } from "@scaffoldapp/shared";
import { Button } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { signInHref } from "@/lib/redirect";
import { AppMark } from "./AppMark";
import { LegalLinks } from "./LegalLinks";

/** Frame for public pages (sign-in, legal, support, delete account): name, one action, footer with legal links and the support address. */
export function PublicFrame({ children }: { children: ReactNode }) {
  const auth = useAuthSession();
  const pathname = usePathname() ?? "/";
  const config = useConfig();
  const onSignIn = pathname.startsWith("/sign-in");
  return (
    <div className="flex min-h-dvh flex-col bg-canvas">
      <header className="border-b border-border pt-safe">
        <div className="mx-auto flex h-topbar w-full max-w-content items-center justify-between gap-3 px-gutter">
          <AppMark />
          {!auth.isLoaded || onSignIn ? null : auth.isSignedIn ? (
            <Button asChild variant="secondary">
              <Link href="/">Open app</Link>
            </Button>
          ) : (
            <Button asChild variant="secondary">
              <Link href={signInHref(pathname)}>Sign in</Link>
            </Button>
          )}
        </div>
      </header>
      <main id="main" className="flex flex-1 flex-col">
        {children}
      </main>
      <footer className="border-t border-border pb-safe">
        <div className="mx-auto flex w-full max-w-content flex-col gap-2 px-gutter py-4 sm:flex-row sm:items-center sm:justify-between">
          <LegalLinks className="-ml-2" />
          {config.data?.support_email && (
            <a href={`mailto:${config.data.support_email}`} className="flex min-h-control items-center type-label text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
              {config.data.support_email}
            </a>
          )}
        </div>
      </footer>
    </div>
  );
}
