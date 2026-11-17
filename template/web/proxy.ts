import { clerkMiddleware } from "@clerk/nextjs/server";
import { NextResponse, type NextFetchEvent, type NextRequest } from "next/server";
import { AUTH_MODE } from "./src/lib/env";
// scaffold:imports

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

/**
 * Handlers features run on every matched request before auth, in order: each returns a
 * response (usually a rewrite) or null to pass it on. A feature adds one entry above the
 * anchor, e.g. `agentWebProxy,` (Markdown twins of public pages). Keep them cheap and
 * never let one answer a non-public path with private data: auth hasn't run yet.
 */
const PROXY_HANDLERS: readonly ((req: NextRequest) => NextResponse | null)[] = [
  // scaffold:proxy
];

const clerk = clerkMiddleware(
  async (auth, req) => {
    if (!isPublic(req)) await auth.protect();
  },
  { signInUrl: "/sign-in" },
);

export default function proxy(req: NextRequest, event: NextFetchEvent) {
  for (const handle of PROXY_HANDLERS) {
    const res = handle(req);
    if (res) return res;
  }
  if (AUTH_MODE !== "clerk") return NextResponse.next();
  return clerk(req, event);
}

export const config = {
  matcher: [
    // Everything except Next internals and static files (unless found in search params).
    "/((?!_next|[^?]*\\.(?:html?|css|js(?!on)|jpe?g|webp|png|gif|ico|ttf|woff2?|csv|docx?|xlsx?|zip|webmanifest)).*)",
  ],
};
