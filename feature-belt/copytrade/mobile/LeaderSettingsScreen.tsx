import {
  copytradeAllocationLabel,
  errorMessage,
  useCopytradeConfig,
  useCopytradeCopiers,
  useMyCopytradeLeader,
  useSetMyCopytradeLeader,
  type CopytradeCopier,
  type CopytradeLeaderSettings,
  type CopytradeLeaderStatus,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Button,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  Input,
  ListGroup,
  ListRow,
  Screen,
  Skeleton,
  SkeletonRow,
  Spinner,
  Switch,
  Text,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { CircleCheck, CircleDashed, Users } from "lucide-react-native";
import { useEffect, useState } from "react";
import { RefreshControl, View } from "react-native";
import { Stat, StatGrid, useExchangeMoney } from "@/features/exchange/parts";
import { day, decimalInput, Group, isDecimal, plainNumber } from "./parts";

function Requirements({ st }: { st: CopytradeLeaderStatus }) {
  const theme = useTheme();
  if (st.requirements.length === 0) return null;
  return (
    <ListGroup title="Requirements" info="You can offer copy trading once you meet all of these.">
      {st.requirements.map((r) => (
        <ListRow
          key={r.key}
          title={r.label}
          leading={
            r.met ? (
              <CircleCheck size={24} color={theme.colors.successStrong} accessibilityLabel="Met" />
            ) : (
              <CircleDashed size={24} color={theme.colors.textMuted} accessibilityLabel="Not met yet" />
            )
          }
          trailing={
            r.met || !r.progress ? undefined : (
              <Text role="caption" tone="muted" tabular>
                {r.progress}
              </Text>
            )
          }
        />
      ))}
    </ListGroup>
  );
}

function CopierRow({ c }: { c: CopytradeCopier }) {
  const theme = useTheme();
  const s = theme.space;
  const { currency } = useExchangeMoney();
  const name = c.user?.display_name ?? "Private copier";
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[3], minHeight: theme.tap.lg, borderBottomWidth: 1, borderBottomColor: theme.colors.border }}>
      <Avatar name={name} src={c.user?.avatar_url ?? null} size="sm" />
      <Text role="label" weight={c.user ? 600 : 400} tone={c.user ? "default" : "muted"} numberOfLines={1} style={{ flex: 1 }}>
        {name}
      </Text>
      <View style={{ alignItems: "flex-end", gap: s[0.5] }}>
        <Text role="label" tabular>
          {copytradeAllocationLabel(c.allocation, currency)}
        </Text>
        <Text role="caption" tone="muted">
          Since {day(c.since)}
        </Text>
      </View>
    </View>
  );
}

function Copiers() {
  const theme = useTheme();
  const s = theme.space;
  const copiers = useCopytradeCopiers();
  return (
    <Group title="Copiers" info="Names show only for people who started copying while you showed copiers' names.">
      {copiers.isPending ? (
        <View style={{ gap: s[2] }} accessibilityLabel="Loading copiers" accessibilityRole="progressbar">
          {[0, 1, 2].map((i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : copiers.isError ? (
        <ErrorState compact title="Couldn't load copiers" info={copiers.error.message} onRetry={() => void copiers.refetch()} retrying={copiers.isFetching} />
      ) : copiers.data.items.length === 0 ? (
        <EmptyState compact icon={Users} title="Nobody copies you yet" />
      ) : (
        <View>
          {copiers.data.items.map((c, i) => (
            <CopierRow key={`${c.user?.id ?? "private"}-${c.since}-${i}`} c={c} />
          ))}
          {copiers.hasNextPage &&
            (copiers.isFetchingNextPage ? (
              <Spinner />
            ) : (
              <Button variant="ghost" onPress={() => void copiers.fetchNextPage()}>
                Load more
              </Button>
            ))}
        </View>
      )}
    </Group>
  );
}

type Draft = { enabled: boolean; profitShare: string; minAllocation: string; maxCopiers: string; showCopiers: boolean };

function draftOf(st: CopytradeLeaderSettings): Draft {
  return {
    enabled: st.enabled,
    profitShare: plainNumber(st.profit_share_pct),
    minAllocation: plainNumber(st.min_allocation),
    maxCopiers: String(st.max_copiers),
    showCopiers: st.show_copiers,
  };
}

/** Settings → Copy trading: eligibility, the offer (profit share, minimum, copier cap, names), your numbers and your copiers. */
export function LeaderSettingsScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { money, currency } = useExchangeMoney();
  const me = useMyCopytradeLeader();
  const config = useCopytradeConfig();
  const save = useSetMyCopytradeLeader();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [confirmOff, setConfirmOff] = useState(false);
  const serverKey = me.data ? JSON.stringify(me.data.settings) : "";
  useEffect(() => {
    if (me.data) setDraft(draftOf(me.data.settings));
    // Reset the form whenever the saved settings change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverKey]);

  const st = me.data;
  const cfg = config.data;
  const maxShare = cfg ? Number(cfg.max_profit_share_pct) : 100;
  const minAlloc = cfg?.min_allocation ?? "0";
  const maxCopiersCap = cfg?.max_copiers_per_leader ?? 10000;

  let shareError: string | undefined;
  let allocError: string | undefined;
  let copiersError: string | undefined;
  let patch: Partial<CopytradeLeaderSettings> = {};
  if (st && draft) {
    if (!isDecimal(draft.profitShare) || Number(draft.profitShare) > maxShare) shareError = `Use 0–${plainNumber(cfg?.max_profit_share_pct ?? "100")}%.`;
    if (!isDecimal(draft.minAllocation) || Number(draft.minAllocation) < Number(minAlloc)) allocError = `At least ${money(minAlloc)}.`;
    if (!/^\d+$/.test(draft.maxCopiers) || Number(draft.maxCopiers) < 1 || Number(draft.maxCopiers) > maxCopiersCap) copiersError = `Use 1–${maxCopiersCap}.`;
    const cur = st.settings;
    patch = {
      ...(draft.enabled !== cur.enabled ? { enabled: draft.enabled } : {}),
      ...(Number(draft.profitShare) !== Number(cur.profit_share_pct) ? { profit_share_pct: draft.profitShare } : {}),
      ...(Number(draft.minAllocation) !== Number(cur.min_allocation) ? { min_allocation: draft.minAllocation } : {}),
      ...(Number(draft.maxCopiers) !== cur.max_copiers ? { max_copiers: Number(draft.maxCopiers) } : {}),
      ...(draft.showCopiers !== cur.show_copiers ? { show_copiers: draft.showCopiers } : {}),
    };
  }
  const dirty = Object.keys(patch).length > 0;
  const valid = !shareError && !allocError && !copiersError;
  const turningOff = !!st && st.settings.enabled && draft?.enabled === false;

  const submit = () =>
    save.mutateAsync(patch).then(
      (next) => toast.success(next.settings.enabled ? "Saved" : "You're not offering copy trading"),
      (e: unknown) => {
        toast.error(errorMessage(e, "Couldn't save. Try again."));
      },
    );
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((d) => (d ? { ...d, [k]: v } : d));

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={<RefreshControl refreshing={me.isRefetching} onRefresh={() => void me.refetch()} />}
      footer={
        st && draft ? (
          <Button fullWidth loading={save.isPending} disabled={!dirty || !valid} onPress={() => (turningOff && st.copiers > 0 ? setConfirmOff(true) : void submit())}>
            Save
          </Button>
        ) : undefined
      }
    >
      {me.isPending ? (
        <View style={{ gap: s[3] }} accessibilityLabel="Loading copy trading settings" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.base * 3} />
          <Skeleton height={theme.tap.base * 4} />
        </View>
      ) : me.isError ? (
        <ErrorState title="Couldn't load your copy trading settings" info={me.error.message} onRetry={() => void me.refetch()} retrying={me.isFetching} />
      ) : !draft ? null : (
        <>
          <Requirements st={me.data} />

          <View style={{ gap: s[4] }}>
            <Switch
              label="Offer copy trading"
              info={me.data.eligible || me.data.settings.enabled ? "People can find you under Traders and copy your trades." : "Meet the requirements to turn this on."}
              value={draft.enabled}
              onValueChange={(v) => set("enabled", v)}
              disabled={!me.data.eligible && !me.data.settings.enabled}
            />
            <Field label="Profit share" info="Your share of a copier's gains above their previous high, paid at each settlement." error={shareError}>
              <Input
                keyboardType="decimal-pad"
                value={draft.profitShare}
                onChangeText={(t) => set("profitShare", decimalInput(t))}
                placeholder="0"
                trailing={
                  <Text role="caption" tone="muted">
                    %
                  </Text>
                }
              />
            </Field>
            <Field label="Minimum allocation" info="The least someone can start copying you with." error={allocError}>
              <Input
                keyboardType="decimal-pad"
                value={draft.minAllocation}
                onChangeText={(t) => set("minAllocation", decimalInput(t))}
                placeholder={plainNumber(minAlloc)}
                trailing={
                  <Text role="caption" tone="muted">
                    {currency}
                  </Text>
                }
              />
            </Field>
            <Field label="Max copiers" info="New people can't start copying you once this many do." error={copiersError}>
              <Input keyboardType="number-pad" value={draft.maxCopiers} onChangeText={(t) => set("maxCopiers", t.replace(/[^0-9]/g, ""))} placeholder="1" />
            </Field>
            <Switch
              label="Show copiers' names"
              info="Applies only to people who start copying while this is on. Everyone else stays private."
              value={draft.showCopiers}
              onValueChange={(v) => set("showCopiers", v)}
            />
          </View>

          <Group title="Your numbers">
            <StatGrid>
              <Stat label="Copiers">{String(me.data.copiers)}</Stat>
              <Stat label="Assets copying you">{money(me.data.aum)}</Stat>
              <Stat label="Profit share earned">{money(me.data.profit_share_earned)}</Stat>
            </StatGrid>
          </Group>

          <Copiers />

          <ConfirmDialog
            open={confirmOff}
            onClose={() => setConfirmOff(false)}
            title="Stop offering copy trading?"
            tone="danger"
            confirmLabel="Stop offering"
            onConfirm={() => save.mutateAsync(patch).then(() => toast.success("You're not offering copy trading"))}
            errorText={(e) => errorMessage(e, "Couldn't save. Try again.")}
          >
            <Text role="body">
              {me.data.copiers === 1 ? "The 1 person copying you stops copying." : `The ${me.data.copiers} people copying you stop copying.`}
            </Text>
          </ConfirmDialog>
        </>
      )}
    </Screen>
  );
}
