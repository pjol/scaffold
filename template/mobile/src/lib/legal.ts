import { applyLegalPlaceholders, getLegalDocument, useConfig, type LegalDocument, type LegalDocumentKind } from "@scaffoldapp/shared";
import { useMemo } from "react";

/**
 * A bundled legal document with the contact placeholder filled from `/v1/config`
 * (`support_email`). The other placeholders must be filled in the source before launch.
 */
export function useLegalDocument(kind: LegalDocumentKind): LegalDocument {
  const config = useConfig();
  const email = config.data?.support_email;
  return useMemo(() => {
    const doc = getLegalDocument(kind);
    return email ? applyLegalPlaceholders(doc, { "[Contact email]": email }) : doc;
  }, [kind, email]);
}
