"use client";

import { useConfig } from "@scaffoldapp/shared";
import { Button, Card, ErrorState, IconButton, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Copy, Mail } from "lucide-react";

/** The published support address (`/v1/config` → SUPPORT_EMAIL) with email and copy actions. */
export function SupportContact() {
  const config = useConfig();
  if (config.isPending) {
    return (
      <SkeletonGroup label="Loading support contact">
        <Card className="flex items-center justify-between gap-4">
          <Skeleton className="h-6 w-56" />
          <Skeleton className="h-10 w-32" />
        </Card>
      </SkeletonGroup>
    );
  }
  if (config.isError) {
    return (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load the support address" info={config.error.message} onRetry={() => void config.refetch()} retrying={config.isFetching} />
      </Card>
    );
  }
  const email = config.data.support_email;
  return (
    <Card className="flex flex-col items-start gap-4 sm:flex-row sm:items-center sm:justify-between">
      <span className="min-w-0 break-all type-body font-semibold text-fg">{email}</span>
      <div className="flex items-center gap-2">
        <IconButton
          label="Copy address"
          tooltip
          icon={<Copy />}
          variant="ghost"
          onClick={() => {
            void navigator.clipboard?.writeText(email).then(
              () => toast.success("Copied"),
              () => toast.error("Couldn't copy. Select the address instead."),
            );
          }}
        />
        <Button asChild size="auto" leftIcon={<Mail />}>
          <a href={`mailto:${email}`}>Email support</a>
        </Button>
      </div>
    </Card>
  );
}
