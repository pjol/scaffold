import { CRM_TYPE_LABEL, useCrmSearch, useCrmTaskSummary, type CrmObjectType } from "@scaffoldapp/shared";
import { Badge, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, SegmentedControl, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Building2, CheckSquare, Handshake, KanbanSquare, Search, SearchX, UserRound } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { CrmGate, TYPE_ICON, recordPath } from "./parts";

type Scope = "all" | CrmObjectType;

/** CRM home on mobile: search every record you can see, then tasks, the board and the lists. */
export function CrmHomeScreen() {
  return <CrmGate>{({ orgId, orgName }) => <Home orgId={orgId} orgName={orgName} />}</CrmGate>;
}

function Home({ orgId, orgName }: { orgId: string; orgName: string }) {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const [scope, setScope] = useState<Scope>("all");
  const tasks = useCrmTaskSummary(orgId);
  const search = useCrmSearch(orgId, q, scope === "all" ? undefined : [scope], { limit: 20, enabled: q.trim().length > 0 });
  const due = tasks.data ? tasks.data.overdue + tasks.data.today : 0;
  return (
    <Screen edges={["bottom"]} gap={theme.space[6]} keyboard>
      <Stack.Screen options={{ title: `CRM · ${orgName}` }} />
      <View style={{ gap: theme.space[3] }}>
        <Input value={q} onChangeText={setQ} placeholder="Search name, email, phone or domain" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} returnKeyType="search" accessibilityLabel="Search the CRM" />
        {q.trim() ? (
          <SegmentedControl<Scope>
            accessibilityLabel="Search in"
            value={scope}
            onValueChange={setScope}
            options={[
              { value: "all", label: "All" },
              { value: "contact", label: "People" },
              { value: "company", label: "Companies" },
              { value: "deal", label: "Deals" },
            ]}
          />
        ) : null}
      </View>
      {q.trim() ? (
        <ListGroup>
          {search.isPending ? (
            <View accessibilityLabel="Searching" accessibilityRole="progressbar">
              <SkeletonRow />
              <SkeletonRow />
            </View>
          ) : search.isError ? (
            <ErrorState compact title="Couldn't search" info={search.error.message} onRetry={() => void search.refetch()} />
          ) : search.data.length === 0 ? (
            <EmptyState compact icon={SearchX} title={`Nothing matches “${q.trim()}”`} />
          ) : (
            search.data.map((h) => {
              const Icon = TYPE_ICON[h.type];
              return (
                <ListRow
                  key={`${h.type}:${h.id}`}
                  title={h.title}
                  leading={<Icon size={20} color={theme.colors.textMuted} />}
                  trailing={
                    <Text role="caption" tone="muted" numberOfLines={1} style={{ maxWidth: 140 }}>
                      {h.detail || CRM_TYPE_LABEL[h.type].One}
                    </Text>
                  }
                  onPress={() => router.push(recordPath(h.type, h.id))}
                />
              );
            })
          )}
        </ListGroup>
      ) : (
        <>
          <ListGroup>
            <ListRow
              title="My tasks"
              leading={<CheckSquare size={20} color={theme.colors.textMuted} />}
              trailing={due > 0 ? <Badge tone={tasks.data?.overdue ? "danger" : "primary"}>{String(due)}</Badge> : undefined}
              onPress={() => router.push("/crm/tasks")}
            />
            <ListRow title="Deals board" leading={<KanbanSquare size={20} color={theme.colors.textMuted} />} onPress={() => router.push("/crm/deals/board")} />
          </ListGroup>
          <ListGroup title="Records">
            <ListRow title="Contacts" leading={<UserRound size={20} color={theme.colors.textMuted} />} onPress={() => router.push("/crm/contacts")} />
            <ListRow title="Companies" leading={<Building2 size={20} color={theme.colors.textMuted} />} onPress={() => router.push("/crm/companies")} />
            <ListRow title="Deals" leading={<Handshake size={20} color={theme.colors.textMuted} />} onPress={() => router.push("/crm/deals")} />
          </ListGroup>
        </>
      )}
    </Screen>
  );
}
