import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";

/**
 * Serves MapLibre's web worker from the installed package (places feature). Bundlers
 * can't resolve the worker URL MapLibre derives from `import.meta.url`, so MapView calls
 * `setWorkerUrl("/maplibre-gl-worker.mjs?v=<version>")`; the version query makes the
 * long cache safe across upgrades. Static: `next build` reads the file once.
 */
export const dynamic = "force-static";

// Built from parts so the bundler leaves the path to Node. The workspace hoists
// node_modules to the repo root, so look in web/ and its parents.
const WORKER = ["node_modules", "maplibre-gl", "dist", "maplibre-gl-worker.mjs"];

async function workerSource(): Promise<string> {
  let dir = process.cwd();
  for (let i = 0; i < 4; i++) {
    try {
      return await readFile(join(dir, ...WORKER), "utf8");
    } catch {
      const up = dirname(dir);
      if (up === dir) break;
      dir = up;
    }
  }
  throw new Error("maplibre-gl is not installed (pnpm install)");
}

export async function GET() {
  return new Response(await workerSource(), {
    headers: { "Content-Type": "text/javascript; charset=utf-8", "Cache-Control": "public, max-age=31536000, immutable" },
  });
}
