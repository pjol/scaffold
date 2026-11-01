"use client";

import {
  SUPPORT_CATEGORIES,
  SUPPORT_CATEGORY_LABEL,
  SUPPORT_STATUS_LABEL,
  useAdminTickets,
  useNow,
  useSupportAssignees,
  useSupportStats,
  type SupportCategory,
  type SupportStatus,
} from "@scaffoldapp/shared";
import { AnimatedNumber, Card, EmptyState, ErrorState, InfoTip, Select, Skeleton, SkeletonGroup, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { Inbox, SearchX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { TicketStatusBadge } from "./components";
import { supportPaths } from "./registry";

type StatusFilter = "any" | SupportStatus;
type CategoryFilter = "any" | SupportCategory;
type SortOrder = "newest" | "oldest";

const STATUS_OPTIONS = [{ value: "any", label: "Any status" }, ...(Object.keys(SUPPORT_STATUS_LABEL) as SupportStatus[]).map((s) => ({ value: s, label: SUPPORT_STATUS_LABEL[s] }))];
const CATEGORY_OPTIONS = [{ value: "any", label: "Any category" }, ...SUPPORT_CATEGORIES.map((c) => ({ value: c, label: SUPPORT_CATEGORY_LABEL[c] }))];
const SORT_OPTIONS = [
  { value: "oldest", label: "Oldest first" },
  { value: "newest", label: "Newest first" },
];

function Stat({ label, value, info }: { label: string; value: number; info: string }) {
  return (
    <Card padding="compact" className="flex h-full flex-col gap-1">
      <span className="flex items-center gap-2 type-label text-fg-muted">
        {label}
        <InfoTip content={info} />
      </span>
      <AnimatedNumber value={value} className="type-title text-fg" />
    </Card>
  );
}

function QueueStats() {
  const stats = useSupportStats();
  const now = useNow(60_000);
  if (stats.isPending) {
    return (
      <SkeletonGroup label="Loading counts" className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
        <Skeleton className="h-20" />
      </SkeletonGroup>
    );
  }
  if (stats.isError) {
    return (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load the counts" info={stats.error.message} onRetry={() => void stats.refetch()} retrying={stats.isFetching} />
      </Card>
    );
  }
  const s = stats.data;
  return (
    <Stagger as="ul" className="grid grid-cols-2 gap-4 lg:grid-cols-4" aria-label="Queue">
      <StaggerItem as="li">
        <Stat label="Open" value={s.open} info={s.oldest_open_at ? `Waiting on staff. The longest has waited since ${formatRelative(s.oldest_open_at, now)}.` : "Waiting on staff."} />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Unassigned" value={s.unassigned_open} info="Open requests nobody has picked up." />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Yours" value={s.mine_open} info="Open requests assigned to you." />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Answered" value={s.answered} info="Waiting on the person who asked." />
      </StaggerItem>
    </Stagger>
  );
}

/** Admin → Support: the request queue. Filters live in the URL; open requests oldest first by default. */
export function AdminQueueScreen() {
  const router = useRouter();
  const now = useNow(60_000);
  const [q, setQ] = useUrlState("q", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "open");
  const [category, setCategory] = useUrlState<CategoryFilter>("category", "any");
  const [assignee, setAssignee] = useUrlState("assignee", "any");
  const [sort, setSort] = useUrlState<SortOrder>("sort", "oldest");
  const staff = useSupportAssignees();
  const tickets = useAdminTickets({
    q: q || undefined,
    status: status === "any" ? undefined : status,
    category: category === "any" ? undefined : category,
    assignee: assignee === "any" ? undefined : assignee,
    sort,
    limit: 50,
  });
  const rows = tickets.data?.items ?? [];
  const filtered = !!q || category !== "any" || assignee !== "any";
  const assigneeOptions = [
    { value: "any", label: "Anyone" },
    { value: "me", label: "Assigned to me" },
    { value: "none", label: "Unassigned" },
    ...(staff.data?.items ?? []).map((s) => ({ value: s.id, label: s.display_name || s.email })),
  ];

  return (
    <Screen title="Support">
      <QueueStats />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <SearchInput value={q} onSearch={setQ} placeholder="Subject, email or #number" label="Search requests" className="sm:max-w-sm" />
          <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
            <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="sm:w-40" />
            <Select aria-label="Category" options={CATEGORY_OPTIONS} value={category} onValueChange={(v) => setCategory(v as CategoryFilter)} className="sm:w-48" />
            <Select aria-label="Assignee" options={assigneeOptions} value={assignee} onValueChange={setAssignee} className="sm:w-44" />
            <Select aria-label="Order" options={SORT_OPTIONS} value={sort} onValueChange={(v) => setSort(v as SortOrder)} className="sm:w-40" />
          </div>
        </div>
        <Card padding="none">
          {tickets.isPending ? (
            <TableSkeleton rows={8} cols={4} />
          ) : tickets.isError ? (
            <ErrorState compact title="Couldn't load requests" info={tickets.error.message} onRetry={() => void tickets.refetch()} retrying={tickets.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <Inbox />} title={filtered ? "No matching requests" : status === "open" ? "No open requests" : "No requests"} />
          ) : (
            <>
              <Table label="Support requests">
                <THead>
                  <TH className="hidden w-16 sm:table-cell">#</TH>
                  <TH>Subject</TH>
                  <TH className="hidden md:table-cell">From</TH>
                  <TH>Status</TH>
                  <TH className="hidden xl:table-cell">Assignee</TH>
                  <TH className="hidden sm:table-cell" align="right">
                    Last message
                  </TH>
                </THead>
                <tbody>
                  {rows.map((t) => (
                    <TR key={t.id} onActivate={() => router.push(supportPaths.adminTicket(t.id))}>
                      <TD className="hidden text-fg-muted tabular-nums sm:table-cell">{t.number}</TD>
                      <TD className="w-full max-w-0">
                        <Link href={supportPaths.adminTicket(t.id)} className="block truncate font-semibold text-fg underline-offset-2 hover:underline">
                          {t.subject}
                        </Link>
                      </TD>
                      <TD className="hidden max-w-48 truncate text-fg-muted md:table-cell">{t.user.status === "deleted" ? "Deleted user" : t.user.email}</TD>
                      <TD className="whitespace-nowrap">
                        <TicketStatusBadge status={t.status} />
                      </TD>
                      <TD className="hidden max-w-40 truncate text-fg-muted xl:table-cell">{t.assignee ? t.assignee.display_name || t.assignee.email : "—"}</TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell" align="right">
                        <time dateTime={t.last_message_at} title={formatDateTime(t.last_message_at)}>
                          {formatRelative(t.last_message_at, now)}
                        </time>
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={tickets.hasNextPage} isFetchingNextPage={tickets.isFetchingNextPage} fetchNextPage={tickets.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </Screen>
  );
}
