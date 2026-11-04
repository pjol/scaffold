import { clerkMiddleware } from "@clerk/nextjs/server";
import { NextResponse, type NextFetchEvent, type NextRequest } from "next/server";
import { AUTH_MODE } from "./src/lib/env";

/**
 * Next 16 renamed `middleware.ts` to `proxy.ts`. With a Clerk publishable key this runs
 * Clerk's middleware and sends signed-out visitors of app routes to /sign-in. The API is
 * the authority on access; this only spares a round trip. Without a key, Clerk is never
 * involved and every request passes through.
 *
 * Public paths: sign-in, legal documents, the account-deletion page (Play's web URL),
 * support, the favicon and the logos in public/brand/ (the matcher below skips most
 * static files, but not SVGs). Each entry is a regular-expression source matched against the
 * whole pathname (escape dots). Features with public pages add one line each above the
 * anchor comment at the end of the list, e.g. `"/invites/(.*)",`.
 */
const PUBLIC_PATHS: readonly string[] = [
  "/sign-in(/.*)?",
  "/legal(/.*)?",
  "/delete-account",
  "/support",
  "/mark\\.svg",
  "/brand/(.*)",
  // scaffold:public-paths
];
const PUBLIC = PUBLIC_PATHS.map((p) => new RegExp(`^${p}$`));
const isPublic = (req: NextRequest) => PUBLIC.some((re) => re.test(req.nextUrl.pathname));

const clerk = clerkMiddleware(
  async (auth, req) => {
    if (!isPublic(req)) await auth.protect();
  },
  { signInUrl: "/sign-in" },
);

export default function proxy(req: NextRequest, event: NextFetchEvent) {
  if (AUTH_MODE !== "clerk") return NextResponse.next();
  return clerk(req, event);
}

export const config = {
  matcher: [
    // Everything except Next internals and static files (unless found in search params).
    "/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|ico|ttf|woff2?|csv|docx?|xlsx?|zip|webmanifest)).*)",
  ],
};
