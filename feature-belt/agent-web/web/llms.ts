/**
 * /llms.txt (llmstxt.org): the app in one screen for agents, with links to every
 * public page's Markdown twin, the MCP endpoint and the tools. /llms-full.txt adds the
 * full Markdown of each page.
 */
import type { AgentWebManifest } from "@scaffoldapp/shared";
import { APP_NAME } from "@/lib/app";
import { MD_UNAVAILABLE, mdLink, mdList, mdSection } from "./md";
import { agentWebListing, agentWebPages } from "./pages";
import { AGENT_WEB_SITE } from "./site";

const SITE_TOOLS = ["site_current_page", "site_list_pages", "site_read_page", "site_open_page"];

function pagesSection(origin: string): string {
  const lines = agentWebPages
    .filter((p) => !p.unlisted)
    .flatMap(agentWebListing)
    .map((l) => `${mdLink(l.title, `${origin}${l.path === "/" ? "/index" : l.path}.md`)}: ${l.summary}`);
  return mdSection("Pages", mdList(lines));
}

function agentsSection(m: AgentWebManifest | null): string {
  if (!m) return mdSection("API and agents", MD_UNAVAILABLE);
  const reads = m.tools.filter((t) => t.read_only).map((t) => `\`${t.name}\``);
  const writes = m.tools.filter((t) => !t.read_only).map((t) => `\`${t.name}\``);
  const lines: string[] = [];
  if (m.mcp) {
    lines.push(
      `${mdLink("MCP server", m.mcp.endpoint)}: streamable HTTP. Tools act as the person who connects (OAuth sign-in or a personal API key) with their permissions. Read tools: ${reads.join(", ") || "none"}.${writes.length ? ` Tools that change data (write access): ${writes.join(", ")}.` : ""}`,
      `${mdLink("MCP server card", m.mcp.server_card)}: endpoints, auth methods and every tool.`,
    );
  }
  lines.push("Every page above also answers with Markdown for `Accept: text/markdown`, or with `.md` appended to its path.");
  if (m.webmcp) {
    lines.push(
      `In a signed-in browser tab the page registers WebMCP tools (\`document.modelContext\`): ${SITE_TOOLS.map((t) => `\`${t}\``).join(", ")}${reads.length ? `, and the read tools ${reads.join(", ")}` : ""}. They run as the signed-in person; nothing they do changes data.`,
    );
  }
  return mdSection("API and agents", mdList(lines));
}

export function llmsTxt(origin: string, manifest: AgentWebManifest | null): string {
  const parts = [`# ${APP_NAME}`, `> ${AGENT_WEB_SITE.summary}`];
  if (AGENT_WEB_SITE.about) parts.push(AGENT_WEB_SITE.about.trim());
  parts.push(pagesSection(origin), agentsSection(manifest));
  if (AGENT_WEB_SITE.conventions.length) parts.push(mdSection("Conventions", mdList(AGENT_WEB_SITE.conventions)));
  parts.push(mdSection("Optional", mdList([`${mdLink("Every page in full", `${origin}/llms-full.txt`)}: the Markdown of each page above in one file`])));
  return `${parts.join("\n\n")}\n`;
}

/** /llms-full.txt: llms.txt without its Optional section, then each listed page in full. */
export async function llmsFullTxt(origin: string, manifest: AgentWebManifest | null): Promise<string> {
  const head = llmsTxt(origin, manifest).replace(/\n## Optional\n[\s\S]*$/, "\n");
  const docs: string[] = [];
  for (const p of agentWebPages) {
    if (p.unlisted) continue;
    for (const { path } of agentWebListing(p)) {
      const params: Record<string, string> = {};
      const segs = p.path.split("/");
      path.split("/").forEach((v, i) => {
        const s = segs[i];
        if (s?.startsWith(":")) params[s.slice(1)] = v;
      });
      let body: string | null = null;
      try {
        body = await p.render({ params, origin });
      } catch {
        body = null;
      }
      docs.push(`<!-- ${origin}${path} -->\n\n${(body ?? MD_UNAVAILABLE).trim()}`);
    }
  }
  return `${head.trimEnd()}\n\n---\n\n${docs.join("\n\n---\n\n")}\n`;
}
