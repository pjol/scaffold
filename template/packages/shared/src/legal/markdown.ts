import type { LegalDocument } from "./types";

const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"] as const;

/** `"2026-10-12"` → `"October 12, 2026"` (no time-zone shifting). */
export function formatLegalDate(value: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  const month = m ? MONTHS[Number(m[2]) - 1] : undefined;
  return m && month ? `${month} ${Number(m[3])}, ${m[1]}` : value;
}

/** Table of contents entries: `[{ id, number, heading }]`. */
export function legalTableOfContents(doc: LegalDocument): { id: string; number: number; heading: string }[] {
  return doc.sections.map((s, i) => ({ id: s.id, number: i + 1, heading: s.heading }));
}

/**
 * Markdown rendering used by `pnpm --filter @scaffoldapp/shared legal:export`
 * (docs/legal/*.md). Deterministic: same document → same bytes.
 */
export function legalDocumentToMarkdown(doc: LegalDocument): string {
  const out: string[] = [];
  out.push(`# ${doc.title}`, "");
  out.push(`Version ${doc.version} · Effective ${formatLegalDate(doc.effective)}`, "");
  out.push("## Contents", "");
  for (const entry of legalTableOfContents(doc)) out.push(`${entry.number}. [${entry.heading}](#${entry.id})`);
  out.push("");
  doc.sections.forEach((s, i) => {
    out.push(`<a id="${s.id}"></a>`, "", `## ${i + 1}. ${s.heading}`, "");
    for (const block of s.blocks) {
      if (block.type === "paragraph") out.push(block.text, "");
      else {
        for (const item of block.items) out.push(`- ${item}`);
        out.push("");
      }
    }
  });
  return `${out.join("\n").trimEnd()}\n`;
}
