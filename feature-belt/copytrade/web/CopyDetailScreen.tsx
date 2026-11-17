"use client";

import {
  copytradeStopReasonLabel,
  errorMessage,
  exchangeSign,
  formatExchangePrice,
  formatExchangeSize,
  useCloseCopytradePosition,
  useCopytradeCopy,
  useCopytradeEvents,
  useCopytradeFunds,
  useExchangeMarkets,
  useStopCopytrade,
  useUpdateCopytrade,
  type CopytradeCopy,
  type CopytradeEvent,
  type CopytradeSettings,
  type CopytradeSettingsPatch,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Input,
  ListGroup,
  ListRow,
  SegmentedControl,
  Skeleton,
  SkeletonGroup,
  Stepper,
  Switch,
  toast,
} from "@scaffoldapp/ui-web";
import { Activity, Layers } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { CopyStatusBadge, PctText, Stat, signTone, useExchangeMoney, when } from "./parts";

const decimalRe = /^\d+(\.\d+)?$/;

/** One copy: value and PnL, funds, settings, the copy account's positions, activity, stop. */
export function CopyDetailScreen({ id }: { id: string }) {
  const copy = useCopytradeCopy(id);
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      {copy.isPending ? (
        <SkeletonGroup label="Loading the copy" className="flex flex-col gap-6">
          <Skeleton className="h-10 w-64" />
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-11" />
            ))}
          </div>
          <Skeleton className="h-40" />
        </SkeletonGroup>
      ) : copy.isError ? (
        <ErrorState
          title={copy.error.status === 404 ? "No such copy" : "Couldn't load the copy"}
          info={copy.error.message}
          onRetry={copy.error.status === 404 ? undefined : () => void copy.refetch()}
          retrying={copy.isFetching}
        />
      ) : (
        <CopyBody copy={copy.data} />
      )}
    </div>
  );
}

function CopyBody({ copy: c }: { copy: CopytradeCopy }) {
  const { money } = useExchangeMoney();
  const update = useUpdateCopytrade(c.id);
  const live = c.status === "active" || c.status === "paused";
  const pause = (status: "active" | "paused") =>
    update.mutate(
      { status },
      { onSuccess: () => toast.success(status === "paused" ? "Copy paused" : "Copy resumed"), onError: (e) => toast.error(errorMessage(e)) },
    );
  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            {c.leader.display_name} <CopyStatusBadge status={c.status} />
          </span>
        }
        info={`Started ${when(c.created_at)}. Positions mirror the trader's at ${Number(c.ratio).toFixed(4)}× their size (this copy's value ÷ theirs).`}
        action={
          live ? (
            <>
              {c.status === "active" ? (
                <Button variant="secondary" size="auto" loading={update.isPending} onClick={() => pause("paused")}>
                  Pause
                </Button>
              ) : (
                <Button variant="secondary" size="auto" loading={update.isPending} onClick={() => pause("active")} disabled={c.pause_reason === "leader_ineligible"}>
                  Resume
                </Button>
              )}
              <StopButton copy={c} />
            </>
          ) : undefined
        }
      />
      {c.status === "paused" && (
        <Banner tone="warning">
          {c.pause_reason === "leader_ineligible" ? "The trader can't lead right now; this copy resumes when they can." : "Paused: new trades aren't copied. Stops still apply."}
        </Banner>
      )}
      {c.status === "stopping" && <Banner tone="warning">Stopping: closing positions. This finishes once the market has liquidity.</Banner>}
      {c.status === "stopped" && (
        <Banner tone="neutral">
          Stopped{c.stop_reason ? `: ${copytradeStopReasonLabel(c.stop_reason).toLowerCase()}` : ""}
          {c.stopped_at ? `, ${when(c.stopped_at)}` : ""}.
        </Banner>
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
        <Stat label="Value">{money(c.equity)}</Stat>
        <Stat label="Profit and loss">
          <span className={signTone(c.pnl)}>{money(c.pnl, true)}</span> <PctText pct={c.pnl_pct} className="type-caption" />
        </Stat>
        <Stat label="Moved in">{money(c.allocation)}</Stat>
        <Stat label="Peak value">{money(c.peak_equity)}</Stat>
        <Stat label="Free">{c.account ? money(c.account.free_collateral) : "–"}</Stat>
        <Stat label="Profit share">
          {c.profit_share_pct}% · {money(c.profit_share_paid)} paid
        </Stat>
      </div>
      {live && <FundsButtons copy={c} />}
      <Section title="Positions" info="This copy's own account. Closing one by hand keeps that market out of mirroring until the trader goes flat or flips.">
        <Positions copy={c} />
      </Section>
      <Section title="Settings">
        <SettingsList copy={c} editable={live} />
      </Section>
      <Section
        title="Skipped, last 30 days"
        info="Orders below the market minimum (too small), beyond your limits (risk limit), refused by the exchange (rejected) or more than 2% past the trader's price (slippage)."
      >
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label="Too small">{c.skipped_30d.too_small}</Stat>
          <Stat label="Risk limit">{c.skipped_30d.risk_limit}</Stat>
          <Stat label="Rejected">{c.skipped_30d.rejected}</Stat>
          <Stat label="Slippage">{c.skipped_30d.slippage}</Stat>
        </div>
      </Section>
      <Section title="Activity">
        <Events id={c.id} />
      </Section>
    </>
  );
}

function StopButton({ copy: c }: { copy: CopytradeCopy }) {
  const stop = useStopCopytrade(c.id);
  const [open, setOpen] = useState(false);
  const hasPositions = (c.account?.positions.length ?? 0) > 0;
  const run = (closePositions: boolean) =>
    stop.mutate(
      { closePositions },
      {
        onSuccess: (x) => {
          setOpen(false);
          toast.success(x.status === "stopped" ? "Copy stopped; funds returned" : "Stopping: closing positions");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't stop the copy.")),
      },
    );
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      trigger={
        <Button variant="danger" size="auto">
          Stop
        </Button>
      }
      title={`Stop copying ${c.leader.display_name}?`}
      info="Open orders are cancelled, any profit share is settled, and free funds return to your main account."
      footer={
        hasPositions ? (
          <>
            <Button variant="secondary" loading={stop.isPending && stop.variables?.closePositions === false} onClick={() => run(false)}>
              Keep positions
            </Button>
            <Button variant="danger" loading={stop.isPending && stop.variables?.closePositions === true} onClick={() => run(true)}>
              Close positions and stop
            </Button>
          </>
        ) : (
          <Button variant="danger" loading={stop.isPending} onClick={() => run(true)}>
            Stop copying
          </Button>
        )
      }
    >
      {hasPositions && <p className="type-body text-fg-muted">Kept positions stay in the copy account until you close them here.</p>}
    </Dialog>
  );
}

function FundsButtons({ copy: c }: { copy: CopytradeCopy }) {
  const [mode, setMode] = useState<"add" | "withdraw" | null>(null);
  return (
    <div className="flex flex-wrap gap-2">
      <Button variant="secondary" size="sm" onClick={() => setMode("add")}>
        Add funds
      </Button>
      <Button variant="secondary" size="sm" onClick={() => setMode("withdraw")}>
        Withdraw
      </Button>
      {mode && <FundsDialog key={mode} copy={c} mode={mode} onClose={() => setMode(null)} />}
    </div>
  );
}

function FundsDialog({ copy: c, mode, onClose }: { copy: CopytradeCopy; mode: "add" | "withdraw"; onClose: () => void }) {
  const funds = useCopytradeFunds(c.id);
  const { money } = useExchangeMoney();
  const [amount, setAmount] = useState("");
  const ok = decimalRe.test(amount) && Number(amount) > 0;
  const submit = () =>
    funds.mutate(mode === "add" ? amount : `-${amount}`, {
      onSuccess: () => {
        toast.success(mode === "add" ? `Added ${money(amount)}` : `Withdrew ${money(amount)}`);
        onClose();
      },
      onError: (e) => toast.error(errorMessage(e)),
    });
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={mode === "add" ? "Add funds" : "Withdraw"}
      info={mode === "add" ? "Moves funds from your main account; positions grow with this copy's value." : "Settles any profit share first. Only free funds can be withdrawn."}
      footer={
        <Button onClick={submit} disabled={!ok} loading={funds.isPending}>
          {mode === "add" ? "Add" : "Withdraw"}
        </Button>
      }
    >
      <Field label="Amount" labelEnd={mode === "withdraw" && c.account ? <span className="type-caption text-fg-muted">{money(c.account.free_collateral)} free</span> : undefined}>
        <Input inputMode="decimal" autoFocus value={amount} onChange={(e) => setAmount(e.target.value.trim())} />
      </Field>
    </Dialog>
  );
}

function Positions({ copy: c }: { copy: CopytradeCopy }) {
  const { money } = useExchangeMoney();
  const markets = useExchangeMarkets();
  const close = useCloseCopytradePosition(c.id);
  const positions = c.account?.positions ?? [];
  if (positions.length === 0) {
    return <EmptyState compact icon={<Layers />} title={c.account ? "No open positions" : "The copy account is closed"} />;
  }
  return (
    <ListGroup>
      {positions.map((p) => {
        const m = markets.data?.items.find((x) => x.symbol === p.symbol);
        const long = exchangeSign(p.sz) > 0;
        return (
          <ListRow
            key={p.symbol}
            title={
              <span className="flex flex-col gap-1">
                <span className="flex items-center gap-2">
                  <Link href={`/markets/${p.symbol}`} className="type-label font-semibold text-fg hover:underline">
                    {p.symbol}
                  </Link>
                  <Badge tone={long ? "success" : "danger"} size="sm">
                    {long ? "Long" : "Short"} {formatExchangeSize(p.sz.replace(/^-/, ""), m?.sz_decimals)}
                  </Badge>
                </span>
                <span className="type-caption text-fg-muted">
                  Entry {formatExchangePrice(p.entry_px, m?.tick_size)} · mark {formatExchangePrice(p.mark_px, m?.tick_size)} ·{" "}
                  <span className={signTone(p.unrealized_pnl)}>{money(p.unrealized_pnl, true)}</span>
                </span>
              </span>
            }
            trailing={
              c.status !== "stopping" && (
                <Button
                  variant="secondary"
                  size="sm"
                  loading={close.isPending && close.variables?.symbol === p.symbol}
                  onClick={() =>
                    close.mutate({ symbol: p.symbol }, { onSuccess: () => toast.success(`${p.symbol} closed`), onError: (e) => toast.error(errorMessage(e)) })
                  }
                >
                  Close
                </Button>
              )
            }
          />
        );
      })}
    </ListGroup>
  );
}

function SettingsList({ copy: c, editable }: { copy: CopytradeCopy; editable: boolean }) {
  const { money } = useExchangeMoney();
  const [editing, setEditing] = useState(false);
  const s = c.settings;
  const rows: [string, string][] = [
    ["Maximum leverage", `${s.max_leverage}×`],
    ["Largest copied order", money(s.max_trade_notional)],
    ["Largest position per market", money(s.max_position_notional)],
    ["Stop loss", s.stop_loss_pct ? `${s.stop_loss_pct}% below what went in` : "Off"],
    ["Drawdown limit", s.max_drawdown_pct ? `${s.max_drawdown_pct}% below the peak` : "Off"],
    ["When the trader stops", s.on_leader_exit === "close" ? "Close positions" : "Keep positions"],
    ["Slippage limit", `${(s.slippage_bps / 100).toFixed(1)}%`],
  ];
  return (
    <>
      <ListGroup>
        {rows.map(([k, v]) => (
          <ListRow key={k} title={k} trailing={<span className="type-label tabular-nums text-fg-muted">{v}</span>} />
        ))}
      </ListGroup>
      {editable && (
        <Button variant="secondary" size="sm" className="self-start" onClick={() => setEditing(true)}>
          Edit settings
        </Button>
      )}
      {editing && <SettingsDialog copy={c} onClose={() => setEditing(false)} />}
    </>
  );
}

function SettingsDialog({ copy: c, onClose }: { copy: CopytradeCopy; onClose: () => void }) {
  const update = useUpdateCopytrade(c.id);
  const [s, setS] = useState<CopytradeSettings>(c.settings);
  const set = <K extends keyof CopytradeSettings>(k: K, v: CopytradeSettings[K]) => setS((x) => ({ ...x, [k]: v }));
  const pctOk = (v: string | null) => v === null || (decimalRe.test(v) && Number(v) >= 5 && Number(v) <= 90);
  const amountOk = (v: string, hi: number) => decimalRe.test(v) && Number(v) >= 10 && Number(v) <= hi;
  const valid = pctOk(s.stop_loss_pct) && pctOk(s.max_drawdown_pct) && amountOk(s.max_trade_notional, 50_000) && amountOk(s.max_position_notional, 1_000_000);
  const save = () => {
    const patch: CopytradeSettingsPatch = {};
    for (const k of Object.keys(s) as (keyof CopytradeSettings)[]) {
      if (s[k] !== c.settings[k]) (patch as Record<string, unknown>)[k] = s[k];
    }
    update.mutate(
      { settings: patch },
      {
        onSuccess: () => {
          toast.success("Settings saved");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e)),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="Copy settings"
      size="md"
      footer={
        <Button onClick={save} disabled={!valid} loading={update.isPending}>
          Save
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Maximum leverage">
          <Stepper value={s.max_leverage} onValueChange={(v) => set("max_leverage", v)} min={1} max={10} label="Maximum leverage" format={(n) => `${n}×`} />
        </Field>
        <Field label="Largest copied order" info="10 to 50,000." error={!amountOk(s.max_trade_notional, 50_000) ? "Enter 10 to 50,000." : undefined}>
          <Input inputMode="decimal" value={s.max_trade_notional} onChange={(e) => set("max_trade_notional", e.target.value.trim())} />
        </Field>
        <Field label="Largest position per market" info="10 to 1,000,000." error={!amountOk(s.max_position_notional, 1_000_000) ? "Enter 10 to 1,000,000." : undefined}>
          <Input inputMode="decimal" value={s.max_position_notional} onChange={(e) => set("max_position_notional", e.target.value.trim())} />
        </Field>
        <PctLimit label="Stop loss, %" info="Stop and close when the value falls this far below what went in." value={s.stop_loss_pct} onChange={(v) => set("stop_loss_pct", v)} valid={pctOk(s.stop_loss_pct)} />
        <PctLimit label="Drawdown limit, %" info="Stop and close when the value falls this far below its peak." value={s.max_drawdown_pct} onChange={(v) => set("max_drawdown_pct", v)} valid={pctOk(s.max_drawdown_pct)} />
        <Field label="When the trader stops">
          <SegmentedControl
            options={[
              { value: "close", label: "Close positions" },
              { value: "keep", label: "Keep positions" },
            ]}
            value={s.on_leader_exit}
            onValueChange={(v) => set("on_leader_exit", v)}
            fullWidth
          />
        </Field>
        <Field label="Slippage limit" info="How far from the mark a copied order may fill.">
          <Stepper value={s.slippage_bps} onValueChange={(v) => set("slippage_bps", v)} min={10} max={500} step={10} label="Slippage limit" format={(n) => `${(n / 100).toFixed(1)}%`} />
        </Field>
      </div>
    </Dialog>
  );
}

function PctLimit({ label, info, value, onChange, valid }: { label: string; info: string; value: string | null; onChange: (v: string | null) => void; valid: boolean }) {
  return (
    <div className="flex flex-col gap-2">
      <Switch checked={value !== null} onCheckedChange={(on) => onChange(on ? "30" : null)} label={label.replace(", %", "")} info={info} />
      {value !== null && (
        <Field label={label} error={!valid ? "Enter 5 to 90." : undefined}>
          <Input inputMode="decimal" value={value} onChange={(e) => onChange(e.target.value.trim())} />
        </Field>
      )}
    </div>
  );
}

const kindLabels: Record<CopytradeEvent["kind"], string> = {
  start: "Started",
  order: "Order",
  skip: "Skipped",
  stop: "Stop",
  transfer: "Transfer",
  profit_share: "Profit share",
  reconcile: "Reconciled",
  pause: "Paused",
  resume: "Resumed",
  settings: "Settings",
};

function Events({ id }: { id: string }) {
  const events = useCopytradeEvents(id);
  const { money } = useExchangeMoney();
  if (events.isPending) {
    return (
      <SkeletonGroup label="Loading activity" className="flex flex-col gap-2">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </SkeletonGroup>
    );
  }
  if (events.isError) return <ErrorState compact title="Couldn't load activity" info={events.error.message} onRetry={() => void events.refetch()} />;
  if (events.data.items.length === 0) return <EmptyState compact icon={<Activity />} title="No activity yet" />;
  return (
    <>
      <ListGroup>
        {events.data.items.map((e) => (
          <ListRow
            key={e.id}
            title={
              <span className="flex flex-col gap-1">
                <span className="flex flex-wrap items-center gap-2">
                  <Badge tone={e.kind === "skip" ? "warning" : e.kind === "stop" ? "danger" : "neutral"} size="sm">
                    {kindLabels[e.kind]}
                  </Badge>
                  {e.symbol && <span className="type-label font-semibold text-fg">{e.symbol}</span>}
                  {e.side && e.sz && (
                    <span className="type-caption text-fg-muted">
                      {e.side === "buy" ? "Bought" : "Sold"} {e.sz}
                      {e.notional ? ` · ${money(e.notional)}` : ""}
                    </span>
                  )}
                  {!e.side && e.notional && <span className="type-caption tabular-nums text-fg-muted">{money(e.notional, true)}</span>}
                </span>
                <span className="type-caption text-fg-muted">{e.reason}</span>
              </span>
            }
            trailing={<span className="shrink-0 type-caption text-fg-muted">{when(e.created_at)}</span>}
          />
        ))}
      </ListGroup>
      <LoadMore hasNextPage={events.hasNextPage} isFetchingNextPage={events.isFetchingNextPage} fetchNextPage={events.fetchNextPage} />
    </>
  );
}
