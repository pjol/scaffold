/**
 * The Markdown twin of every public page. Each entry names a public route pattern and
 * renders its Markdown on the server (from bundled content or the public API). The
 * proxy answers `<path>.md` and `Accept: text/markdown` requests for these paths with
 * the twin; /llms.txt lists them and /llms-full.txt inlines them.
 *
 * Stitching: add one entry per public page the app or its features have (proxy.ts
 * PUBLIC_PATHS). A twin renders only public data: it runs without a session.
 */
import { getLegalDocument, legalDocumentToMarkdown, type LegalDocumentKind } from "@scaffoldapp/shared/legal";
import { APP_NAME } from "@/lib/app";
import { mdDoc, mdLink, mdList, mdSection } from "./md";
import { agentWebConfig } from "./server";
import { AGENT_WEB_SITE } from "./site";

export interface AgentWebRenderContext {
  /** Values of the pattern's `:params`. */
  params: Record<string, string>;
  /** Site origin, for absolute links (`https://example.com`). */
  origin: string;
}

export interface AgentWebPage {
  /** Route pattern: `/`, `/legal/:doc`, `/book/:slug`. */
  path: string;
  /** Link text in /llms.txt. */
  title: string;
  /** One line in /llms.txt. */
  summary: string;
  /** Concrete pages of a pattern with params, each listed in /llms.txt and inlined in /llms-full.txt. */
  examples?: { path: string; title: string; summary?: string }[];
  /** Leave out of /llms.txt (still served). */
  unlisted?: boolean;
  /** CDN cache seconds. @default 60 */
  maxAge?: number;
  /** The Markdown, or null for "no such page" (404). */
  render: (ctx: AgentWebRenderContext) => Promise<string | null>;
}

/** The concrete pages an entry stands for: itself, or its examples when it has params. */
export function agentWebListing(p: AgentWebPage): { path: string; title: string; summary: string }[] {
  if (!p.path.includes(":")) return [{ path: p.path, title: p.title, summary: p.summary }];
  return (p.examples ?? []).map((e) => ({ path: e.path, title: e.title, summary: e.summary ?? p.summary }));
}

const LEGAL: readonly LegalDocumentKind[] = ["terms", "privacy"];
const isLegal = (v: string | undefined): v is LegalDocumentKind => !!v && (LEGAL as readonly string[]).includes(v);

async function supportLine(): Promise<string> {
  const cfg = await agentWebConfig();
  return cfg?.support_email ? `Email ${mdLink(cfg.support_email, `mailto:${cfg.support_email}`)}.` : "_The support address is temporarily unavailable._";
}

export const agentWebPages: AgentWebPage[] = [
  {
    path: "/",
    title: APP_NAME,
    summary: "What this is and where everything is",
    maxAge: 300,
    render: async ({ origin }) =>
      mdDoc(
        APP_NAME,
        AGENT_WEB_SITE.summary,
        AGENT_WEB_SITE.about,
        mdSection(
          "Pages",
          mdList(
            agentWebPages
              .filter((p) => p.path !== "/" && !p.unlisted)
              .flatMap(agentWebListing)
              .map((l) => `${mdLink(l.title, `${origin}${l.path}.md`)}: ${l.summary}`),
          ),
        ),
        mdSection("For agents", `Start with ${mdLink("llms.txt", `${origin}/llms.txt`)}. Every page above also answers with Markdown for \`Accept: text/markdown\` or with \`.md\` appended.`),
      ),
  },
  {
    path: "/legal/:doc",
    title: "Legal",
    summary: "Terms of Service and Privacy Policy",
    examples: [
      { path: "/legal/terms", title: "Terms of Service", summary: "The rules for using the app" },
      { path: "/legal/privacy", title: "Privacy Policy", summary: "What data is kept, why and for how long" },
    ],
    maxAge: 3600,
    render: async ({ params }) => {
      const doc = params["doc"];
      return isLegal(doc) ? legalDocumentToMarkdown(getLegalDocument(doc)) : null;
    },
  },
  {
    path: "/support",
    title: "Support",
    summary: "How to reach the team",
    maxAge: 600,
    render: async ({ origin }) =>
      mdDoc(
        "Support",
        null,
        await supportLine(),
        mdList([
          mdLink("Terms of Service", `${origin}/legal/terms.md`),
          mdLink("Privacy Policy", `${origin}/legal/privacy.md`),
          mdLink("Delete your account", `${origin}/delete-account.md`),
        ]),
      ),
  },
  {
    path: "/delete-account",
    title: "Delete your account",
    summary: "How to delete an account and what is kept",
    maxAge: 600,
    render: async ({ origin }) =>
      mdDoc(
        `Delete your ${APP_NAME} account`,
        null,
        mdSection(
          "How",
          [`1. Sign in to ${APP_NAME} on the web (${origin}/sign-in) or in the app.`, "2. Open Settings, then Account, then Delete account.", "3. Type DELETE and confirm. The account is deleted immediately."].join("\n"),
        ),
        mdSection(
          "What happens",
          mdList([
            "Deleted immediately: your name, email address, profile photo and uploaded files. You can't sign in to the account again.",
            "Kept without your name or email: the record of when you accepted the Terms of Service and Privacy Policy, which the law requires as proof of consent.",
          ]),
        ),
        mdSection("Can't sign in?", `${await supportLine()} Write from the address on the account.`),
      ),
  },
  // agent-web:pages (add the app's public pages above this line)
];
