/**
 * Markdown building blocks for the agent-readable surfaces (page twins, /llms.txt).
 * Pure functions: no DOM, no fetch.
 */

/** Escape a table cell: pipes and newlines would break the row. */
export function mdCell(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

/** A GitHub-style table. Rows shorter than the head are padded. */
export function mdTable(head: string[], rows: string[][]): string {
  const line = (cells: string[]) => `| ${cells.map(mdCell).join(" | ")} |`;
  return [line(head), `|${head.map(() => "---").join("|")}|`, ...rows.map((r) => line(head.map((_, i) => r[i] ?? "")))].join("\n");
}

/** `[text](url)` with brackets in the text escaped. */
export function mdLink(text: string, url: string): string {
  return `[${text.replace(/[[\]]/g, "\\$&")}](${url.replace(/\s/g, "%20").replace(/\)/g, "%29")})`;
}

/** A bullet list. */
export function mdList(items: string[]): string {
  return items.map((i) => `- ${i}`).join("\n");
}

/** An RFC 3339 time without milliseconds, or "—". */
export function mdTime(value: string | number | Date | null | undefined): string {
  if (value === null || value === undefined || value === "") return "—";
  const d = value instanceof Date ? value : new Date(value);
  return Number.isNaN(d.getTime()) ? "—" : d.toISOString().replace(/\.\d{3}Z$/, "Z");
}

/** A document: title, an optional one-line summary as a quote, then sections joined by blank lines. */
export function mdDoc(title: string, summary: string | null, ...sections: (string | null | undefined | false)[]): string {
  const parts = [`# ${title}`];
  if (summary) parts.push(`> ${summary.replace(/\n/g, " ")}`);
  for (const s of sections) if (s) parts.push(s.trim());
  return `${parts.join("\n\n")}\n`;
}

/** A section: `## heading` and its body. */
export function mdSection(heading: string, body: string): string {
  return `## ${heading}\n\n${body.trim()}`;
}

/** Shown where live data couldn't be loaded. */
export const MD_UNAVAILABLE = "_This part is temporarily unavailable. Try again shortly._";
