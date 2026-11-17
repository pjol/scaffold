import {
  copytradeStopReasonLabel,
  errorMessage,
  exchangeSign,
  formatCopytradePct,
  formatExchangePrice,
  formatExchangeSize,
  useCloseCopytradePosition,
  useCopytradeCopy,
  useCopytradeEvents,
  useCopytradeFunds,
  useExchangeAccount,
  useExchangeMarkets,
  useStopCopytrade,
  useUpdateCopytrade,
  type CopytradeCopy,
  type CopytradeEvent,
  type CopytradeEventKind,
  type CopytradeSettingsPatch,
  type ExchangeMarket,
  type ExchangePosition,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Banner,
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Input,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  Sheet,
  Skeleton,
  SkeletonRow,
  Spinner,
  Text,
  Title,
  haptic,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { ArrowLeftRight, History, Layers, Pause, Play } from "lucide-react-native";
import { useCallback, useEffect, useState } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { signTone, Stat, StatGrid, useExchangeMoney } from "@/features/exchange/parts";
import { CopyStatusBadge, decimalInput, Facts, Group, isDecimal, Item, PctLimitField, pctError, plainNumber, StepperRow, when } from "./parts";

type Lookup = (symbol: string) => ExchangeMarket | undefined;
type Exit = "close" | "keep";

const kindLabels: Record<CopytradeEventKind, string> = {
  start: "Started",
  order: "Order",
  skip: "Skipped",
  stop: "Stopped",
  transfer: "Funds",
  profit_share: "Profit share",
  reconcile: "Sync",
  pause: "Paused",
  resume: "Resumed",
  settings: "Settings",
};

function slippageText(bps: number): string {
  return `${plainNumber((bps / 100).toFixed(2))}%`;
}

// ---- positions ----

function CopyPositionItem({ copy, p, market, closeable }: { copy: CopytradeCopy; p: ExchangePosition; market: ExchangeMarket | undefined; closeable: boolean }) {
  const { money } = useExchangeMoney();
  const close = useCloseCopytradePosition(copy.id);
  const [confirm, setConfirm] = useState(false);
  const long = exchangeSign(p.sz) > 0;
  const abs = p.sz.replace(/^-/, "");
  const live = copy.status === "active" || copy.status === "paused";
  return (
    <>
      <Item
        title={
          <>
            <Text role="label" weight={600}>
              {p.symbol}
            </Text>
            <Badge size="sm" tone={long ? "success" : "danger"}>
              {long ? "Long" : "Short"}
            </Badge>
            <Text role="caption" tone="muted">
              {p.leverage}×
            </Text>
          </>
        }
        action={
          closeable ? (
            <Button variant="outline" size="md" loading={close.isPending} onPress={() => setConfirm(true)}>
              Close
            </Button>
          ) : undefined
        }
        cells={[
          ["Size", formatExchangeSize(abs, market?.sz_decimals)],
          [
            "PnL",
            <Text key="pnl" role="label" tabular tone={signTone(p.unrealized_pnl)}>
              {money(p.unrealized_pnl, true)} ({p.roe_pct}%)
            </Text>,
          ],
          ["Entry", formatExchangePrice(p.entry_px, market?.tick_size)],
          ["Mark", formatExchangePrice(p.mark_px, market?.tick_size)],
          ["Liq. price", p.liquidation_px ? formatExchangePrice(p.liquidation_px, market?.tick_size) : "–"],
        ]}
      />
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Close ${p.symbol} at market?`}
        tone="danger"
        confirmLabel="Close position"
        onConfirm={() => close.mutateAsync({ symbol: p.symbol }).then(() => toast.success(`${p.symbol} position closed`))}
        errorText={(e) => errorMessage(e, "Couldn't close the position. Try again.")}
      >
        <Text role="body">
          {long ? "Sells" : "Buys"} {formatExchangeSize(abs, market?.sz_decimals)} {p.symbol} against the book now.
          {live ? " The trader's trades in it aren't copied again until they close or flip their position." : ""}
        </Text>
      </ConfirmDialog>
    </>
  );
}

function Positions({ copy, lookup }: { copy: CopytradeCopy; lookup: Lookup }) {
  const positions = copy.account?.positions ?? [];
  if (positions.length === 0) return <EmptyState compact icon={Layers} title="No open positions" />;
  return (
    <View>
      {positions.map((p) => (
        <CopyPositionItem key={p.symbol} copy={copy} p={p} market={lookup(p.symbol)} closeable={copy.status !== "stopping"} />
      ))}
    </View>
  );
}

// ---- activity ----

function EventRow({ e, market }: { e: CopytradeEvent; market: ExchangeMarket | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const sz = e.sz ? formatExchangeSize(e.sz.replace(/^-/, ""), market?.sz_decimals) : "";
  const what = [sz, e.symbol].filter(Boolean).join(" ");
  return (
    <View style={{ gap: s[1], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: theme.colors.border }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
        <Text role="label" weight={600} tone={e.kind === "skip" ? "warning" : "default"}>
          {kindLabels[e.kind]}
        </Text>
        {e.side ? (
          <Badge size="sm" tone={e.side === "buy" ? "success" : "danger"}>
            {e.side === "buy" ? "Buy" : "Sell"}
          </Badge>
        ) : null}
        {what ? (
          <Text role="label" tabular>
            {what}
          </Text>
        ) : null}
        <Text role="caption" tone="muted" style={{ marginLeft: "auto" }}>
          {when(e.created_at)}
        </Text>
      </View>
      {e.reason ? (
        <Text role="label" tone="muted">
          {e.reason}
        </Text>
      ) : null}
    </View>
  );
}

function Activity({ copy, lookup }: { copy: CopytradeCopy; lookup: Lookup }) {
  const theme = useTheme();
  const s = theme.space;
  const events = useCopytradeEvents(copy.id);
  const k = copy.skipped_30d;
  const skipped = k.too_small + k.risk_limit + k.rejected + k.slippage;
  const info =
    skipped > 0
      ? `${skipped === 1 ? "1 order" : `${skipped} orders`} skipped in the last 30 days: ${k.too_small} too small, ${k.risk_limit} over your limits, ${k.slippage} past the slippage limit, ${k.rejected} rejected.`
      : undefined;
  return (
    <Group title="Activity" info={info}>
      {events.isPending ? (
        <View style={{ gap: s[2] }} accessibilityLabel="Loading activity" accessibilityRole="progressbar">
          {[0, 1, 2].map((i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : events.isError ? (
        <ErrorState compact title="Couldn't load activity" info={events.error.message} onRetry={() => void events.refetch()} retrying={events.isFetching} />
      ) : events.data.items.length === 0 ? (
        <EmptyState compact icon={History} title="No activity yet" />
      ) : (
        <View>
          {events.data.items.map((e) => (
            <EventRow key={e.id} e={e} market={e.symbol ? lookup(e.symbol) : undefined} />
          ))}
          {events.hasNextPage &&
            (events.isFetchingNextPage ? (
              <Spinner />
            ) : (
              <Button variant="ghost" onPress={() => void events.fetchNextPage()}>
                Load more
              </Button>
            ))}
        </View>
      )}
    </Group>
  );
}

// ---- funds ----

type FundsMode = "add" | "withdraw";

function FundsSheet({ copy, open, onClose }: { copy: CopytradeCopy; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const { money, currency } = useExchangeMoney();
  const main = useExchangeAccount();
  const funds = useCopytradeFunds(copy.id);
  const [mode, setMode] = useState<FundsMode>("add");
  const [amount, setAmount] = useState("");
  useEffect(() => {
    if (open) {
      setMode("add");
      setAmount("");
    }
  }, [open]);
  const available = mode === "add" ? main.data?.free_collateral : copy.account?.free_collateral;
  const valid = isDecimal(amount) && Number(amount) > 0;
  const over = valid && available !== undefined && Number(amount) > Number(available);
  const submit = () =>
    funds.mutate(mode === "add" ? amount : `-${amount}`, {
      onSuccess: () => {
        haptic.success();
        toast.success(mode === "add" ? "Funds added" : "Funds withdrawn");
        onClose();
      },
      onError: (e) => toast.error(errorMessage(e, mode === "add" ? "Couldn't add funds. Try again." : "Couldn't withdraw. Try again.")),
    });
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="Add or withdraw"
      snapPoints={["65%"]}
      footer={
        <Button fullWidth loading={funds.isPending} disabled={!valid || over} onPress={submit}>
          {mode === "add" ? (valid ? `Add ${money(amount)}` : "Add funds") : valid ? `Withdraw ${money(amount)}` : "Withdraw"}
        </Button>
      }
    >
      <View style={{ gap: s[4] }}>
        <SegmentedControl<FundsMode>
          accessibilityLabel="Add or withdraw"
          options={[
            { value: "add", label: "Add" },
            { value: "withdraw", label: "Withdraw" },
          ]}
          value={mode}
          onValueChange={setMode}
        />
        <Field
          label="Amount"
          info={
            mode === "add"
              ? "Moved from your main account's free collateral."
              : "Only free funds: margin held by open positions stays. Any profit share due is settled first."
          }
          error={over && available !== undefined ? `You have ${money(available)} available.` : undefined}
          labelEnd={
            available !== undefined && Number(available) > 0 ? (
              <Pressable accessibilityRole="button" accessibilityLabel="Use the full amount available" hitSlop={12} onPress={() => setAmount(available)}>
                <Text role="caption" tone="primary" weight={600}>
                  Max
                </Text>
              </Pressable>
            ) : null
          }
        >
          <Input
            keyboardType="decimal-pad"
            value={amount}
            onChangeText={(t) => setAmount(decimalInput(t))}
            placeholder="0"
            trailing={
              <Text role="caption" tone="muted">
                {currency}
              </Text>
            }
          />
        </Field>
        <Facts rows={[[mode === "add" ? "Free in your main account" : "Free in this copy", available !== undefined ? money(available) : "–"]]} />
      </View>
    </Sheet>
  );
}

// ---- settings ----

function changedPct(next: string | null, prev: string | null): boolean {
  if (next === null || prev === null) return next !== prev;
  return Number(next) !== Number(prev);
}

function amountError(v: string, lo: number, hi: number, money: (a: string) => string): string | undefined {
  if (!isDecimal(v) || Number(v) < lo || Number(v) > hi) return `Use ${money(String(lo))}–${money(String(hi))}.`;
  return undefined;
}

function SettingsSheet({ copy, open, onClose }: { copy: CopytradeCopy; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const { money, currency } = useExchangeMoney();
  const update = useUpdateCopytrade(copy.id);
  const st = copy.settings;
  const [lev, setLev] = useState(st.max_leverage);
  const [stopOn, setStopOn] = useState(st.stop_loss_pct !== null);
  const [stop, setStop] = useState(plainNumber(st.stop_loss_pct ?? "30"));
  const [ddOn, setDdOn] = useState(st.max_drawdown_pct !== null);
  const [dd, setDd] = useState(plainNumber(st.max_drawdown_pct ?? "20"));
  const [perOrder, setPerOrder] = useState(plainNumber(st.max_trade_notional));
  const [perMarket, setPerMarket] = useState(plainNumber(st.max_position_notional));
  const [slip, setSlip] = useState(st.slippage_bps);
  const [onExit, setOnExit] = useState<Exit>(st.on_leader_exit);
  useEffect(() => {
    if (!open) return;
    setLev(st.max_leverage);
    setStopOn(st.stop_loss_pct !== null);
    setStop(plainNumber(st.stop_loss_pct ?? "30"));
    setDdOn(st.max_drawdown_pct !== null);
    setDd(plainNumber(st.max_drawdown_pct ?? "20"));
    setPerOrder(plainNumber(st.max_trade_notional));
    setPerMarket(plainNumber(st.max_position_notional));
    setSlip(st.slippage_bps);
    setOnExit(st.on_leader_exit);
    // Load the saved settings each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const stopError = stopOn ? pctError(stop, 5, 90) : undefined;
  const ddError = ddOn ? pctError(dd, 5, 90) : undefined;
  const orderError = amountError(perOrder, 10, 50_000, money);
  const marketError = amountError(perMarket, 10, 1_000_000, money);
  const valid = !stopError && !ddError && !orderError && !marketError;

  const stopNext = stopOn ? stop : null;
  const ddNext = ddOn ? dd : null;
  const patch: CopytradeSettingsPatch = {
    ...(lev !== st.max_leverage ? { max_leverage: lev } : {}),
    ...(changedPct(stopNext, st.stop_loss_pct) ? { stop_loss_pct: stopNext } : {}),
    ...(changedPct(ddNext, st.max_drawdown_pct) ? { max_drawdown_pct: ddNext } : {}),
    ...(Number(perOrder) !== Number(st.max_trade_notional) ? { max_trade_notional: perOrder } : {}),
    ...(Number(perMarket) !== Number(st.max_position_notional) ? { max_position_notional: perMarket } : {}),
    ...(slip !== st.slippage_bps ? { slippage_bps: slip } : {}),
    ...(onExit !== st.on_leader_exit ? { on_leader_exit: onExit } : {}),
  };
  const dirty = Object.keys(patch).length > 0;

  const submit = () =>
    update.mutate(
      { settings: patch },
      {
        onSuccess: () => {
          haptic.success();
          toast.success("Settings saved");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save the settings. Try again.")),
      },
    );

  const unit = (
    <Text role="caption" tone="muted">
      {currency}
    </Text>
  );

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="Copy settings"
      snapPoints={["92%"]}
      footer={
        <Button fullWidth loading={update.isPending} disabled={!dirty || !valid} onPress={submit}>
          Save
        </Button>
      }
    >
      <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: s[6], paddingBottom: s[6] }}>
        <StepperRow
          label="Max leverage"
          info="Copied positions use the trader's leverage, up to this."
          value={lev}
          min={1}
          max={10}
          onValueChange={setLev}
          format={(n) => `${n}×`}
        />
        <PctLimitField
          label="Stop loss"
          info="Stops the copy and closes its positions once it has lost this share of what you put in."
          on={stopOn}
          onToggle={setStopOn}
          value={stop}
          onChange={setStop}
          error={stopError}
        />
        <PctLimitField
          label="Drawdown limit"
          info="Stops the copy and closes its positions once its value falls this far below its peak."
          on={ddOn}
          onToggle={setDdOn}
          value={dd}
          onChange={setDd}
          error={ddError}
        />
        <Field label="Max per order" info="The largest single order copied for you." error={orderError}>
          <Input keyboardType="decimal-pad" value={perOrder} onChangeText={(t) => setPerOrder(decimalInput(t))} placeholder="0" trailing={unit} />
        </Field>
        <Field label="Max per market" info="The largest position the copy holds in one market." error={marketError}>
          <Input keyboardType="decimal-pad" value={perMarket} onChangeText={(t) => setPerMarket(decimalInput(t))} placeholder="0" trailing={unit} />
        </Field>
        <StepperRow
          label="Slippage"
          info="How far past the trader's price a copied order may fill. Orders that would fill further away are skipped."
          value={slip}
          min={10}
          max={500}
          step={10}
          onValueChange={setSlip}
          format={slippageText}
        />
        <Field label="If the trader stops" info="If the trader stops offering copies or leaves, your copy stops too. This decides what happens to its positions.">
          <SegmentedControl<Exit>
            accessibilityLabel="If the trader stops"
            options={[
              { value: "close", label: "Close positions" },
              { value: "keep", label: "Keep positions" },
            ]}
            value={onExit}
            onValueChange={setOnExit}
          />
        </Field>
      </ScrollView>
    </Sheet>
  );
}

// ---- stop ----

function StopDialog({ copy, open, onClose }: { copy: CopytradeCopy; open: boolean; onClose: () => void }) {
  const stop = useStopCopytrade(copy.id);
  const [which, setWhich] = useState<"close" | "keep" | null>(null);
  const hasPositions = (copy.account?.positions.length ?? 0) > 0;
  const name = copy.leader.display_name;
  const run = (closePositions: boolean) => {
    setWhich(closePositions ? "close" : "keep");
    stop.mutate(
      { closePositions },
      {
        onSuccess: () => {
          haptic.success();
          toast.success(`Stopped copying ${name}`);
          onClose();
        },
        onError: (e) => {
          haptic.error();
          toast.error(errorMessage(e, "Couldn't stop the copy. Try again."));
        },
        onSettled: () => setWhich(null),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onClose={() => {
        if (!stop.isPending) onClose();
      }}
      dismissible={!stop.isPending}
      title={`Stop copying ${name}?`}
      footer={
        <>
          <Button variant="danger" fullWidth loading={which === "close"} disabled={stop.isPending && which !== "close"} onPress={() => run(true)}>
            {hasPositions ? "Stop and close positions" : "Stop copying"}
          </Button>
          {hasPositions ? (
            <Button variant="secondary" fullWidth loading={which === "keep"} disabled={stop.isPending && which !== "keep"} onPress={() => run(false)}>
              Stop and keep positions
            </Button>
          ) : null}
          <Button variant="ghost" fullWidth disabled={stop.isPending} onPress={onClose}>
            Cancel
          </Button>
        </>
      }
    >
      <Text role="body">
        New trades aren't copied and free funds go back to your main account.
        {hasPositions ? " Kept positions stay in the copy until you close them." : ""}
      </Text>
    </Dialog>
  );
}

// ---- screen ----

function Notice({ copy }: { copy: CopytradeCopy }) {
  if (copy.status === "stopped") {
    const reason = copytradeStopReasonLabel(copy.stop_reason) || "Stopped";
    const title = copy.stopped_at ? `${reason} · ${when(copy.stopped_at)}` : reason;
    return <Banner tone={copy.stop_reason === "stop_loss" || copy.stop_reason === "drawdown" ? "warning" : "info"} title={title} />;
  }
  if (copy.status === "stopping") return <Banner tone="info" title="Stopping: closing positions and returning funds." />;
  if (copy.status === "paused" && copy.pause_reason === "leader_ineligible") {
    return <Banner tone="warning" title="Paused: the trader can't lead right now. The copy resumes when they can; stops still apply." />;
  }
  return null;
}

function CopyView({ copy, refreshing, onRefresh }: { copy: CopytradeCopy; refreshing: boolean; onRefresh: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const { money } = useExchangeMoney();
  const markets = useExchangeMarkets();
  const lookup = useCallback((x: string) => markets.data?.items.find((m) => m.symbol === x), [markets.data]);
  const update = useUpdateCopytrade(copy.id);
  const [sheet, setSheet] = useState<"funds" | "settings" | null>(null);
  const [stopOpen, setStopOpen] = useState(false);
  const live = copy.status === "active" || copy.status === "paused";
  const st = copy.settings;
  const editSettings = live ? { onPress: () => setSheet("settings") } : {};

  const togglePause = () =>
    update.mutate(
      { status: copy.status === "active" ? "paused" : "active" },
      {
        onSuccess: (c) => toast.success(c.status === "paused" ? "Copy paused" : "Copy resumed"),
        onError: (e) => toast.error(errorMessage(e, "Couldn't change the copy. Try again.")),
      },
    );

  const settingRows: [string, string][] = [
    ["Max leverage", `${st.max_leverage}×`],
    ["Stop loss", st.stop_loss_pct !== null ? `${plainNumber(st.stop_loss_pct)}%` : "Off"],
    ["Drawdown limit", st.max_drawdown_pct !== null ? `${plainNumber(st.max_drawdown_pct)}%` : "Off"],
    ["Max per order", money(st.max_trade_notional)],
    ["Max per market", money(st.max_position_notional)],
    ["Slippage", slippageText(st.slippage_bps)],
    ["If the trader stops", st.on_leader_exit === "close" ? "Close positions" : "Keep positions"],
  ];

  return (
    <Screen edges={["bottom"]} gap={s[8]} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <View style={{ gap: s[4] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
          <Avatar name={copy.leader.display_name} src={copy.leader.avatar_url} size="lg" />
          <View style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: s[2] }}>
            <Title numberOfLines={2} style={{ flexShrink: 1 }}>
              {copy.leader.display_name}
            </Title>
            <CopyStatusBadge status={copy.status} />
          </View>
        </View>
        <Notice copy={copy} />
        <StatGrid>
          <Stat label="Value">
            <Text role="titleSm" tabular numberOfLines={1}>
              {money(copy.equity)}
            </Text>
          </Stat>
          <Stat label="Profit and loss">
            <Text role="label" tabular tone={signTone(copy.pnl)}>
              {money(copy.pnl, true)} ({formatCopytradePct(copy.pnl_pct)})
            </Text>
          </Stat>
          <Stat label="Allocation">{money(copy.allocation)}</Stat>
          <Stat label="Free funds">{copy.account ? money(copy.account.free_collateral) : "–"}</Stat>
          <Stat label="Profit share">{`${plainNumber(copy.profit_share_pct)}%`}</Stat>
          <Stat label="Paid to the trader">{money(copy.profit_share_paid)}</Stat>
        </StatGrid>
        {live ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[3] }}>
            <Button
              variant="secondary"
              leftIcon={copy.status === "active" ? Pause : Play}
              loading={update.isPending}
              style={{ flexGrow: 1 }}
              onPress={togglePause}
            >
              {copy.status === "active" ? "Pause" : "Resume"}
            </Button>
            <Button variant="secondary" leftIcon={ArrowLeftRight} style={{ flexGrow: 1 }} onPress={() => setSheet("funds")}>
              Add or withdraw
            </Button>
          </View>
        ) : null}
      </View>

      {copy.account ? (
        <Group title="Positions">
          <Positions copy={copy} lookup={lookup} />
        </Group>
      ) : null}

      <ListGroup title="Settings">
        {settingRows.map(([title, value]) => (
          <ListRow
            key={title}
            title={title}
            trailing={
              <Text role="label" tone="muted" tabular>
                {value}
              </Text>
            }
            {...editSettings}
          />
        ))}
      </ListGroup>

      <Activity copy={copy} lookup={lookup} />

      {live ? (
        <ListGroup>
          <ListRow title="Stop copying" destructive chevron={false} onPress={() => setStopOpen(true)} />
        </ListGroup>
      ) : null}

      {live ? (
        <>
          <FundsSheet copy={copy} open={sheet === "funds"} onClose={() => setSheet(null)} />
          <SettingsSheet copy={copy} open={sheet === "settings"} onClose={() => setSheet(null)} />
          <StopDialog copy={copy} open={stopOpen} onClose={() => setStopOpen(false)} />
        </>
      ) : null}
    </Screen>
  );
}

/** One copy: value and PnL, pause or resume, funds, settings, the copy's positions, its activity, and stop. Stopped copies are read-only except closing kept positions. */
export function CopyDetailScreen({ id }: { id: string }) {
  const theme = useTheme();
  const s = theme.space;
  const copy = useCopytradeCopy(id);
  if (copy.isPending) {
    return (
      <Screen edges={["bottom"]} gap={s[8]}>
        <View style={{ gap: s[4] }} accessibilityLabel="Loading the copy" accessibilityRole="progressbar">
          <SkeletonRow />
          <Skeleton height={theme.tap.xl * 2} />
        </View>
        <Skeleton height={theme.tap.xl * 3} />
      </Screen>
    );
  }
  if (copy.isError) {
    return (
      <Screen edges={["bottom"]}>
        <ErrorState title="Couldn't load this copy" info={copy.error.message} onRetry={() => void copy.refetch()} retrying={copy.isFetching} />
      </Screen>
    );
  }
  return <CopyView copy={copy.data} refreshing={copy.isRefetching} onRefresh={() => void copy.refetch()} />;
}
