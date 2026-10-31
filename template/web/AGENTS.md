# web/

The Next.js app. Read `~/scaffold/concrete/web.md` (structure, providers, gates, data rules, registry) before changing anything here.

This is Next.js 16 with the App Router: APIs and conventions differ from older versions (`proxy.ts` replaces `middleware.ts`; `params` are promises; error boundaries get `retry`). Read the relevant guide in `node_modules/next/dist/docs/` (from the workspace root) before writing Next-specific code, and heed deprecation notices.
