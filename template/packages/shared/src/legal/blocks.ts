import type { LegalBlock, LegalBullets, LegalParagraph, LegalSection } from "./types";

// Shorthand constructors for the document sources (internal; not exported from the package).
export const p = (text: string): LegalParagraph => ({ type: "paragraph", text });
export const ul = (...items: string[]): LegalBullets => ({ type: "bullets", items });
export const section = (id: string, heading: string, ...blocks: LegalBlock[]): LegalSection => ({ id, heading, blocks });
