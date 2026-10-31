"use client";

import { errorMessage, useAcceptConsents, useConsentGate } from "@scaffoldapp/shared";
import { Banner, Button, ConsentForm, InfoTip } from "@scaffoldapp/ui-web";
import { LogOut, RotateCw } from "lucide-react";
import { useState } from "react";
import { SupportLinks } from "@/components/account/SupportLinks";
import { useSignOut } from "@/components/auth/useSignOut";
import { CenteredScreen } from "@/components/shell/CenteredScreen";
import { APP_NAME } from "@/lib/app";
import { useLegalDocuments } from "./useLegalDocuments";

/**
 * Shown instead of the app while `me.consent.required` (first sign-in, or after either
 * document's version changes). Accepts exactly the versions bundled in this build: if
 * the server has moved on, it answers 409 and the banner asks for a reload.
 */
export function ConsentScreen() {
  const gate = useConsentGate();
  const accept = useAcceptConsents();
  const { terms, privacy } = useLegalDocuments();
  const signOut = useSignOut();
  const [leaving, setLeaving] = useState(false);

  return (
    <CenteredScreen>
      <div className="flex flex-col gap-6">
        <h1 className="flex items-center gap-2 type-title text-fg">
          Before you start
          <InfoTip size="md" content={`${APP_NAME} needs your agreement to its Terms of Service and Privacy Policy. Select either name to read it.`} />
        </h1>
        {!gate.bundledVersionsMatch && (
          <Banner
            tone="warning"
            action={
              <Button size="sm" variant="secondary" leftIcon={<RotateCw />} onClick={() => window.location.reload()}>
                Reload
              </Button>
            }
          >
            Updated terms are available.
          </Banner>
        )}
        <ConsentForm
          terms={terms}
          privacy={privacy}
          errorText={(e) => errorMessage(e, "Couldn't save your agreement. Try again.")}
          onSubmit={(v) => accept.mutateAsync({ terms_version: v.termsVersion, privacy_version: v.privacyVersion })}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            leftIcon={<LogOut />}
            loading={leaving}
            onClick={() => {
              setLeaving(true);
              void signOut().catch(() => setLeaving(false));
            }}
          >
            Sign out
          </Button>
          <SupportLinks variant="buttons" />
        </div>
      </div>
    </CenteredScreen>
  );
}
