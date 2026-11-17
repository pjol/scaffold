import {
  errorMessage,
  formatCopytradePct,
  useCopytradeConfig,
  useCopytradeLeader,
  useCopytradeLeaders,
  type CopytradeLeaderCard,
  type CopytradeLeaderSort,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, EmptyState, ErrorState, Screen, SkeletonRow, ToggleChip, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, useLocalSearchParams } from "expo-router";
import { Users } from "lucide-react-native";
import { useEffect, useState } from "react";
import { RefreshControl, ScrollView, View } from "react-native";
import { useExchangeMoney } from "@/features/exchange/parts";
import { Item, PctText, PersonTitle, plainNumber } from "./parts";
import { StartCopySheet } from "./StartCopySheet";

const SORTS: { value: CopytradeLeaderSort; label: string }[] = [
  { value: "return_90d", label: "90-day return" },
  { value: "return_30d", label: "30-day return" },
  { value: "copiers", label: "Copiers" },
  { value: "aum", label: "Assets" },
];

function LeaderRow({ l, sort, onPick }: { l: CopytradeLeaderCard; sort: CopytradeLeaderSort; onPick: (l: CopytradeLeaderCard) => void }) {
  const { money } = useExchangeMoney();
  const thirty = sort === "return_30d";
  const ret = thirty ? l.return_30d_pct : l.return_90d_pct;
  const open = () => {
    if (l.is_me) router.push("/settings/copy-trading");
    else if (l.copying) router.push("/copy");
    else onPick(l);
  };
  return (
    <Item
      onPress={open}
      accessibilityLabel={`${l.user.display_name}${l.is_me ? ", you" : ""}, ${thirty ? "30" : "90"}-day return ${formatCopytradePct(ret)}`}
      title={
        <PersonTitle person={l.user}>
          {l.is_me ? (
            <Badge size="sm" tone="primary">
              You
            </Badge>
          ) : l.copying ? (
            <Badge size="sm" tone="success">
              Copying
            </Badge>
          ) : !l.accepting ? (
            <Badge size="sm">Not taking copiers</Badge>
          ) : null}
        </PersonTitle>
      }
      cells={[
        [thirty ? "30-day return" : "90-day return", <PctText key="ret" pct={ret} />],
        ["Max drawdown", formatCopytradePct(l.max_drawdown_90d_pct, false)],
        sort === "aum" ? ["Assets", money(l.aum)] : ["Copiers", String(l.copiers)],
        ["Profit share", `${plainNumber(l.profit_share_pct)}%`],
      ]}
    />
  );
}

/** Traders who offer copy trading, sorted by return, copiers or assets. `?leader=<user id>` opens that trader's start sheet. */
export function LeadersScreen() {
  const theme = useTheme();
  const s = theme.space;
  const params = useLocalSearchParams<{ leader?: string }>();
  const wanted = typeof params.leader === "string" && params.leader ? params.leader : null;
  const [sort, setSort] = useState<CopytradeLeaderSort>("return_90d");
  const leaders = useCopytradeLeaders(sort);
  const config = useCopytradeConfig();

  // The sheet's trader: from the list when it's there, fetched on its own otherwise (a link from elsewhere).
  const [picked, setPicked] = useState<string | null>(wanted);
  useEffect(() => {
    if (wanted) setPicked(wanted);
  }, [wanted]);
  const listed = picked ? leaders.data?.items.find((l) => l.user.id === picked) : undefined;
  const fetched = useCopytradeLeader(picked && !listed ? picked : null);
  const card = listed ?? (picked ? fetched.data : undefined);
  const [shown, setShown] = useState<CopytradeLeaderCard | null>(null);
  useEffect(() => {
    if (card) setShown(card);
  }, [card]);
  useEffect(() => {
    if (picked && !listed && fetched.isError) {
      toast.error(errorMessage(fetched.error, "Couldn't find that trader."));
      setPicked(null);
    }
  }, [picked, listed, fetched.isError, fetched.error]);

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={leaders.isRefetching} onRefresh={() => void leaders.refetch()} />}>
      {config.data?.paused ? <Banner tone="warning" title="Copy trading is paused. New copies can't start right now." /> : null}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }} accessibilityLabel="Sort traders">
        {SORTS.map((o) => (
          <ToggleChip key={o.value} pressed={sort === o.value} onPressedChange={() => setSort(o.value)}>
            {o.label}
          </ToggleChip>
        ))}
      </ScrollView>
      {leaders.isPending ? (
        <View style={{ gap: s[2] }} accessibilityLabel="Loading traders" accessibilityRole="progressbar">
          {Array.from({ length: 5 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : leaders.isError ? (
        <ErrorState title="Couldn't load traders" info={leaders.error.message} onRetry={() => void leaders.refetch()} retrying={leaders.isFetching} />
      ) : leaders.data.items.length === 0 ? (
        <EmptyState
          icon={Users}
          title="No traders to copy yet"
          action={
            <Button variant="secondary" onPress={() => router.push("/settings/copy-trading")}>
              Offer copy trading
            </Button>
          }
        />
      ) : (
        <View>
          {leaders.data.items.map((l) => (
            <LeaderRow key={l.user.id} l={l} sort={sort} onPick={(x) => setPicked(x.user.id)} />
          ))}
        </View>
      )}
      <StartCopySheet leader={shown} open={!!picked && !!card} onClose={() => setPicked(null)} />
    </Screen>
  );
}
