"use client";

import {
  errorMessage,
  useCopytradeConfig,
  useExchangeAccount,
  useStartCopytrade,
  type CopytradeLeaderCard,
  type CopytradeSettingsPatch,
} from "@scaffoldapp/shared";
import { Banner, Button, Checkbox, ErrorState, Field, Input, SegmentedControl, Sheet, Skeleton, SkeletonGroup, Stepper, Switch, toast } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Disclosure, PctText, PersonLine, Stat, useExchangeMoney } from "./parts";

const decimalRe = /^\d+(\.\d+)?$/;

/** Start copying one leader: allocation, the main risk limits and the risk disclosure. */
export function StartCopySheet({ leader, open, onOpenChange }: { leader: CopytradeLeaderCard | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={leader ? `Copy ${leader.user.display_name}` : "Copy a trader"} size="md">
      {leader && <StartCopyForm key={leader.user.id} leader={leader} onDone={() => onOpenChange(false)} />}
    </Sheet>
  );
}

function StartCopyForm({ leader, onDone }: { leader: CopytradeLeaderCard; onDone: () => void }) {
  const config = useCopytradeConfig();
  const account = useExchangeAccount();
  const start = useStartCopytrade();
  const router = useRouter();
  const { money } = useExchangeMoney();
  const minimum = String(Math.max(Number(config.data?.min_allocation ?? 0), Number(leader.min_allocation)));
  const [allocation, setAllocation] = useState("");
  const [leverage, setLeverage] = useState(3);
  const [stopLoss, setStopLoss] = useState<string | null>("30");
  const [openPositions, setOpenPositions] = useState(false);
  const [onExit, setOnExit] = useState<"close" | "keep">("close");
  const [accepted, setAccepted] = useState(false);

  if (config.isPending || account.isPending) {
    return (
      <SkeletonGroup label="Loading" className="flex flex-col gap-4">
        <Skeleton className="h-16" />
        <Skeleton className="h-12" />
        <Skeleton className="h-40" />
      </SkeletonGroup>
    );
  }
  if (config.isError || account.isError) {
    const err = config.error ?? account.error;
    return <ErrorState compact title="Couldn't load copy trading" info={err?.message} onRetry={() => void (config.isError ? config.refetch() : account.refetch())} />;
  }
  const cfg = config.data;
  const free = account.data.free_collateral;
  const amountOk = decimalRe.test(allocation) && Number(allocation) >= Number(minimum) && Number(allocation) <= Number(free);
  const stopOk = stopLoss === null || (decimalRe.test(stopLoss) && Number(stopLoss) >= 5 && Number(stopLoss) <= 90);
  const blocked = cfg.paused ? "Copy trading is paused right now." : leader.is_me ? "This is you." : leader.copying ? "You already copy this trader." : !leader.accepting ? "This trader isn't taking copiers right now." : null;
  const submit = () => {
    const settings: CopytradeSettingsPatch = { max_leverage: leverage, stop_loss_pct: stopLoss, copy_open_positions: openPositions, on_leader_exit: onExit };
    start.mutate(
      { leader_id: leader.user.id, allocation, settings, disclosure_version: cfg.disclosure_version },
      {
        onSuccess: (c) => {
          toast.success(`You're copying ${leader.user.display_name}`);
          onDone();
          router.push(`/copy/${c.id}`);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't start the copy.")),
      },
    );
  };

  return (
    <div className="flex flex-col gap-6">
      <PersonLine person={leader.user} size="md">
        <span className="type-caption text-fg-muted">
          90 days <PctText pct={leader.return_90d_pct} /> · max drawdown <PctText pct={leader.max_drawdown_90d_pct} signed={false} />
        </span>
      </PersonLine>
      <div className="grid grid-cols-2 gap-4">
        <Stat label="Profit share">{leader.profit_share_pct}%</Stat>
        <Stat label="Copiers">{leader.copiers}</Stat>
      </div>
      {blocked && <Banner tone="warning">{blocked}</Banner>}
      <Field label="Allocation" info={`Moves from your main account into a separate copy account. At least ${money(minimum)}.`} error={allocation && !amountOk ? `Enter ${money(minimum)} to ${money(free)}.` : undefined}>
        <Input inputMode="decimal" value={allocation} onChange={(e) => setAllocation(e.target.value.trim())} placeholder={minimum} trailing={<span className="type-caption text-fg-muted">of {money(free)} free</span>} />
      </Field>
      <Field label="Maximum leverage" info="Copied positions use the trader's leverage, capped here.">
        <Stepper value={leverage} onValueChange={setLeverage} min={1} max={10} label="Maximum leverage" format={(n) => `${n}×`} />
      </Field>
      <div className="flex flex-col gap-2">
        <Switch checked={stopLoss !== null} onCheckedChange={(on) => setStopLoss(on ? "30" : null)} label="Stop loss" info="Stops the copy and closes its positions when its value falls this far below what you put in. Checked about every 15 seconds." />
        {stopLoss !== null && (
          <Field label="Stop loss, %" error={!stopOk ? "Enter 5 to 90." : undefined}>
            <Input inputMode="decimal" value={stopLoss} onChange={(e) => setStopLoss(e.target.value.trim())} />
          </Field>
        )}
      </div>
      <Switch checked={openPositions} onCheckedChange={setOpenPositions} label="Copy open positions now" info="Off: only the trader's new positions are copied; ones they already hold join once they close or flip." />
      <Field label="When the trader stops" info="If the trader stops offering copies or leaves, close your copied positions or keep them.">
        <SegmentedControl
          options={[
            { value: "close", label: "Close positions" },
            { value: "keep", label: "Keep positions" },
          ]}
          value={onExit}
          onValueChange={setOnExit}
          fullWidth
        />
      </Field>
      {leader.show_copiers && <Banner tone="info">This trader sees the names of people who copy them.</Banner>}
      <div className="flex flex-col gap-3 rounded-lg bg-sunken p-4">
        <Disclosure markdown={cfg.disclosure_md} />
        <Checkbox checked={accepted} onCheckedChange={setAccepted} label="I've read and accept the risk disclosure" />
      </div>
      <Button onClick={submit} disabled={!!blocked || !amountOk || !stopOk || !accepted} loading={start.isPending}>
        Start copying
      </Button>
    </div>
  );
}
