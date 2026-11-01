"use client";

import { formatMinor, isPaymentsUnconfigured, useCheckoutStatus } from "@scaffoldapp/shared";
import { Button, Celebrate, EmptyState, ErrorState, Spinner } from "@scaffoldapp/ui-web";
import { CircleCheck, CircleSlash, Clock, SearchX } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { PaymentsUnavailable } from "./PaymentsUnavailable";

const GIVE_UP_MS = 3 * 60_000;

function safeNext(raw: string | null): string | null {
  return raw && raw.startsWith("/") && !raw.startsWith("//") ? raw : null;
}

function Frame({ children }: { children: ReactNode }) {
  return <div className="mx-auto flex w-full max-w-form flex-col gap-section px-gutter py-section">{children}</div>;
}

/**
 * `/payments/success?checkout_id=…`: where the provider sends the payer. The page
 * polls the session until the webhook confirms it (usually seconds); it never trusts
 * the redirect alone.
 */
export function CheckoutSuccessScreen() {
  const params = useSearchParams();
  const id = params.get("checkout_id");
  const next = safeNext(params.get("next"));
  const status = useCheckoutStatus(id);
  const [gaveUp, setGaveUp] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setGaveUp(true), GIVE_UP_MS);
    return () => clearTimeout(t);
  }, []);

  if (!id) {
    return (
      <Frame>
        <EmptyState
          icon={<SearchX />}
          title="No checkout to show"
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href="/settings/billing">Go to billing</Link>
            </Button>
          }
        />
      </Frame>
    );
  }
  if (status.isError) {
    return <Frame>{isPaymentsUnconfigured(status.error) ? <PaymentsUnavailable /> : <ErrorState title="Couldn't check your payment" info={status.error.message} onRetry={() => void status.refetch()} retrying={status.isFetching} />}</Frame>;
  }
  const s = status.data;
  if (!s || (s.status === "pending" && !gaveUp)) {
    return (
      <Frame>
        <div role="status" className="flex flex-col items-center gap-4 py-16 text-center">
          <Spinner size={32} label="Confirming your payment" />
          <h1 className="type-title-sm text-fg">Confirming your payment</h1>
        </div>
      </Frame>
    );
  }
  if (s.status === "pending") {
    return (
      <Frame>
        <EmptyState
          icon={<Clock />}
          title="Still confirming your payment"
          info="The payment provider hasn't confirmed it yet. Billing updates on its own once it does; you won't be charged twice."
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href="/settings/billing">Go to billing</Link>
            </Button>
          }
        />
      </Frame>
    );
  }
  if (s.status === "completed") {
    return (
      <Frame>
        <div className="flex flex-col items-center gap-6 py-16 text-center">
          <Celebrate fire={s.id} mode="fountain">
            <span className="flex size-16 items-center justify-center rounded-xl bg-success-soft text-success-strong [&_svg]:size-8">
              <CircleCheck />
            </span>
          </Celebrate>
          <div className="flex flex-col gap-2">
            <h1 className="type-title text-fg">Payment received</h1>
            {s.amount_minor !== null && <p className="type-body text-fg-muted tabular-nums">{formatMinor(s.amount_minor, s.currency)}</p>}
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Button asChild size="auto">
              <Link href={next ?? "/"}>Continue</Link>
            </Button>
            <Button asChild variant="secondary" size="auto">
              <Link href="/settings/billing">Go to billing</Link>
            </Button>
          </div>
        </div>
      </Frame>
    );
  }
  return (
    <Frame>
      <EmptyState
        icon={<CircleSlash />}
        title="The payment didn't go through"
        info={s.status === "expired" ? "The checkout page expired before payment." : "The checkout couldn't be completed. If you were charged, contact support and it will be refunded."}
        action={
          <Button asChild size="auto">
            <Link href="/payments">Back to plans</Link>
          </Button>
        }
      />
    </Frame>
  );
}

/** `/payments/cancel`: the payer left checkout. Nothing was charged. */
export function CheckoutCanceledScreen() {
  return (
    <Frame>
      <EmptyState
        icon={<CircleSlash />}
        title="Checkout canceled"
        info="You weren't charged."
        action={
          <Button asChild size="auto">
            <Link href="/payments">Back to plans</Link>
          </Button>
        }
      />
    </Frame>
  );
}
