"use client";

import {
  errorMessage,
  useAgentsBulk,
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
  IconButton,
  ListRow,
  Skeleton,
  SkeletonGroup,
  Stagger,
  StaggerItem,
  Tab,
  Tabs,
  TabsList,
  toast,
} from "@scaffoldapp/ui-web";
import { Inbox, Settings2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { STATUS_LABEL, STATUS_TONE, agentIcon } from "./labels";
import { SuggestionCard } from "./SuggestionCard";

type TabId = "pending" | "accepted" | "rejected" | "failed";

const TABS: { id: TabId; label: string; status: AgentsSuggestionFilter[] }[] = [
  { id: "pending", label: "Waiting", status: ["pending"] },
  { id: "accepted", label: "Accepted", status: ["accepted"] },
  { id: "rejected", label: "Rejected", status: ["rejected"] },
  { id: "failed", label: "Failed", status: ["failed", "expired"] },
];

const EMPTY: Record<TabId, string> = {
  pending: "Nothing waiting",
  accepted: "No accepted suggestions",
  rejected: "No rejected suggestions",
  failed: "Nothing failed or expired",
};

/** /agents: changes AI agents suggested, waiting ones grouped by agent with Accept all. */
export function AgentsSuggestionsScreen() {
  const router = useRouter();
  const [tab, setTab] = useUrlState<TabId>("status", "pending");
  const current = TABS.find((t) => t.id === tab) ?? TABS[0]!;
  const summary = useAgentsSummary();
  const list = useAgentsSuggestions({ status: current.status, limit: 30 });
  const rows = list.data?.items ?? [];

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <PageHeader
        title="Agent suggestions"
        info="Changes AI agents proposed while their access was set to Suggest. Accepting runs the change as you, with the access you have now."
        action={
          <>
            <IconButton label="Agent access" icon={<Settings2 />} variant="secondary" className="sm:hidden" onClick={() => router.push("/settings/agents")} />
            <Button asChild variant="secondary" size="auto" leftIcon={<Settings2 />} className="hidden sm:inline-flex">
              <Link href="/settings/agents">Agent access</Link>
            </Button>
          </>
        }
      />
      {summary.data?.paused && <Banner tone="warning">All AI agent access is off. Agents can't read or suggest anything until it's back on.</Banner>}
      <Tabs value={current.id} onValueChange={(v) => setTab(v as TabId)}>
        <TabsList aria-label="Suggestions">
          {TABS.map((t) => (
            <Tab key={t.id} value={t.id} {...(t.id === "pending" && summary.data?.pending ? { count: summary.data.pending } : {})}>
              {t.label}
            </Tab>
          ))}
        </TabsList>
      </Tabs>
      {list.isPending ? (
        <SkeletonGroup label="Loading suggestions" className="flex flex-col gap-4">
          <Skeleton className="h-40 w-full rounded-lg" />
          <Skeleton className="h-40 w-full rounded-lg" />
        </SkeletonGroup>
      ) : list.isError ? (
        <Card>
          <ErrorState compact title="Couldn't load suggestions" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        </Card>
      ) : rows.length === 0 ? (
        <Card>
          <EmptyState compact icon={<Inbox />} title={EMPTY[current.id]} />
        </Card>
      ) : current.id === "pending" ? (
        <PendingGroups rows={rows} />
      ) : (
        <Card padding="none" className="p-1">
          <Stagger as="ul" className="flex flex-col">
            {rows.map((s) => {
              const Icon = agentIcon(s.agent.via);
              return (
                <StaggerItem as="li" key={s.id}>
                  <ListRow
                    asChild
                    title={s.summary}
                    leading={<Icon className="size-5 text-fg-muted" aria-hidden />}
                    trailing={
                      <span className="flex items-center gap-2">
                        <span className="hidden type-caption sm:inline">{formatRelative(s.decided_at ?? s.created_at)}</span>
                        <Badge tone={STATUS_TONE[s.status]}>{STATUS_LABEL[s.status]}</Badge>
                      </span>
                    }
                  >
                    <Link href={`/agents/suggestions/${s.id}`} />
                  </ListRow>
                </StaggerItem>
              );
            })}
          </Stagger>
        </Card>
      )}
      {list.isPending || list.isError ? null : (
        <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
      )}
    </div>
  );
}

function PendingGroups({ rows }: { rows: AgentsSuggestion[] }) {
  const groups = useMemo(() => {
    const map = new Map<string, { agent: AgentsAgentRef; items: AgentsSuggestion[] }>();
    for (const s of rows) {
      const key = `${s.agent.via}|${s.agent.client_key}`;
      const g = map.get(key) ?? { agent: s.agent, items: [] };
      g.items.push(s);
      map.set(key, g);
    }
    return [...map.values()];
  }, [rows]);
  return (
    <div className="flex flex-col gap-section">
      {groups.map((g) => (
        <AgentGroup key={`${g.agent.via}|${g.agent.client_key}`} agent={g.agent} items={g.items} />
      ))}
    </div>
  );
}

function AgentGroup({ agent, items }: { agent: AgentsAgentRef; items: AgentsSuggestion[] }) {
  const bulk = useAgentsBulk();
  const [confirm, setConfirm] = useState<"accept" | "reject" | null>(null);
  const Icon = agentIcon(agent.via);
  const run = async (action: "accept" | "reject") => {
    try {
      const r = await bulk.mutateAsync({ action, via: agent.via, client_key: agent.client_key });
      const done = action === "accept" ? `${r.accepted} accepted` : `${r.rejected} rejected`;
      const extra = [r.failed ? `${r.failed} failed` : "", r.skipped ? `${r.skipped} changed since and still waiting` : ""].filter(Boolean).join(", ");
      (r.failed || r.skipped ? toast.warning : toast.success)(extra ? `${done}, ${extra}` : done);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't finish. Try again."));
      throw e;
    }
  };
  return (
    <section className="flex flex-col gap-3" aria-label={`Suggestions from ${agent.client}`}>
      <div className="flex min-h-control flex-wrap items-center gap-3">
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
          <Icon className="size-5 shrink-0 text-fg-muted" aria-hidden />
          <span className="truncate">{agent.client}</span>
          <Badge tone="warning">{items.length}</Badge>
        </h2>
        {items.length > 1 && (
          <span className="flex gap-2">
            <Button variant="ghost" size="sm" disabled={bulk.isPending} onClick={() => setConfirm("reject")}>
              Reject all
            </Button>
            <Button variant="secondary" size="sm" disabled={bulk.isPending} loading={bulk.isPending} onClick={() => setConfirm("accept")}>
              Accept all
            </Button>
          </span>
        )}
      </div>
      <Stagger className="flex flex-col gap-4">
        {items.map((s) => (
          <StaggerItem key={s.id}>
            <SuggestionCard s={s} />
          </StaggerItem>
        ))}
      </Stagger>
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={confirm === "accept" ? `Accept everything from ${agent.client}?` : `Reject everything from ${agent.client}?`}
        confirmLabel={confirm === "accept" ? "Accept all" : "Reject all"}
        tone={confirm === "reject" ? "danger" : "primary"}
        onConfirm={() => run(confirm ?? "reject")}
      >
        {confirm === "accept"
          ? "Each change runs as you, oldest first. Anything that changed since it was suggested stays here for you to review."
          : "The changes won't be made. Your agent can check what happened to each one."}
      </ConfirmDialog>
    </section>
  );
}
