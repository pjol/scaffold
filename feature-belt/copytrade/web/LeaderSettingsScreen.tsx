"use client";

import {
  copytradeAllocationLabel,
  errorMessage,
  useCopytradeConfig,
  useCopytradeCopiers,
  useMyCopytradeLeader,
  useSetMyCopytradeLeader,
  type CopytradeLeaderSettings,
  type CopytradeLeaderStatus,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";
import { CircleCheck, CircleDashed, Users } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { PersonLine, Stat, useExchangeMoney } from "./parts";

/** Settings → Copy trading: offer your trades to copiers, on your terms. */
export function LeaderSettingsScreen() {
  const status = useMyCopytradeLeader();
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Copy trading" info="Others copy your main account's positions in their own accounts. You never see or touch their money." />
      {status.isPending ? (
        <SkeletonGroup label="Loading" className="flex flex-col gap-3">
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
          <Skeleton className="h-32" />
        </SkeletonGroup>
      ) : status.isError ? (
        <ErrorState compact title="Couldn't load your copy trading settings" info={status.error.message} onRetry={() => void status.refetch()} />
      ) : (
        <LeaderForm key={JSON.stringify(status.data.settings)} status={status.data} />
      )}
    </div>
  );
}

const decimalRe = /^\d+(\.\d+)?$/;

function LeaderForm({ status }: { status: CopytradeLeaderStatus }) {
  const config = useCopytradeConfig();
  const save = useSetMyCopytradeLeader();
  const { money } = useExchangeMoney();
  const [form, setForm] = useState<CopytradeLeaderSettings>(status.settings);
  const [confirmOff, setConfirmOff] = useState(false);
  const set = <K extends keyof CopytradeLeaderSettings>(k: K, v: CopytradeLeaderSettings[K]) => setForm((f) => ({ ...f, [k]: v }));
  const maxShare = Number(config.data?.max_profit_share_pct ?? 20);
  const minAlloc = Number(config.data?.min_allocation ?? 0);
  const maxCopiers = config.data?.max_copiers_per_leader ?? 500;
  const shareOk = decimalRe.test(form.profit_share_pct) && Number(form.profit_share_pct) <= maxShare;
  const allocOk = decimalRe.test(form.min_allocation) && Number(form.min_allocation) >= minAlloc;
  const copiersOk = Number.isInteger(form.max_copiers) && form.max_copiers >= 1 && form.max_copiers <= maxCopiers;
  const dirty = JSON.stringify(form) !== JSON.stringify(status.settings);
  const submit = () =>
    save.mutate(form, {
      onSuccess: (st) => toast.success(st.settings.enabled ? (status.settings.enabled ? "Saved" : "You're taking copiers") : "Saved"),
      onError: (e) => toast.error(errorMessage(e)),
    });
  const turningOff = status.settings.enabled && !form.enabled && status.copiers > 0;

  return (
    <>
      <Section title="Who can lead" info="The rules for offering copy trading. They are checked again every few minutes.">
        <ListGroup>
          {status.requirements.map((r) => (
            <ListRow
              key={r.key}
              leading={r.met ? <CircleCheck className="size-5 text-success-strong" aria-label="Met" /> : <CircleDashed className="size-5 text-fg-muted" aria-label="Not met yet" />}
              title={r.label}
              trailing={<span className="type-caption text-fg-muted">{r.progress}</span>}
            />
          ))}
        </ListGroup>
      </Section>
      <Section title="Your offer">
        <div className="flex flex-col gap-4">
          <Switch
            checked={form.enabled}
            onCheckedChange={(v) => set("enabled", v)}
            disabled={!status.eligible && !status.settings.enabled}
            label="Offer copy trading"
            info="Turning it off stops every copy of you; copiers' positions close or stay as each chose."
          />
          <Field label="Profit share, %" info={`Paid to you from copiers' gains above their previous high, 0 to ${maxShare}%. Applies to copies that start after a change.`} error={!shareOk ? `Enter 0 to ${maxShare}.` : undefined}>
            <Input inputMode="decimal" value={form.profit_share_pct} onChange={(e) => set("profit_share_pct", e.target.value.trim())} />
          </Field>
          <Field label="Smallest allocation" error={!allocOk ? `At least ${money(String(minAlloc))}.` : undefined}>
            <Input inputMode="decimal" value={form.min_allocation} onChange={(e) => set("min_allocation", e.target.value.trim())} />
          </Field>
          <Field label="Most copiers" error={!copiersOk ? `Enter 1 to ${maxCopiers}.` : undefined}>
            <Input inputMode="numeric" value={String(form.max_copiers)} onChange={(e) => set("max_copiers", Number.parseInt(e.target.value, 10) || 0)} />
          </Field>
          <Switch
            checked={form.show_copiers}
            onCheckedChange={(v) => set("show_copiers", v)}
            label="See copiers' names"
            info="Only for people who start copying while this is on; they're told before they start. Others stay private."
          />
          <Button className="self-start" onClick={() => (turningOff ? setConfirmOff(true) : submit())} disabled={!dirty || !shareOk || !allocOk || !copiersOk} loading={save.isPending}>
            Save
          </Button>
        </div>
      </Section>
      <Section title="Copiers">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Stat label="Copiers">{status.copiers}</Stat>
          <Stat label="Copying you">{money(status.aum)}</Stat>
          <Stat label="Profit share earned">{money(status.profit_share_earned)}</Stat>
        </div>
        <Copiers />
      </Section>
      <ConfirmDialog
        open={confirmOff}
        onOpenChange={setConfirmOff}
        title="Stop taking copiers?"
        info={`Your ${status.copiers} ${status.copiers === 1 ? "copy stops" : "copies stop"} now.`}
        confirmLabel="Stop copies"
        tone="danger"
        onConfirm={() => {
          setConfirmOff(false);
          submit();
        }}
      />
    </>
  );
}

function Copiers() {
  const copiers = useCopytradeCopiers();
  const { currency } = useExchangeMoney();
  if (copiers.isPending) {
    return (
      <SkeletonGroup label="Loading copiers" className="flex flex-col gap-2">
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </SkeletonGroup>
    );
  }
  if (copiers.isError) return <ErrorState compact title="Couldn't load your copiers" info={copiers.error.message} onRetry={() => void copiers.refetch()} />;
  if (copiers.data.items.length === 0) {
    return (
      <EmptyState
        compact
        icon={<Users />}
        title="Nobody copies you yet"
        action={
          <Button asChild variant="ghost" size="sm">
            <Link href="/leaders">See the trader list</Link>
          </Button>
        }
      />
    );
  }
  return (
    <>
      <ListGroup>
        {copiers.data.items.map((c, i) => (
          <ListRow
            key={`${c.since}-${i}`}
            title={c.user ? <PersonLine person={c.user} /> : <span className="type-label text-fg">Private copier</span>}
            trailing={
              <span className="flex flex-col items-end">
                <span className="type-label tabular-nums text-fg">{copytradeAllocationLabel(c.allocation, currency)}</span>
                <span className="type-caption text-fg-muted">since {new Date(c.since).toLocaleDateString()}</span>
              </span>
            }
          />
        ))}
      </ListGroup>
      <LoadMore hasNextPage={copiers.hasNextPage} isFetchingNextPage={copiers.isFetchingNextPage} fetchNextPage={copiers.fetchNextPage} />
    </>
  );
}
