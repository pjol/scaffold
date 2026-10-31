import { PRIVACY_POLICY } from "./privacy";
import { TERMS_OF_SERVICE } from "./terms";
import type { LegalDocument, LegalDocumentKind } from "./types";

export * from "./markdown";
export * from "./privacy";
export * from "./terms";
export * from "./types";
export * from "./versions";

/** Both documents by kind (consent screen, legal pages). */
export const LEGAL_DOCUMENTS: Readonly<Record<LegalDocumentKind, LegalDocument>> = {
  terms: TERMS_OF_SERVICE,
  privacy: PRIVACY_POLICY,
};

/** `getLegalDocument("terms")` → the bundled Terms of Service. */
export function getLegalDocument(kind: LegalDocumentKind): LegalDocument {
  return LEGAL_DOCUMENTS[kind];
}
