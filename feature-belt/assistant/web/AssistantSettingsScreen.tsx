"use client";

import { errorMessage, formatTokenCount, useAssistantStatus, useWithdrawAssistantConsent } from "@scaffoldapp/shared";
import { Button, Card, ConfirmDialog, Divider, ErrorState, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { useState, type ReactNode } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime } from "@/lib/format";
import { ConsentDialog } from "./ConsentDialog";

/** Settings → Assistant: which provider receives your messages, your consent, today's usage. */
export function AssistantSettingsScreen() {
  const status = useAssistantStatus();
  const withdraw = useWithdrawAssistantConsent();
  const [allowing, setAllowing] = useState(false);
  const [withdrawing, setWithdrawing] = useState(false);

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Assistant" />
      {status.isPending ? (
        <SkeletonGroup className="flex flex-col gap-4">
          <Skeleton className="h-32" />
          <Skeleton className="h-32" />
        </SkeletonGroup>
      ) : status.isError ? (
        <ErrorState title="Couldn't load assistant settings" onRetry={() => void status.refetch()} retrying={status.isFetching} />
      ) : (
        <>
          <Section title="AI provider" info="Your messages, and the app data the assistant reads to answer them, are sent to this company to write replies.">
            <Card padding="none">
              <Row label="Provider" value={status.data.provider?.label ?? "Not configured"} />
              {status.data.provider ? (
                <>
                  <Divider className="mx-4" />
                  <Row
                    label="Permission"
                    value={status.data.consent.required ? "Not allowed" : `Allowed ${formatDate(status.data.consent.accepted_at ?? "")}`}
                    action={
                      status.data.consent.required ? (
                        <Button size="sm" variant="secondary" onClick={() => setAllowing(true)}>
                          Allow
                        </Button>
                      ) : (
                        <Button size="sm" variant="danger-ghost" onClick={() => setWithdrawing(true)}>
                          Turn off
                        </Button>
                      )
                    }
                  />
                </>
              ) : null}
            </Card>
          </Section>
          <Section title="Today" info="Limits reset at midnight UTC.">
            <Card padding="none">
              <Row
                label="Messages"
                value={status.data.usage.unlimited || status.data.usage.message_limit === 0 ? String(status.data.usage.messages) : `${status.data.usage.messages} of ${status.data.usage.message_limit}`}
              />
              <Divider className="mx-4" />
              <Row
                label="Tokens"
                value={
                  status.data.usage.unlimited || status.data.usage.token_limit === 0
                    ? formatTokenCount(status.data.usage.tokens)
                    : `${formatTokenCount(status.data.usage.tokens)} of ${formatTokenCount(status.data.usage.token_limit)}`
                }
              />
              <Divider className="mx-4" />
              <Row label="Resets" value={formatDateTime(status.data.usage.resets_at)} />
            </Card>
          </Section>
          {status.data.provider ? <ConsentDialog provider={status.data.provider} open={allowing} onOpenChange={setAllowing} onAccepted={() => toast.success("Assistant turned on")} /> : null}
          <ConfirmDialog
            open={withdrawing}
            onOpenChange={setWithdrawing}
            title="Turn off the assistant?"
            confirmLabel="Turn off"
            tone="danger"
            onConfirm={async () => {
              await withdraw.mutateAsync();
              toast.success("Assistant turned off");
            }}
            errorText={(e) => errorMessage(e, "Couldn't save your choice. Try again.")}
          >
            Nothing more is sent to {status.data.provider?.label ?? "the provider"} until you allow it again. Your conversations stay until you delete them.
          </ConfirmDialog>
        </>
      )}
    </div>
  );
}

function Row({ label, value, action }: { label: string; value: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex min-h-row items-center gap-3 px-4 py-2">
      <span className="flex-1 type-body text-fg-muted">{label}</span>
      <span className="type-body text-fg tabular-nums">{value}</span>
      {action}
    </div>
  );
}
