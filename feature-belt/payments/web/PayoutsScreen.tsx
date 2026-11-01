"use client";

import { errorMessage, isConnectDisabled, isPaymentsUnconfigured, useConnectOnboarding, useConnectedAccount } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, Divider, EmptyState, ErrorState, ListRow, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Banknote, Check, ExternalLink, X } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { PageHeader } from "@/components/shell/PageHeader";
import { PaymentsUnavailable } from "./PaymentsUnavailable";

function Flag({ on, label }: { on: boolean; label: string }) {
  return (
    <ListRow
      title={label}
      trailing={
        on ? (
          <Badge tone="success" icon={<Check />}>
            Done
          </Badge>
        ) : (
          <Badge tone="neutral" icon={<X />}>
            Not yet
          </Badge>
        )
      }
    />
  );
}

/**
 * `/payments/payouts` (Connect): set up or finish the payout account on the
 * provider's hosted onboarding. The provider sends people back here with
 * `?onboarding=return` (done or paused) or `?onboarding=refresh` (the link expired).
 */
export function PayoutsScreen() {
  const params = useSearchParams();
  const back = params.get("onboarding");
  const acct = useConnectedAccount();
  const onboarding = useConnectOnboarding();
  const [leaving, setLeaving] = useState(false);
  const start = () =>
    onboarding.mutate(
      { return_path: "/payments/payouts" },
      {
        onSuccess: (res) => {
          setLeaving(true);
          window.location.assign(res.url);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't open payout setup. Try again.")),
      },
    );
  const busy = onboarding.isPending || leaving;

  let body;
  if (acct.isPending) {
    body = (
      <SkeletonGroup className="flex flex-col gap-4">
        <Skeleton className="h-40" />
      </SkeletonGroup>
    );
  } else if (acct.isError) {
    body = isPaymentsUnconfigured(acct.error) ? (
      <PaymentsUnavailable />
    ) : isConnectDisabled(acct.error) ? (
      <EmptyState icon={<Banknote />} title="Payouts aren't turned on" />
    ) : (
      <ErrorState title="Couldn't load payouts" info={acct.error.message} onRetry={() => void acct.refetch()} retrying={acct.isFetching} />
    );
  } else if (!acct.data.account) {
    body = (
      <EmptyState
        icon={<Banknote />}
        tone="primary"
        title="No payout account yet"
        info="Setup happens on the payment provider's secure page: identity details and a bank account."
        action={
          <Button size="auto" loading={busy} onClick={start} rightIcon={<ExternalLink />}>
            Set up payouts
          </Button>
        }
      />
    );
  } else {
    const a = acct.data.account;
    const ready = a.charges_enabled && a.payouts_enabled;
    body = (
      <div className="flex flex-col gap-4">
        {back === "refresh" && !ready && <Banner tone="info">That setup link expired. Continue where you left off.</Banner>}
        <Card padding="none" className="p-1">
          <Flag on={a.details_submitted} label="Details submitted" />
          <Divider className="mx-4" />
          <Flag on={a.charges_enabled} label="Can take payments" />
          <Divider className="mx-4" />
          <Flag on={a.payouts_enabled} label="Payouts to your bank" />
        </Card>
        <Button variant={ready ? "secondary" : "primary"} size="auto" className="self-start" loading={busy} onClick={start} rightIcon={<ExternalLink />}>
          {ready ? "Update payout details" : "Continue setup"}
        </Button>
      </div>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-form flex-col gap-section px-gutter py-section">
      <PageHeader title="Payouts" />
      {body}
    </div>
  );
}
