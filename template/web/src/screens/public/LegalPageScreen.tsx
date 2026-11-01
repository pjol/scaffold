"use client";

import type { LegalDocumentKind } from "@scaffoldapp/shared";
import { LegalDocument } from "@scaffoldapp/ui-web";
import { useLegalDocument } from "@/components/legal/useLegalDocuments";

/** /legal/terms and /legal/privacy: the bundled document, readable signed out. */
export function LegalPageScreen({ kind }: { kind: LegalDocumentKind }) {
  const doc = useLegalDocument(kind);
  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <LegalDocument document={doc} updateHash />
    </div>
  );
}
