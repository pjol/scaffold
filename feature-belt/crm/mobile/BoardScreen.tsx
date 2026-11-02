import { CRM_SOURCE_APP, crmMoney, crmMoneyTotals, useCrmBoard, useCrmPipelines, useCrmRecords, type CrmDeal, type CrmSources } from "@scaffoldapp/shared";
import { Button, Chip, EmptyState, ErrorState, IconButton, ListGroup, Screen, SegmentedControl, Skeleton, SourceBadge, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { ArrowRightLeft, Handshake } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { StageSheet } from "./RecordScreen";
import { CrmGate, MobileSourceFilter, dayLabel, recordPath, useMobileSource } from "./parts";

/** The deals board on mobile: pick a stage, see its deals and totals, move a deal to another stage. */
export function CrmBoardScreen() {
  return <CrmGate>{({ orgId, settings, sources }) => <Board orgId={orgId} currency={settings.currency} sources={sources} />}</CrmGate>;
}

function Board({ orgId, currency, sources }: { orgId: string; currency: string; sources: CrmSources | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const src = useMobileSource(sources, "board");
  const sourceId = src.connection ? src.value : CRM_SOURCE_APP;
  const pipelines = useCrmPipelines(orgId, sourceId);
  const [pipelineId, setPipelineId] = useState<string | undefined>(undefined);
  useEffect(() => setPipelineId(undefined), [sourceId]);
  const [who, setWho] = useState<"all" | "me">("all");
  const board = useCrmBoard(orgId, pipelineId, {
    filters: who === "me" ? [{ key: "owner", op: "is_any", value: ["me"] }] : [],
    ...(sourceId !== CRM_SOURCE_APP ? { source: sourceId } : {}),
  });
  const [stageId, setStageId] = useState<string | null>(null);
  const [moving, setMoving] = useState<CrmDeal | null>(null);
  const columns = board.data?.columns ?? [];
  useEffect(() => {
    if (!stageId || !columns.some((c) => c.stage.id === stageId)) setStageId(columns[0]?.stage.id ?? null);
  }, [columns, stageId]);
  const col = columns.find((c) => c.stage.id === stageId);
  const all = useCrmRecords(
    orgId,
    "deal",
    {
      filters: [...(who === "me" ? [{ key: "owner", op: "is_any" as const, value: ["me"] }] : []), { key: "stage", op: "is_any", value: [stageId ?? ""] }],
      sort: "close_date",
      dir: "asc",
      limit: 50,
      ...(sourceId !== CRM_SOURCE_APP ? { source: sourceId } : {}),
    },
    { enabled: !!col && !!col.next_cursor },
  );
  const deals = col?.next_cursor && all.data ? all.data.items : (col?.deals ?? []);

  return (
    <Screen edges={["bottom"]} gap={s[4]} refreshControl={<RefreshControl refreshing={board.isRefetching} onRefresh={() => void board.refetch()} />}>
      <Stack.Screen options={{ title: board.data?.pipeline.name ?? "Deals board" }} />
      <MobileSourceFilter state={src} />
      {pipelines.data && pipelines.data.length > 1 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
          {pipelines.data.map((p) => (
            <Chip key={p.id} size="sm" selected={(pipelineId ?? pipelines.data?.[0]?.id) === p.id} onPress={() => setPipelineId(p.id)}>
              {p.name}
            </Chip>
          ))}
        </ScrollView>
      ) : null}
      <SegmentedControl<"all" | "me">
        accessibilityLabel="Whose deals"
        value={who}
        onValueChange={setWho}
        options={[
          { value: "all", label: "Everyone's" },
          { value: "me", label: "Mine" },
        ]}
      />
      {board.isPending ? (
        <View style={{ gap: s[3] }}>
          <Skeleton height={theme.tap.lg} />
          <Skeleton height={theme.tap.lg * 3} />
        </View>
      ) : board.isError ? (
        <ErrorState title="Couldn't load the board" info={board.error.message} onRetry={() => void board.refetch()} retrying={board.isRefetching} />
      ) : (
        <>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
            <SourceBadge source={board.data.source} />
            <Text role="label" tone="muted">
              Open {crmMoneyTotals(board.data.open_totals, currency)} · weighted {crmMoneyTotals(board.data.open_totals, currency, "weighted")}
            </Text>
          </View>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }} accessibilityRole="tablist">
            {columns.map((c) => (
              <Chip key={c.stage.id} size="sm" selected={c.stage.id === stageId} count={c.count} onPress={() => setStageId(c.stage.id)} accessibilityRole="tab">
                {c.stage.name}
              </Chip>
            ))}
          </ScrollView>
          {col ? (
            <Text role="caption" tone="muted">
              {crmMoneyTotals(col.totals, currency)}
              {col.stage.outcome === "open" ? ` · ${col.stage.probability}% → ${crmMoneyTotals(col.totals, currency, "weighted")}` : ""}
            </Text>
          ) : null}
          <ListGroup>
            {deals.length === 0 ? (
              <EmptyState compact icon={Handshake} title="No deals in this stage" />
            ) : (
              deals.map((d) => (
                <View key={d.id} style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingLeft: s[4], paddingRight: s[2], minHeight: theme.tap.lg }}>
                  <Pressable style={{ flex: 1, paddingVertical: s[2], gap: s[0.5] }} accessibilityRole="button" onPress={() => router.push(recordPath("deal", d.id))}>
                    <Text weight={600}>{d.name}</Text>
                    <Text role="caption" tone="muted" numberOfLines={1}>
                      {[d.amount !== null ? crmMoney(d.amount, d.currency) : "No amount", d.close_date ? dayLabel(d.close_date) : "", d.owner?.name ?? ""].filter(Boolean).join(" · ")}
                    </Text>
                  </Pressable>
                  {d.my_level === "edit" || d.my_level === "manage" ? <IconButton label={`Move ${d.name}`} icon={ArrowRightLeft} onPress={() => setMoving(d)} /> : null}
                </View>
              ))
            )}
          </ListGroup>
          {col?.next_cursor && all.hasNextPage ? (
            <Button variant="ghost" loading={all.isFetchingNextPage} onPress={() => void all.fetchNextPage()}>
              Load more
            </Button>
          ) : null}
        </>
      )}
      {moving ? <StageSheet deal={moving} orgId={orgId} open onClose={() => setMoving(null)} /> : null}
    </Screen>
  );
}
