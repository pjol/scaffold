import { mdDoc, mdLink } from "@/features/agent-web/md";
import { findAgentWebPage } from "@/features/agent-web/match";
import { agentWebMarkdown, agentWebOrigin } from "@/features/agent-web/server";

/**
 * Markdown twins. The proxy rewrites `<page>.md` and `Accept: text/markdown` requests
 * for public pages here (web/src/features/agent-web/pages.ts lists them).
 */
export async function GET(req: Request, ctx: { params: Promise<{ path?: string[] }> }) {
  const { path = [] } = await ctx.params;
  const target = `/${path.map((s) => encodeURIComponent(s)).join("/")}`;
  const origin = agentWebOrigin(req);
  const notFound = () =>
    agentWebMarkdown(mdDoc("Not found", null, `There is no Markdown version of ${target}. ${mdLink("llms.txt", `${origin}/llms.txt`)} lists the pages that have one.`), {
      status: 404,
      maxAge: 60,
    });
  const hit = findAgentWebPage(target);
  if (!hit) return notFound();
  const body = await hit.page.render({ params: hit.params, origin });
  if (body === null) return notFound();
  return agentWebMarkdown(body, { maxAge: hit.page.maxAge ?? 60 });
}
