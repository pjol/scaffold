/**
 * Structured legal documents, rendered natively by every client and exported to
 * Markdown (`docs/legal/*.md`). Text is plain (no inline markup); sections render in
 * order, numbered 1…n.
 */

export type LegalDocumentKind = "terms" | "privacy";

export interface LegalParagraph {
  type: "paragraph";
  text: string;
}

export interface LegalBullets {
  type: "bullets";
  items: string[];
}

export type LegalBlock = LegalParagraph | LegalBullets;

export interface LegalSection {
  /** Stable anchor (kebab-case), e.g. `deleting-your-account`. */
  id: string;
  heading: string;
  blocks: LegalBlock[];
}

export interface LegalDocument {
  kind: LegalDocumentKind;
  /** Must equal `ClientConfig.terms_version` / `privacy_version` for consent. */
  version: string;
  title: string;
  /** Effective date, `YYYY-MM-DD`. */
  effective: string;
  sections: LegalSection[];
}

/**
 * Placeholders the documents contain where only the operator knows the value. Replace
 * them in the source before launch, or at render time with {@link applyLegalPlaceholders}.
 */
export const LEGAL_PLACEHOLDERS = ["[Company legal name]", "[Company address]", "[Contact email]", "[Governing jurisdiction]", "[Hosting provider]"] as const;
export type LegalPlaceholder = (typeof LEGAL_PLACEHOLDERS)[number];

/** A copy of `doc` with placeholders replaced (unlisted ones are left as-is). */
export function applyLegalPlaceholders(doc: LegalDocument, values: Partial<Record<LegalPlaceholder, string>>): LegalDocument {
  const entries = Object.entries(values).filter(([, v]) => typeof v === "string" && v.length > 0) as [string, string][];
  if (entries.length === 0) return doc;
  const fill = (text: string) => entries.reduce((acc, [k, v]) => acc.split(k).join(v), text);
  return {
    ...doc,
    sections: doc.sections.map((s) => ({
      ...s,
      heading: fill(s.heading),
      blocks: s.blocks.map((b): LegalBlock => (b.type === "paragraph" ? { type: "paragraph", text: fill(b.text) } : { type: "bullets", items: b.items.map(fill) })),
    })),
  };
}

/** Placeholders still present in `doc` (empty = ready to publish). */
export function unfilledPlaceholders(doc: LegalDocument): LegalPlaceholder[] {
  const text = doc.sections.map((s) => [s.heading, ...s.blocks.flatMap((b) => (b.type === "paragraph" ? [b.text] : b.items))].join("\n")).join("\n");
  return LEGAL_PLACEHOLDERS.filter((p) => text.includes(p));
}
