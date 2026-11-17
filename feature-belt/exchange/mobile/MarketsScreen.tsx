import { compactExchangeNumber, formatExchangePrice, useExchangeMarkets, useExchangeMarketsTopic, useExchangeSparklines, type ExchangeMarket } from "@scaffoldapp/shared";
import { EmptyState, ErrorState, IconButton, Input, SkeletonRow, Text, Title, ToggleChip, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router, useScrollToTop } from "expo-router";
import { CandlestickChart, Search, SearchX, WalletCards } from "lucide-react-native";
import { useMemo, useRef, useState } from "react";
import { FlatList, Pressable, RefreshControl, ScrollView, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ChangeText, MarketStatusBadge, Sparkline } from "./parts";

function Row({ m, closes }: { m: ExchangeMarket; closes: string[] | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${m.name}, ${m.stats.mark_px}, ${m.stats.day_change_pct} percent today`}
      onPress={() => router.push({ pathname: "/markets/[symbol]", params: { symbol: m.symbol } })}
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
      {m.image_url ? <Image source={{ uri: m.image_url }} style={{ width: 32, height: 32, borderRadius: theme.radius.md }} accessibilityIgnoresInvertColors /> : null}
      <View style={{ flex: 1, minWidth: 0, gap: s[0.5] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
            {m.name}
          </Text>
          <MarketStatusBadge market={m} />
        </View>
        <Text role="caption" tone="muted" numberOfLines={1}>
          {m.symbol} · vol {compactExchangeNumber(m.stats.day_volume)}
        </Text>
      </View>
      <Sparkline closes={closes} label={`${m.name}, last 24 hours`} />
      <View style={{ alignItems: "flex-end", minWidth: 80 }}>
        <Text role="label" tabular>
          {formatExchangePrice(m.stats.mark_px, m.tick_size)}
        </Text>
        <ChangeText pct={m.stats.day_change_pct} />
      </View>
    </Pressable>
  );
}

/** Markets tab: every market with live price, 24h change and a day's sparkline; search and category filter. */
export function MarketsScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const s = theme.space;
  useExchangeMarketsTopic();
  const markets = useExchangeMarkets();
  const sparks = useExchangeSparklines("1h", 24);
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<string | null>(null);
  const listRef = useRef<FlatList<ExchangeMarket>>(null);
  useScrollToTop(listRef);
  const all = markets.data?.items ?? [];
  const categories = useMemo(() => [...new Set(all.map((m) => m.category).filter(Boolean))].sort(), [all]);
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return all
      .filter((m) => (!category || m.category === category) && (!q || `${m.symbol} ${m.name} ${m.category}`.toLowerCase().includes(q)))
      .sort((a, b) => Number(b.stats.day_volume) - Number(a.stats.day_volume));
  }, [all, query, category]);
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };

  const header = (
    <View style={{ gap: s[4], paddingTop: insets.top + s[4], paddingBottom: s[3], ...pad }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Title style={{ flex: 1 }}>Markets</Title>
        <IconButton label="Portfolio" icon={WalletCards} variant="soft" onPress={() => router.push("/portfolio")} />
      </View>
      <Input accessibilityLabel="Search markets" leadingIcon={Search} value={query} onChangeText={setQuery} placeholder="Search markets" autoCorrect={false} returnKeyType="search" />
      {categories.length > 1 && (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
          <ToggleChip pressed={!category} onPressedChange={() => setCategory(null)}>
            All
          </ToggleChip>
          {categories.map((c) => (
            <ToggleChip key={c} pressed={category === c} onPressedChange={(on) => setCategory(on ? c : null)}>
              {c}
            </ToggleChip>
          ))}
        </ScrollView>
      )}
    </View>
  );

  let body;
  if (markets.isPending) {
    body = (
      <View style={{ gap: s[2], ...pad }} accessibilityLabel="Loading markets" accessibilityRole="progressbar">
        {Array.from({ length: 6 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  } else if (markets.isError) {
    body = <ErrorState title="Couldn't load markets" info={markets.error.message} onRetry={() => void markets.refetch()} retrying={markets.isFetching} />;
  } else {
    body = (
      <FlatList
        ref={listRef}
        data={rows}
        keyExtractor={(m) => m.symbol}
        renderItem={({ item }) => <Row m={item} closes={sparks.data?.[item.symbol]} />}
        contentContainerStyle={{ ...pad, paddingBottom: s[8], flexGrow: 1 }}
        refreshControl={<RefreshControl refreshing={markets.isRefetching} onRefresh={() => void markets.refetch()} tintColor={theme.colors.textMuted} />}
        ListEmptyComponent={all.length === 0 ? <EmptyState icon={CandlestickChart} title="No markets listed yet" /> : <EmptyState icon={SearchX} title="No markets match" />}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
      {header}
      <View style={{ flex: 1 }}>{body}</View>
    </View>
  );
}
