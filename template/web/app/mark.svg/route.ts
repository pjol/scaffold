import { getPalette } from "@scaffoldapp/tokens";
import { APP_NAME } from "@/lib/app";

export const dynamic = "force-static";

/** Favicon: the app's initial on the palette's primary. Replace with the real mark after the design round. */
export function GET() {
  const c = getPalette().colors.light;
  const initial = APP_NAME.trim().charAt(0).toUpperCase() || "A";
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="${c.primary}"/><text x="32" y="44" text-anchor="middle" font-family="Inter, system-ui, sans-serif" font-size="36" font-weight="700" fill="${c.primaryFg}">${initial}</text></svg>`;
  return new Response(svg, { headers: { "Content-Type": "image/svg+xml", "Cache-Control": "public, max-age=86400" } });
}
