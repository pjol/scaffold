import {
  CRM_SOURCE_APP,
  CRM_TICKET_PRIORITY_LABEL,
  CRM_TYPE_LABEL,
  crmTotal,
  useCrmPipelines,
  useCrmRecords,
  type CrmFilter,
  type CrmObjectType,
  type CrmRecord,
  type CrmSources,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, SegmentedControl, SkeletonRow, SourceBadge, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Search } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { RefreshControl, View } from "react-native";
import { CrmGate, MobileSourceFilter, TYPE_ICON, dayLabel, money, recordPath, useMobileSource } from "./parts";

type Status = "open" | "closed" | "all";

/** A list of contacts, companies, deals or tickets you can see: search, yours or everyone's (tickets: open, closed or all). */
export function CrmListScreen({ type, mine: initialMine }: { type: CrmObjectType; mine?: boolean }) {
  // Tickets are the app's own only: connected CRMs never serve them.
  return <CrmGate>{({ orgId, sources }) => <List orgId={orgId} type={type} initialMine={!!initialMine} sources={type === "ticket" ? undefined : sources} />}</CrmGate>;
}

function detail(r: CrmRecord): string {
  switch (r.type) {
    case "contact":
      return r.company?.name ?? r.emails[0] ?? r.job_title;
    case "company":
      return r.domain ?? r.industry;
    case "ticket":
      return [r.stage.name, r.priority === "high" || r.priority === "urgent" ? CRM_TICKET_PRIORITY_LABEL[r.priority] : "", dayLabel(r.created_at)].filter(Boolean).join(" · ");
    default:
      return [r.stage.name, r.amount !== null ? money(r.amount, r.currency) : "", r.close_date ? dayLabel(r.close_date) : ""].filter(Boolean).join(" · ");
  }
}

function sortOf(type: CrmObjectType): { sort: string; dir: "asc" | "desc" } {
  if (type === "deal") return { sort: "close_date", dir: "asc" };
  if (type === "ticket") return { sort: "created_at", dir: "desc" };
  return { sort: "name", dir: "asc" };
}

function List({ orgId, type, initialMine, sources }: { orgId: string; type: CrmObjectType; initialMine: boolean; sources: CrmSources | undefined }) {
  const theme = useTheme();
  const src = useMobileSource(sources, "list");
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [who, setWho] = useState<"all" | "mine">(initialMine ? "mine" : "all");
  const [status, setStatus] = useState<Status>("open");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 300);
    return () => clearTimeout(t);
  }, [q]);
  // Tickets: open or closed means every ticket pipeline's statuses with that outcome.
  const isTicket = type === "ticket";
  const byStatus = isTicket && status !== "all";
  const pipelines = useCrmPipelines(isTicket ? orgId : null);
  const statusIds = useMemo(() => {
    if (!byStatus) return [];
    return (pipelines.data ?? [])
      .filter((p) => p.object_type === "ticket")
      .flatMap((p) => p.stages.filter((st) => (status === "open" ? st.outcome === "open" : st.outcome !== "open")).map((st) => st.id));
  }, [byStatus, status, pipelines.data]);
  const filters: CrmFilter[] = [];
  if (who === "mine") filters.push({ key: "owner", op: "is_any", value: ["me"] });
  if (statusIds.length > 0) filters.push({ key: "stage", op: "is_any", value: statusIds });
  const list = useCrmRecords(
    orgId,
    type,
    {
      q: debounced,
      filters,
      ...sortOf(type),
      limit: 30,
      ...(src.value !== CRM_SOURCE_APP ? { source: src.value } : {}),
    },
    // Wait for the ticket statuses so the list doesn't flash every ticket first.
    { enabled: !byStatus || pipelines.isSuccess },
  );
  const total = crmTotal(list.data);
  const Icon = TYPE_ICON[type];
  const label = CRM_TYPE_LABEL[type];
  const narrowed = !!debounced || who === "mine";
  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[4]}
      refreshControl={
        <RefreshControl
          refreshing={list.isRefetching}
          onRefresh={() => {
            if (isTicket) void pipelines.refetch();
            void list.refetch();
          }}
        />
      }
    >
      <Stack.Screen options={{ title: label.Many }} />
      <View style={{ gap: theme.space[3] }}>
        <MobileSourceFilter state={src} />
        <Input value={q} onChangeText={setQ} placeholder={`Search ${label.many}`} leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel={`Search ${label.many}`} />
        {isTicket ? (
          <SegmentedControl<Status>
            accessibilityLabel="Status"
            value={status}
            onValueChange={setStatus}
            options={[
              { value: "open", label: "Open" },
              { value: "closed", label: "Closed" },
              { value: "all", label: "All" },
            ]}
          />
        ) : null}
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
            {total} {total === 1 ? label.one : label.many}
          </Text>
        ) : null}
      </View>
      <ListGroup>
        {byStatus && pipelines.isError ? (
          <ErrorState compact title="Couldn't load ticket statuses" info={pipelines.error.message} onRetry={() => void pipelines.refetch()} retrying={pipelines.isRefetching} />
        ) : list.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : list.isError ? (
          <ErrorState compact title={`Couldn't load ${label.many}`} info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
        ) : list.data.items.length === 0 ? (
          <EmptyState compact icon={Icon} title={narrowed ? `No ${label.many} match` : byStatus ? `No ${status} ${label.many}` : `No ${label.many} yet`} />
        ) : (
          list.data.items.map((r) => (
            <ListRow
              key={r.id}
              title={r.title}
              leading={<Icon size={20} color={theme.colors.textMuted} />}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                  <SourceBadge source={r.source} iconOnly />
                  <Text role="caption" tone={r.type === "ticket" && r.priority === "urgent" && r.stage.outcome === "open" ? "danger" : "muted"} numberOfLines={1} style={{ maxWidth: 160 }}>
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
