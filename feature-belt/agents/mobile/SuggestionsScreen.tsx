import {
  errorMessage,
  hasErrorCode,
  useAgentsAccept,
  useAgentsBulk,
  useAgentsReject,
  useAgentsSuggestions,
  useAgentsSummary,
  type AgentsAgentRef,
  type AgentsSuggestion,
  type AgentsSuggestionFilter,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  haptic,
  InfoTip,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  SkeletonRow,
  Text,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams, type Href } from "expo-router";
import { Inbox } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";
import { agentIcon, relativeTime, STATUS_LABEL, STATUS_TONE } from "./labels";
import { SuggestionChange } from "./SuggestionDiff";

type TabId = "pending" | "accepted" | "rejected" | "failed";
const TABS: { id: TabId; label: string; status: AgentsSuggestionFilter[]; empty: string }[] = [
  { id: "pending", label: "Waiting", status: ["pending"], empty: "Nothing waiting" },
  { id: "accepted", label: "Accepted", status: ["accepted"], empty: "No accepted suggestions" },
  { id: "rejected", label: "Rejected", status: ["rejected"], empty: "No rejected suggestions" },
  { id: "failed", label: "Failed", status: ["failed", "expired"], empty: "Nothing failed or expired" },
];

const open = (id: string) => router.push(`/agents/suggestions/${id}` as Href);

/** Accept / Reject one waiting suggestion; a changed target opens the full view. */
export function useSuggestionDecision() {
  const accept = useAgentsAccept();
  const reject = useAgentsReject();
  return {
    busy: accept.isPending || reject.isPending,
    accepting: accept.isPending,
    rejecting: reject.isPending,
    accept: (s: AgentsSuggestion, force = false, onChanged?: (d: { before: string; after: string }) => void) =>
      accept.mutate(
        { id: s.id, force },
        {
          onSuccess: (next) => {
            if (next.status === "accepted") {
              haptic.success();
              toast.success("Done");
            } else toast.error(next.error?.message ?? "Couldn't apply this change.");
          },
          onError: (e) => {
            if (hasErrorCode(e, "target_changed")) {
              const d = (e.details ?? {}) as { before?: string; after?: string };
              if (onChanged) onChanged({ before: d.before ?? "", after: d.after ?? "" });
              else {
                toast.warning("This changed since it was suggested. Review it first.");
                open(s.id);
              }
            } else toast.error(errorMessage(e, "Couldn't accept. Try again."));
          },
        },
      ),
    reject: (s: AgentsSuggestion) => reject.mutate(s.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't reject. Try again.")) }),
  };
}

function PendingCard({ s }: { s: AgentsSuggestion }) {
  const theme = useTheme();
  const d = useSuggestionDecision();
  return (
    <Card style={{ gap: theme.space[3] }}>
      <Pressable onPress={() => open(s.id)} accessibilityRole="link" style={{ flexDirection: "row", gap: theme.space[2], alignItems: "flex-start" }}>
        <Text weight={600} style={{ flex: 1 }}>
          {s.summary}
        </Text>
        <Text role="caption" tone="muted">
          {relativeTime(s.created_at)}
        </Text>
      </Pressable>
      <SuggestionChange s={s} />
      <View style={{ flexDirection: "row", gap: theme.space[2] }}>
        <Button variant="ghost" size="md" style={{ flex: 1 }} disabled={d.busy} loading={d.rejecting} onPress={() => d.reject(s)}>
          Reject
        </Button>
        <Button size="md" style={{ flex: 1 }} disabled={d.busy} loading={d.accepting} onPress={() => d.accept(s)}>
          Accept
        </Button>
      </View>
    </Card>
  );
}

function AgentGroup({ agent, items }: { agent: AgentsAgentRef; items: AgentsSuggestion[] }) {
  const theme = useTheme();
  const bulk = useAgentsBulk();
  const [confirm, setConfirm] = useState<"accept" | "reject" | null>(null);
  const Icon = agentIcon(agent.via);
  return (
    <View style={{ gap: theme.space[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: 40 }}>
        <Icon size={20} color={theme.colors.textMuted} />
        <Text role="titleSm" numberOfLines={1} style={{ flexShrink: 1 }}>
          {agent.client}
        </Text>
        <Badge tone="warning">{String(items.length)}</Badge>
        <InfoTip content="Accept all runs each change as you, oldest first. Anything that changed since it was suggested stays here." />
      </View>
      {items.length > 1 ? (
        <View style={{ flexDirection: "row", gap: theme.space[2] }}>
          <Button variant="ghost" size="md" style={{ flex: 1 }} disabled={bulk.isPending} onPress={() => setConfirm("reject")}>
            Reject all
          </Button>
          <Button variant="secondary" size="md" style={{ flex: 1 }} loading={bulk.isPending} onPress={() => setConfirm("accept")}>
            Accept all
          </Button>
        </View>
      ) : null}
      {items.map((s) => (
        <PendingCard key={s.id} s={s} />
      ))}
      <ConfirmDialog
        open={confirm !== null}
        onClose={() => setConfirm(null)}
        title={confirm === "accept" ? `Accept everything from ${agent.client}?` : `Reject everything from ${agent.client}?`}
        confirmLabel={confirm === "accept" ? "Accept all" : "Reject all"}
        tone={confirm === "reject" ? "danger" : "primary"}
        onConfirm={async () => {
          const action = confirm ?? "reject";
          try {
            const r = await bulk.mutateAsync({ action, via: agent.via, client_key: agent.client_key });
            const done = action === "accept" ? `${r.accepted} accepted` : `${r.rejected} rejected`;
            const extra = [r.failed ? `${r.failed} failed` : "", r.skipped ? `${r.skipped} changed since and still waiting` : ""].filter(Boolean).join(", ");
            (r.failed || r.skipped ? toast.warning : toast.success)(extra ? `${done}, ${extra}` : done);
          } catch (e) {
            toast.error(errorMessage(e, "Couldn't finish. Try again."));
            throw e;
          }
        }}
      >
        {confirm === "accept" ? "Each change runs as you. Anything that changed since it was suggested stays here for you to review." : "The changes won't be made."}
      </ConfirmDialog>
    </View>
  );
}

/** Suggestions: waiting ones grouped by agent with Accept all; accepted, rejected and failed below the switch. */
export function AgentsSuggestionsScreen() {
  const theme = useTheme();
  const s = theme.space;
  const params = useLocalSearchParams<{ status?: string }>();
  const [tab, setTab] = useState<TabId>((TABS.find((t) => t.id === params.status)?.id ?? "pending") as TabId);
  const current = TABS.find((t) => t.id === tab) ?? TABS[0]!;
  const summary = useAgentsSummary();
  const list = useAgentsSuggestions({ status: current.status, limit: 30 });
  const rows = list.data?.items ?? [];
  const groups = useMemo(() => {
    const map = new Map<string, { agent: AgentsAgentRef; items: AgentsSuggestion[] }>();
    for (const x of rows) {
      const key = `${x.agent.via}|${x.agent.client_key}`;
      const g = map.get(key) ?? { agent: x.agent, items: [] };
      g.items.push(x);
      map.set(key, g);
    }
    return [...map.values()];
  }, [rows]);

  return (
    <Screen edges={["bottom"]} gap={s[6]} refreshControl={<RefreshControl refreshing={list.isRefetching && !list.isFetchingNextPage} onRefresh={() => void list.refetch()} />}>
      <Stack.Screen options={{ title: "Suggestions" }} />
      {summary.data?.paused ? <Banner tone="warning" title="All AI agent access is off. Agents can't read or suggest anything until it's back on." /> : null}
      <SegmentedControl<TabId> accessibilityLabel="Suggestions" value={tab} onValueChange={setTab} options={TABS.map((t) => ({ value: t.id, label: t.label }))} />
      {list.isPending ? (
        <ListGroup>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </ListGroup>
      ) : list.isError ? (
        <ErrorState title="Couldn't load suggestions" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Inbox} title={current.empty} />
      ) : current.id === "pending" ? (
        <View style={{ gap: s[8] }}>
          {groups.map((g) => (
            <AgentGroup key={`${g.agent.via}|${g.agent.client_key}`} agent={g.agent} items={g.items} />
          ))}
        </View>
      ) : (
        <ListGroup>
          {rows.map((x) => {
            const Icon = agentIcon(x.agent.via);
            return (
              <ListRow
                key={x.id}
                title={x.summary}
                leading={<Icon size={20} color={theme.colors.textMuted} />}
                trailing={<Badge tone={STATUS_TONE[x.status]}>{STATUS_LABEL[x.status]}</Badge>}
                onPress={() => open(x.id)}
              />
            );
          })}
        </ListGroup>
      )}
      {list.hasNextPage ? (
        <Button variant="ghost" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()}>
          Show more
        </Button>
      ) : null}
    </Screen>
  );
}
