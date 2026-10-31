"use client";

import {
  errorMessage,
  formatMinor,
  isConnectDisabled,
  isPaymentsUnconfigured,
  useConnectedAccount,
  useEntitlements,
  usePaymentHistory,
  usePortal,
  usePrices,
  useSubscription,
  type PaymentLedgerEntry,
  type PaymentSubscription,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, Divider, EmptyState, ErrorState, ListRow, Skeleton, SkeletonGroup, SkeletonRow, cn, toast } from "@scaffoldapp/ui-web";
import { Banknote, ExternalLink, Receipt, Undo2 } from "lucide-react";
import Link from "next/link";
import { Fragment, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { SUBSCRIPTION_STATUS, subscriptionDateLine } from "./labels";
import { PaymentsUnavailable } from "./PaymentsUnavailable";

function usePortalButton() {
  const portal = usePortal();
  const [leaving, setLeaving] = useState(false);
  const open = () =>
    portal.mutate(
      { return_path: "/settings/billing" },
      {
        onSuccess: (res) => {
          setLeaving(true);
          window.location.assign(res.url);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't open billing. Try again.")),
      },
    );
  return { open, busy: portal.isPending || leaving };
}

function PlanCard({ sub }: { sub: PaymentSubscription }) {
  const status = SUBSCRIPTION_STATUS[sub.status];
  const line = subscriptionDateLine(sub);
  const portal = usePortalButton();
  const ended = sub.status === "canceled" || sub.status === "incomplete_expired";
  const [when, date] = line ? splitDateLine(line) : [null, null];
  return (
    <div className="flex flex-col gap-4">
      {(sub.status === "past_due" || sub.status === "unpaid") && (
        <Banner
          tone="warning"
          action={
            <Button size="sm" variant="secondary" loading={portal.busy} onClick={portal.open}>
              Update card
            </Button>
          }
        >
          Your last payment failed. Update your card to keep your plan.
        </Banner>
      )}
      <Card padding="none" className="p-1">
        <ListRow title={sub.quantity > 1 ? `${sub.label} × ${sub.quantity}` : sub.label} trailing={<Badge tone={status.tone}>{status.label}</Badge>} />
        {when && (
          <>
            <Divider className="mx-4" />
            <ListRow title={when} trailing={<span className="type-label text-fg-muted">{date}</span>} />
          </>
        )}
      </Card>
      {ended ? (
        <Button asChild variant="secondary" size="auto" className="self-start">
          <Link href="/payments">See plans</Link>
        </Button>
      ) : (
        <Button variant="secondary" size="auto" className="self-start" rightIcon={<ExternalLink />} loading={portal.busy} onClick={portal.open}>
          Manage billing
        </Button>
      )}
    </div>
  );
}

/** "Renews Nov 30, 2026" → ["Renews", "Nov 30, 2026"]. */
function splitDateLine(line: string): [string, string] {
  const m = /^(Renews|Ends|Ended|Trial ends) (.+)$/.exec(line);
  return m ? [m[1] ?? line, m[2] ?? ""] : [line, ""];
}

function PlanSection() {
  const sub = useSubscription();
  if (sub.isPending) return <Skeleton className="h-24" />;
  if (sub.isError) return <ErrorState compact title="Couldn't load your plan" info={sub.error.message} onRetry={() => void sub.refetch()} retrying={sub.isFetching} />;
  if (!sub.data.subscription) {
    return (
      <Card padding="none">
        <EmptyState
          compact
          icon={<Receipt />}
          title="No plan"
          action={
            <Button asChild size="auto">
              <Link href="/payments">See plans</Link>
            </Button>
          }
        />
      </Card>
    );
  }
  return <PlanCard sub={sub.data.subscription} />;
}

/** What the user holds, named after the catalog price that grants it. */
function AccessSection() {
  const ents = useEntitlements();
  const prices = usePrices();
  if (!ents.data || ents.data.length === 0) return null;
  const name = (key: string) => prices.data?.find((p) => p.entitlement === key)?.label ?? key;
  return (
    <Section title="Access">
      <Card padding="none" className="p-1">
        {ents.data.map((e, i) => (
          <Fragment key={e.key}>
            {i > 0 && <Divider className="mx-4" />}
            <ListRow title={name(e.key)} trailing={<span className="type-label text-fg-muted">{e.active_until ? `Until ${formatDate(e.active_until)}` : "No expiry"}</span>} />
          </Fragment>
        ))}
      </Card>
    </Section>
  );
}

/** Shown only when the server has Connect on. */
function PayoutsSection() {
  const acct = useConnectedAccount();
  if (acct.isPending || (acct.isError && isConnectDisabled(acct.error))) return null;
  const a = acct.data?.account;
  const label = !a ? "Not set up" : a.charges_enabled && a.payouts_enabled ? "On" : "Setup unfinished";
  return (
    <Card padding="none" className="p-1">
        <ListRow
          asChild
          title="Payouts"
          leading={
            <span className="flex size-8 items-center justify-center rounded-md bg-sunken text-fg">
              <Banknote className="size-4" aria-hidden />
            </span>
          }
          trailing={<span className="type-label text-fg-muted">{label}</span>}
        >
          <Link href="/payments/payouts" />
        </ListRow>
    </Card>
  );
}

const shortDate = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

function HistoryRow({ entry }: { entry: PaymentLedgerEntry }) {
  const refund = entry.kind === "refund";
  return (
    <div className="flex min-h-tap-lg items-center gap-3 px-4 py-2">
      <span className={cn("flex size-8 shrink-0 items-center justify-center rounded-md", refund ? "bg-success-soft text-success-strong" : "bg-sunken text-fg")}>
        {refund ? <Undo2 className="size-4" aria-hidden /> : <Receipt className="size-4" aria-hidden />}
      </span>
      <span className="min-w-0 flex-1 truncate type-body font-medium text-fg">{entry.description || (refund ? "Refund" : "Payment")}</span>
      <time dateTime={entry.created_at} className="hidden shrink-0 type-label whitespace-nowrap text-fg-muted sm:block">
        {shortDate.format(new Date(entry.created_at))}
      </time>
      <span className={cn("shrink-0 text-right type-body font-semibold tabular-nums sm:min-w-20", refund ? "text-success-strong" : "text-fg")}>{formatMinor(entry.amount_minor, entry.currency, { signed: refund })}</span>
    </div>
  );
}

function HistorySection() {
  const history = usePaymentHistory();
  const rows = history.data?.items ?? [];
  return (
    <Section title="History">
      <Card padding="none">
        {history.isPending ? (
          <SkeletonGroup className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : history.isError ? (
          <ErrorState compact title="Couldn't load your history" info={history.error.message} onRetry={() => void history.refetch()} retrying={history.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Receipt />} title="No payments yet" />
        ) : (
          <>
            <ul className="flex flex-col">
              {rows.map((e, i) => (
                <li key={e.id}>
                  {i > 0 && <Divider className="mx-4" />}
                  <HistoryRow entry={e} />
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={history.hasNextPage} isFetchingNextPage={history.isFetchingNextPage} fetchNextPage={history.fetchNextPage} />
          </>
        )}
      </Card>
    </Section>
  );
}

/**
 * Settings → Billing: the current plan (status, renewal, a link to the provider's
 * billing portal for cards, invoices, plan changes and cancellation), what the user
 * holds, payouts when Connect is on, and the payment history.
 */
export function BillingScreen() {
  const sub = useSubscription();
  if (sub.isError && isPaymentsUnconfigured(sub.error)) {
    return (
      <div className="flex flex-col gap-section">
        <PageHeader title="Billing" />
        <PaymentsUnavailable />
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Billing" info="Cards, invoices, plan changes and cancellation are handled on the payment provider's secure billing page." />
      <Section title="Plan">
        <PlanSection />
      </Section>
      <AccessSection />
      <PayoutsSection />
      <HistorySection />
    </div>
  );
}
