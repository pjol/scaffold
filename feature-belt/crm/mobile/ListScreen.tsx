import { CRM_SOURCE_APP, CRM_TYPE_LABEL, crmTotal, useCrmRecords, type CrmObjectType, type CrmRecord, type CrmSources } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, SegmentedControl, SkeletonRow, SourceBadge, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Search } from "lucide-react-native";
import { useEffect, useState } from "react";
import { RefreshControl, View } from "react-native";
import { CrmGate, MobileSourceFilter, TYPE_ICON, dayLabel, money, recordPath, useMobileSource } from "./parts";

/** A list of contacts, companies or deals you can see: search, yours or everyone's. */
export function CrmListScreen({ type, mine: initialMine }: { type: CrmObjectType; mine?: boolean }) {
  return <CrmGate>{({ orgId, sources }) => <List orgId={orgId} type={type} initialMine={!!initialMine} sources={sources} />}</CrmGate>;
}

function detail(r: CrmRecord): string {
  switch (r.type) {
    case "contact":
      return r.company?.name ?? r.emails[0] ?? r.job_title;
    case "company":
      return r.domain ?? r.industry;
    default:
      return [r.stage.name, r.amount !== null ? money(r.amount, r.currency) : "", r.close_date ? dayLabel(r.close_date) : ""].filter(Boolean).join(" · ");
  }
}

function List({ orgId, type, initialMine, sources }: { orgId: string; type: CrmObjectType; initialMine: boolean; sources: CrmSources | undefined }) {
  const theme = useTheme();
  const src = useMobileSource(sources, "list");
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [who, setWho] = useState<"all" | "mine">(initialMine ? "mine" : "all");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 300);
    return () => clearTimeout(t);
  }, [q]);
  const list = useCrmRecords(orgId, type, {
    q: debounced,
    filters: who === "mine" ? [{ key: "owner", op: "is_any", value: ["me"] }] : [],
    sort: type === "deal" ? "close_date" : "name",
    dir: "asc",
    limit: 30,
    ...(src.value !== CRM_SOURCE_APP ? { source: src.value } : {}),
  });
  const total = crmTotal(list.data);
  const Icon = TYPE_ICON[type];
  return (
    <Screen edges={["bottom"]} gap={theme.space[4]} refreshControl={<RefreshControl refreshing={list.isRefetching} onRefresh={() => void list.refetch()} />}>
      <Stack.Screen options={{ title: CRM_TYPE_LABEL[type].Many }} />
      <View style={{ gap: theme.space[3] }}>
        <MobileSourceFilter state={src} />
        <Input value={q} onChangeText={setQ} placeholder={`Search ${CRM_TYPE_LABEL[type].many}`} leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel={`Search ${CRM_TYPE_LABEL[type].many}`} />
        <SegmentedControl<"all" | "mine">
          accessibilityLabel="Whose"
          value={who}
          onValueChange={setWho}
          options={[
            { value: "all", label: "Everyone's" },
            { value: "mine", label: "Mine" },
          ]}
        />
        {total !== null ? (
          <Text role="caption" tone="muted">
            {total} {total === 1 ? CRM_TYPE_LABEL[type].one : CRM_TYPE_LABEL[type].many}
          </Text>
        ) : null}
      </View>
      <ListGroup>
        {list.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : list.isError ? (
          <ErrorState compact title={`Couldn't load ${CRM_TYPE_LABEL[type].many}`} info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
        ) : list.data.items.length === 0 ? (
          <EmptyState compact icon={Icon} title={debounced || who === "mine" ? `No ${CRM_TYPE_LABEL[type].many} match` : `No ${CRM_TYPE_LABEL[type].many} yet`} />
        ) : (
          list.data.items.map((r) => (
            <ListRow
              key={r.id}
              title={r.title}
              leading={<Icon size={20} color={theme.colors.textMuted} />}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                  <SourceBadge source={r.source} iconOnly />
                  <Text role="caption" tone="muted" numberOfLines={1} style={{ maxWidth: 160 }}>
                    {detail(r)}
                  </Text>
                </View>
              }
              onPress={() => router.push(recordPath(r.type, r.id))}
            />
          ))
        )}
      </ListGroup>
      {list.hasNextPage ? (
        <Button variant="ghost" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </Screen>
  );
}
