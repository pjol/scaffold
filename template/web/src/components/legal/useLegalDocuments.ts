"use client";

import { applyLegalPlaceholders, getLegalDocument, useConfig, type LegalDocument, type LegalDocumentKind } from "@scaffoldapp/shared";
import { useMemo } from "react";

/**
 * A bundled legal document with the contact address filled from `/v1/config`
 * (SUPPORT_EMAIL). The other placeholders stay until the owner fills them in
 * packages/shared/src/legal before launch.
 */
export function useLegalDocument(kind: LegalDocumentKind): LegalDocument {
  const config = useConfig();
  const email = config.data?.support_email;
  return useMemo(() => {
    const doc = getLegalDocument(kind);
    return email ? applyLegalPlaceholders(doc, { "[Contact email]": email }) : doc;
  }, [kind, email]);
}

export function useLegalDocuments(): { terms: LegalDocument; privacy: LegalDocument } {
  return { terms: useLegalDocument("terms"), privacy: useLegalDocument("privacy") };
}
