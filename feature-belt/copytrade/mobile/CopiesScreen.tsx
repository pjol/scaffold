import { copytradeStatusLabel, formatCopytradePct, useCopytradeConfig, useCopytradeCopies, type CopytradeCopy, type PagedQueryResult } from "@scaffoldapp/shared";
import { Avatar, Banner, Button, EmptyState, ErrorState, Screen, SkeletonRow, Spinner, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Copy, Search } from "lucide-react-native";
import { Pressable, RefreshControl, View } from "react-native";
import { useExchangeMoney } from "@/features/exchange/parts";
import { CopyStatusBadge, Group, PctText } from "./parts";

function CopyRow({ c }: { c: CopytradeCopy }) {
  const theme = useTheme();
  const s = theme.space;
  const { money } = useExchangeMoney();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${c.leader.display_name}, ${copytradeStatusLabel(c.status)}, ${money(c.equity)}, ${formatCopytradePct(c.pnl_pct)}`}
      onPress={() => router.push({ pathname: "/copy/[id]", params: { id: c.id } })}
      style={({ pressed }) => ({
        flexDirection: "row",
        alignItems: "center",
        gap: s[3],
        paddingVertical: s[3],
        minHeight: theme.tap.xl,
        borderBottomWidth: 1,
        borderBottomColor: theme.colors.border,
        backgroundColor: pressed ? theme.colors.sunken : "transparent",
      })}
    >
      <Avatar name={c.leader.display_name} src={c.leader.avatar_url} size="md" />
      <View style={{ flex: 1, minWidth: 0, gap: s[1] }}>
        <Text role="label" weight={600} numberOfLines={1}>
          {c.leader.display_name}
        </Text>
        <CopyStatusBadge status={c.status} />
      </View>
      <View style={{ alignItems: "flex-end", gap: s[0.5] }}>
        <Text role="label" tabular>
          {money(c.equity)}
        </Text>
        <PctText pct={c.pnl_pct} role="caption" />
      </View>
    </Pressable>
  );
}

function CopyRows({ copies }: { copies: PagedQueryResult<CopytradeCopy> }) {
  if (!copies.data) return null;
  return (
    <View>
      {copies.data.items.map((c) => (
        <CopyRow key={c.id} c={c} />
      ))}
      {copies.hasNextPage &&
        (copies.isFetchingNextPage ? (
          <Spinner />
        ) : (
          <Button variant="ghost" onPress={() => void copies.fetchNextPage()}>
            Load more
          </Button>
        ))}
    </View>
  );
}

/** Your copies: the ones running (or paused) first, then the stopped ones. */
export function CopiesScreen() {
  const theme = useTheme();
  const s = theme.space;
  const open = useCopytradeCopies("open");
  const stopped = useCopytradeCopies("stopped");
  const config = useCopytradeConfig();
  const none = open.data?.items.length === 0;
  const findTraders = () => router.push("/leaders");

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={
        <RefreshControl
          refreshing={open.isRefetching || stopped.isRefetching}
          onRefresh={() => {
            void open.refetch();
            void stopped.refetch();
          }}
        />
      }
      footer={
        none ? undefined : (
          <Button fullWidth leftIcon={Search} onPress={findTraders}>
            Find traders
          </Button>
        )
      }
    >
      {config.data?.paused ? <Banner tone="warning" title="Copy trading is paused. Trades aren't copied right now; stops and withdrawals still work." /> : null}
      <Group title="Copying">
        {open.isPending ? (
          <View style={{ gap: s[2] }} accessibilityLabel="Loading your copies" accessibilityRole="progressbar">
            {[0, 1, 2].map((i) => (
              <SkeletonRow key={i} />
            ))}
          </View>
        ) : open.isError ? (
          <ErrorState title="Couldn't load your copies" info={open.error.message} onRetry={() => void open.refetch()} retrying={open.isFetching} />
        ) : open.data.items.length === 0 ? (
          <EmptyState
            icon={Copy}
            title="You're not copying anyone"
            action={
              <Button leftIcon={Search} onPress={findTraders}>
                Find traders
              </Button>
            }
          />
        ) : (
          <CopyRows copies={open} />
        )}
      </Group>
      {stopped.isError ? (
        <Group title="Stopped">
          <ErrorState compact title="Couldn't load stopped copies" info={stopped.error.message} onRetry={() => void stopped.refetch()} retrying={stopped.isFetching} />
        </Group>
      ) : stopped.data && stopped.data.items.length > 0 ? (
        <Group title="Stopped">
          <CopyRows copies={stopped} />
        </Group>
      ) : null}
    </Screen>
  );
}
