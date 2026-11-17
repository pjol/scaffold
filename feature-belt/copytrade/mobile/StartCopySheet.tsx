import { errorMessage, formatCopytradePct, useCopytradeConfig, useExchangeAccount, useStartCopytrade, type CopytradeLeaderCard } from "@scaffoldapp/shared";
import { Banner, Button, Card, Checkbox, ErrorState, Field, Input, SegmentedControl, Sheet, SkeletonText, Switch, Text, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import { Stat, StatGrid, useExchangeMoney } from "@/features/exchange/parts";
import { decimalInput, Facts, isDecimal, PctLimitField, PctText, pctError, PersonTitle, plainNumber, StepperRow } from "./parts";

type Exit = "close" | "keep";
type Block = { kind: "heading" | "para" | "bullet"; text: string };

/** The disclosure's Markdown as plain blocks: headings, paragraphs and bullets (continuation lines joined; ** and ## dropped). */
function disclosureBlocks(md: string): Block[] {
  const out: Block[] = [];
  let cur: Block | null = null;
  for (const raw of md.split("\n")) {
    const line = raw.trim();
    if (!line) {
      cur = null;
      continue;
    }
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    const bullet = /^[-*]\s+(.*)$/.exec(line);
    if (heading) {
      cur = null;
      out.push({ kind: "heading", text: heading[1] ?? "" });
    } else if (bullet) {
      cur = { kind: "bullet", text: bullet[1] ?? "" };
      out.push(cur);
    } else if (cur) {
      cur.text += ` ${line}`;
    } else {
      cur = { kind: "para", text: line };
      out.push(cur);
    }
  }
  return out.map((b) => ({ ...b, text: b.text.replace(/\*\*(.+?)\*\*/g, "$1").replace(/__(.+?)__/g, "$1").replace(/`/g, "").trim() }));
}

function Disclosure({ md }: { md: string }) {
  const theme = useTheme();
  const s = theme.space;
  const blocks = useMemo(() => disclosureBlocks(md), [md]);
  return (
    <View style={{ gap: s[3] }}>
      {blocks.map((b, i) =>
        b.kind === "heading" ? (
          <Text key={i} role="label" weight={600} accessibilityRole="header">
            {b.text}
          </Text>
        ) : b.kind === "bullet" ? (
          <View key={i} style={{ flexDirection: "row", gap: s[2] }}>
            <Text role="label" tone="muted" importantForAccessibility="no">
              •
            </Text>
            <Text role="label" tone="muted" style={{ flex: 1 }}>
              {b.text}
            </Text>
          </View>
        ) : (
          <Text key={i} role="label" tone="muted">
            {b.text}
          </Text>
        ),
      )}
    </View>
  );
}

/** Start copying one trader: allocation, risk limits, what happens when they stop, and the risk disclosure. */
export function StartCopySheet({ leader, open, onClose }: { leader: CopytradeLeaderCard | null; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const { money, currency } = useExchangeMoney();
  const config = useCopytradeConfig();
  const account = useExchangeAccount();
  const start = useStartCopytrade();
  const defaults = config.data?.default_settings;

  const [allocation, setAllocation] = useState("");
  const [leverage, setLeverage] = useState(3);
  const [stopOn, setStopOn] = useState(true);
  const [stopPct, setStopPct] = useState("30");
  const [copyOpen, setCopyOpen] = useState(true);
  const [onExit, setOnExit] = useState<Exit>("close");
  const [accepted, setAccepted] = useState(false);
  useEffect(() => {
    if (!open) return;
    setAllocation("");
    setLeverage(3);
    setStopOn(true);
    setStopPct("30");
    setCopyOpen(defaults?.copy_open_positions ?? true);
    setOnExit(defaults?.on_leader_exit ?? "close");
    setAccepted(false);
    // Reset only when the sheet opens or the trader changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, leader?.user.id]);

  const min = useMemo(() => {
    const a = config.data?.min_allocation ?? "0";
    const b = leader?.min_allocation ?? "0";
    return Number(b) > Number(a) ? b : a;
  }, [config.data?.min_allocation, leader?.min_allocation]);
  const free = account.data?.free_collateral;

  let allocError: string | undefined;
  if (allocation && !isDecimal(allocation)) allocError = "Enter an amount.";
  else if (allocation && Number(allocation) < Number(min)) allocError = `The minimum is ${money(min)}.`;
  else if (allocation && free !== undefined && Number(allocation) > Number(free)) allocError = `You have ${money(free)} free.`;
  const stopError = stopOn && stopPct ? pctError(stopPct, 5, 90) : undefined;

  let notice: string | null = null;
  if (config.data?.paused) notice = "Copy trading is paused. New copies can't start right now.";
  else if (leader?.is_me) notice = "You can't copy yourself.";
  else if (leader?.copying) notice = "You're already copying this trader.";
  else if (leader && !leader.accepting) notice = "This trader isn't taking new copiers.";

  const ready =
    !!leader &&
    !!config.data &&
    !notice &&
    isDecimal(allocation) &&
    Number(allocation) > 0 &&
    !allocError &&
    (!stopOn || (!!stopPct && !stopError)) &&
    accepted;

  const submit = () => {
    if (!leader || !config.data) return;
    const name = leader.user.display_name;
    start.mutate(
      {
        leader_id: leader.user.id,
        allocation,
        settings: { max_leverage: leverage, stop_loss_pct: stopOn ? stopPct : null, copy_open_positions: copyOpen, on_leader_exit: onExit },
        disclosure_version: config.data.disclosure_version,
      },
      {
        onSuccess: (c) => {
          haptic.success();
          toast.success(`Copying ${name}`);
          onClose();
          router.push({ pathname: "/copy/[id]", params: { id: c.id } });
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't start copying. Try again.")),
      },
    );
  };

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={leader ? `Copy ${leader.user.display_name}` : "Copy a trader"}
      snapPoints={["92%"]}
      footer={
        <Button fullWidth loading={start.isPending} disabled={!ready} onPress={submit}>
          {isDecimal(allocation) && Number(allocation) > 0 ? `Start copying with ${money(allocation)}` : "Start copying"}
        </Button>
      }
    >
      {leader ? (
        <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: s[8], paddingBottom: s[6] }}>
          <View style={{ gap: s[4] }}>
            {notice ? <Banner tone="warning" title={notice} /> : null}
            <PersonTitle person={leader.user} size="md" />
            <StatGrid>
              <Stat label="90-day return">
                <PctText pct={leader.return_90d_pct} />
              </Stat>
              <Stat label="Max drawdown">{formatCopytradePct(leader.max_drawdown_90d_pct, false)}</Stat>
              <Stat label="Copiers">{String(leader.copiers)}</Stat>
              <Stat label="Profit share">{`${plainNumber(leader.profit_share_pct)}%`}</Stat>
            </StatGrid>
          </View>

          <View style={{ gap: s[4] }}>
            <Field
              label="Allocation"
              info="Moved from your main account into a separate copy account. You can add or withdraw free funds later."
              error={allocError}
              labelEnd={
                free !== undefined && Number(free) >= Number(min) ? (
                  <Pressable accessibilityRole="button" accessibilityLabel="Use all free collateral" hitSlop={12} onPress={() => setAllocation(free)}>
                    <Text role="caption" tone="primary" weight={600}>
                      Max
                    </Text>
                  </Pressable>
                ) : null
              }
            >
              <Input
                keyboardType="decimal-pad"
                value={allocation}
                onChangeText={(t) => setAllocation(decimalInput(t))}
                placeholder={plainNumber(min)}
                trailing={
                  <Text role="caption" tone="muted">
                    {currency}
                  </Text>
                }
              />
            </Field>
            <Facts
              rows={[
                ["Minimum", money(min)],
                ["Free collateral", free !== undefined ? money(free) : account.isError ? "Couldn't load" : "–"],
              ]}
            />
          </View>

          <View style={{ gap: s[4] }}>
            <StepperRow
              label="Max leverage"
              info="Copied positions use the trader's leverage, up to this."
              value={leverage}
              min={1}
              max={10}
              onValueChange={setLeverage}
              format={(n) => `${n}×`}
            />
            <PctLimitField
              label="Stop loss"
              info="Stops the copy and closes its positions once it has lost this share of what you put in."
              on={stopOn}
              onToggle={setStopOn}
              value={stopPct}
              onChange={setStopPct}
              error={stopError}
            />
            <Switch
              label="Copy open positions"
              info="On: opens the trader's current positions now. Off: only their next trades are copied."
              value={copyOpen}
              onValueChange={setCopyOpen}
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
          </View>

          <View style={{ gap: s[4] }}>
            {leader.show_copiers ? <Banner tone="info" title="This trader sees the names of people who copy them." /> : null}
            {config.isPending ? (
              <SkeletonText lines={6} />
            ) : config.isError ? (
              <ErrorState compact title="Couldn't load the risk disclosure" info={config.error.message} onRetry={() => void config.refetch()} retrying={config.isFetching} />
            ) : (
              <>
                <Card variant="sunken" padding="compact">
                  <Disclosure md={config.data.disclosure_md} />
                </Card>
                <Checkbox checked={accepted} onCheckedChange={setAccepted} label="I've read and accept the risk disclosure" />
              </>
            )}
          </View>
        </ScrollView>
      ) : null}
    </Sheet>
  );
}
