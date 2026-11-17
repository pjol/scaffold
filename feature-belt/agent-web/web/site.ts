/**
 * What agents read first about this app (/llms.txt, the home page twin). Rewrite both for
 * the product: the summary is one sentence; `about` says who it's for and the facts an
 * agent needs to use it correctly (units, limits, what's demo data). Plain Markdown.
 */
export const AGENT_WEB_SITE = {
  summary: "Scaffold App, on the web and on phones.",
  about: "",
  /** Conventions agents should know (one per line, Markdown bullets without the dash), or none. */
  conventions: [] as string[],
};
