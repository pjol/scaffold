import { crmMoney, useCrmDashboards, useCrmReportDashboard, type CrmReportResult } from "@scaffoldapp/shared";
import { Card, EmptyState, ErrorState, Headline, ListGroup, ListRow, Screen, Skeleton, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { BarChart3, LayoutDashboard } from "lucide-react-native";
import { RefreshControl, View } from "react-native";
import { CrmGate } from "./parts";

function valueText(v: number, r: CrmReportResult): string {
  if (r.value_type === "money") return crmMoney(Math.round(v), r.currency ?? "USD", { short: true });
  return v.toLocaleString(undefined, { maximumFractionDigits: r.value_type === "count" ? 0 : 2 });
}

/** Dashboards you can open. */
export function CrmReportsScreen() {
  return <CrmGate>{({ orgId }) => <Dashboards orgId={orgId} />}</CrmGate>;
}

function Dashboards({ orgId }: { orgId: string }) {
  const theme = useTheme();
  const list = useCrmDashboards(orgId);
  return (
    <Screen edges={["bottom"]} gap={theme.space[4]} refreshControl={<RefreshControl refreshing={list.isRefetching} onRefresh={() => void list.refetch()} />}>
      <Stack.Screen options={{ title: "Reports" }} />
      {list.isPending ? (
        <SkeletonRow />
      ) : list.isError ? (
        <ErrorState title="Couldn't load dashboards" info={list.error.message} onRetry={() => void list.refetch()} />
      ) : list.data.items.length === 0 ? (
        <EmptyState icon={LayoutDashboard} title="No dashboards yet" info="Make dashboards on the web; they show here." />
      ) : (
        <ListGroup>
          {list.data.items.map((d) => (
            <ListRow key={d.id} title={d.name} leading={<LayoutDashboard size={20} color={theme.colors.textMuted} />} onPress={() => router.push(`/crm/reports/${d.id}`)} />
          ))}
        </ListGroup>
      )}
    </Screen>
  );
}

/** A dashboard's reports as lists with proportional bars, over the records you can see. */
export function CrmReportDashboardScreen({ id }: { id: string }) {
  return <CrmGate>{() => <DashboardView id={id} />}</CrmGate>;
}

function DashboardView({ id }: { id: string }) {
  const theme = useTheme();
  const dash = useCrmReportDashboard(id);
  if (dash.isPending) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Skeleton height={theme.tap.lg * 3} />
        <Skeleton height={theme.tap.lg * 3} />
      </Screen>
    );
  }
  if (dash.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <ErrorState title="Couldn't load the dashboard" info={dash.error.message} onRetry={() => void dash.refetch()} />
      </Screen>
    );
  }
  const reports = dash.data.reports ?? [];
  return (
    <Screen edges={["bottom"]} gap={theme.space[4]} refreshControl={<RefreshControl refreshing={dash.isRefetching} onRefresh={() => void dash.refetch()} />}>
      <Stack.Screen options={{ title: dash.data.name }} />
      <Headline>{dash.data.name}</Headline>
      {reports.length === 0 ? <EmptyState icon={BarChart3} title="No reports yet" /> : null}
      {reports.map((r) => {
        const res = r.result;
        const max = res ? Math.max(...res.rows.map((x) => Math.abs(x.value)), 0) : 0;
        return (
          <Card key={r.id}>
            <View style={{ gap: theme.space[3] }}>
              <View style={{ flexDirection: "row", alignItems: "baseline", justifyContent: "space-between", gap: theme.space[2] }}>
                <Text role="label" weight={600} style={{ flex: 1 }}>
                  {r.name}
                </Text>
                {res ? (
                  <Text role="label" weight={600} tabular>
                    {valueText(res.total, res)}
                  </Text>
                ) : null}
              </View>
              {r.error ? <Text tone="danger">{r.error}</Text> : null}
              {res && res.spec.group_by
                ? res.rows.slice(0, 12).map((row) => (
                    <View key={row.key} style={{ gap: theme.space[1] }}>
                      <View style={{ flexDirection: "row", justifyContent: "space-between", gap: theme.space[2] }}>
                        <Text role="caption" numberOfLines={1} style={{ flex: 1 }}>
                          {row.label}
                        </Text>
                        <Text role="caption" tone="muted" tabular>
                          {valueText(row.value, res)}
                        </Text>
                      </View>
                      <View style={{ height: theme.space[2], borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, overflow: "hidden" }}>
                        <View style={{ width: `${max > 0 ? Math.max(2, (Math.abs(row.value) / max) * 100) : 0}%`, height: "100%", backgroundColor: theme.colors.primary }} />
                      </View>
                    </View>
                  ))
                : null}
              {res && res.rows.length > 12 ? (
                <Text role="caption" tone="muted">
                  {`${res.rows.length - 12} more on the web`}
                </Text>
              ) : null}
            </View>
          </Card>
        );
      })}
    </Screen>
  );
}
