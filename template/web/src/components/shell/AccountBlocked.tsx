"use client";

import { errorMessage, hasErrorCode, isUnauthenticated, useConfig, type ApiError } from "@scaffoldapp/shared";
import { Button, ErrorState } from "@scaffoldapp/ui-web";
import { LogIn, LogOut, Mail, MailCheck, ShieldOff } from "lucide-react";
import { useState } from "react";
import { SupportLinks } from "@/components/account/SupportLinks";
import { useSignOut } from "@/components/auth/useSignOut";
import { CenteredScreen } from "./CenteredScreen";

/** `GET /v1/me` failed: banned, unverified email, expired session, or the API is unreachable. */
export function AccountBlocked({ error, onRetry, retrying }: { error: ApiError; onRetry: () => void; retrying: boolean }) {
  const signOut = useSignOut();
  const config = useConfig();
  const [leaving, setLeaving] = useState(false);
  const leave = () => {
    setLeaving(true);
    void signOut().catch(() => setLeaving(false));
  };
  const support = config.data?.support_email;

  let title: string;
  let icon = <ShieldOff aria-hidden />;
  let retry = false;
  if (hasErrorCode(error, "account_suspended")) {
    title = "Your account is suspended";
  } else if (hasErrorCode(error, "email_unverified")) {
    title = "Verify your email address to continue";
    icon = <MailCheck aria-hidden />;
  } else if (isUnauthenticated(error)) {
    title = "Your session has ended";
    icon = <LogIn aria-hidden />;
  } else {
    title = "Couldn't load your account";
    retry = true;
  }

  return (
    <CenteredScreen>
      <div className="flex flex-col items-center gap-4">
        <ErrorState
          title={title}
          info={errorMessage(error)}
          {...(retry ? { onRetry, retrying } : {})}
          {...(retry ? {} : { icon })}
          compact
          className="py-0"
        />
        <div className="flex flex-col flex-wrap items-center justify-center gap-2 sm:flex-row">
          {support && !retry && (
            <Button asChild variant="secondary" size="auto" leftIcon={<Mail />}>
              <a href={`mailto:${support}`}>Email support</a>
            </Button>
          )}
          {!retry && <SupportLinks variant="buttons" />}
          <Button variant="ghost" size="auto" leftIcon={isUnauthenticated(error) ? <LogIn /> : <LogOut />} loading={leaving} onClick={leave}>
            {isUnauthenticated(error) ? "Sign in again" : "Sign out"}
          </Button>
        </div>
      </div>
    </CenteredScreen>
  );
}
