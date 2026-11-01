"use client";

import { errorMessage, useAcceptAssistantConsent, type AssistantProvider } from "@scaffoldapp/shared";
import { Button, Dialog, toast } from "@scaffoldapp/ui-web";
import Link from "next/link";

/**
 * Asks once, before the first message, to send the user's content to the named AI provider
 * (Apple 5.1.2(i)). The choice is stored on the server; Settings → Assistant turns it off.
 */
export function ConsentDialog({ provider, open, onOpenChange, onAccepted }: { provider: AssistantProvider; open: boolean; onOpenChange: (open: boolean) => void; onAccepted: () => void }) {
  const accept = useAcceptAssistantConsent();
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Use the assistant?"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Not now
          </Button>
          <Button
            loading={accept.isPending}
            onClick={() =>
              accept.mutate(provider.name, {
                onSuccess: () => {
                  onOpenChange(false);
                  onAccepted();
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't save your choice. Try again.")),
              })
            }
          >
            Allow
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p>
          To answer, the assistant sends your messages, and the app data it reads for you, to <strong className="font-semibold">{provider.label}</strong>, which writes the replies.
        </p>
        <p className="text-fg-muted">
          You can turn this off in Settings. Read the{" "}
          <Link href="/legal/privacy" className="text-primary-strong underline underline-offset-2">
            Privacy Policy
          </Link>
          .
        </p>
      </div>
    </Dialog>
  );
}
