"use client";

import {
  crmMoney,
  crmMoneyTotals,
  errorMessage,
  useCrmBoard,
  useCrmMoveStage,
  useCrmPipelines,
  useCrmRecords,
  useTopic,
  topic,
  type CrmBoardColumn,
  type CrmDeal,
  type CrmFilter,
  type CrmStage,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, IconButton, Input, Menu, MenuItem, MenuLabel, Select, Skeleton, SkeletonGroup, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowRightLeft, Handshake, List, Plus } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type DragEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { CreateRecordDialog } from "./CreateRecordDialog";
import { dayLabel, recordHref } from "./parts";

/** /crm/deals/board: a pipeline's stages as columns; drag a card (or use its menu) to move it. */
export function CrmBoardScreen() {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame
      actions={
        <>
          <Button asChild variant="secondary" size="auto" leftIcon={<List />}>
            <Link href="/crm/deals">Table</Link>
          </Button>
          <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
            New deal
          </Button>
        </>
      }
    >
      <Board />
      <CreateRecordDialog type="deal" open={creating} onOpenChange={setCreating} />
    </CrmFrame>
  );
}

type OwnerFilter = "all" | "me";
type CloseFilter = "any" | "month" | "quarter" | "overdue";

function closeRange(c: CloseFilter): CrmFilter[] {
  const now = new Date();
  const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  if (c === "month") {
    const start = new Date(now.getFullYear(), now.getMonth(), 0);
    const end = new Date(now.getFullYear(), now.getMonth() + 1, 1);
    return [
      { key: "close_date", op: "after", value: iso(start) },
      { key: "close_date", op: "before", value: iso(end) },
    ];
  }
  if (c === "quarter") {
    const q = Math.floor(now.getMonth() / 3);
    const start = new Date(now.getFullYear(), q * 3, 0);
    const end = new Date(now.getFullYear(), q * 3 + 3, 1);
    return [
      { key: "close_date", op: "after", value: iso(start) },
      { key: "close_date", op: "before", value: iso(end) },
    ];
  }
  if (c === "overdue") return [{ key: "close_date", op: "before", value: iso(now) }];
  return [];
}

function Board() {
  const { orgId, settings } = useCrm();
  const pipelines = useCrmPipelines(orgId);
  const [pipelineId, setPipelineId] = useUrlState("pipeline", "");
  const [owner, setOwner] = useUrlState<OwnerFilter>("owner", "all");
  const [close, setClose] = useUrlState<CloseFilter>("close", "any");
  const [q, setQ] = useState("");
  const filters = useMemo<CrmFilter[]>(() => [...(owner === "me" ? [{ key: "owner", op: "is_any" as const, value: ["me"] }] : []), ...closeRange(close)], [owner, close]);
  const params = useMemo(() => ({ filters, ...(q.trim() ? { q: q.trim() } : {}) }), [filters, q]);
  const board = useCrmBoard(orgId, pipelineId || undefined, params);
  useTopic(board.data ? topic("crm", board.data.pipeline.id) : null);
  const move = useCrmMoveStage(orgId);
  const [dragging, setDragging] = useState<CrmDeal | null>(null);
  const [over, setOver] = useState<string | null>(null);

  const doMove = (deal: CrmDeal, stage: CrmStage) => {
    if (deal.stage.id === stage.id) return;
    if (deal.my_level !== "edit" && deal.my_level !== "manage") {
      toast.error("You can't edit this deal.");
      return;
    }
    move.mutate(
      { deal, stageId: stage.id },
      {
        onSuccess: () => toast.success(`${deal.name} → ${stage.name}`),
        onError: (e) => toast.error(errorMessage(e, "Couldn't move the deal.")),
      },
    );
  };

  return (
    <section className="flex flex-col gap-4" aria-label="Deals board">
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        {pipelines.data && pipelines.data.length > 1 ? (
          <Select
            aria-label="Pipeline"
            className="md:w-56"
            value={pipelineId || pipelines.data[0]?.id}
            onValueChange={(v) => setPipelineId(v === pipelines.data?.[0]?.id ? "" : v)}
            options={pipelines.data.map((p) => ({ value: p.id, label: p.name }))}
          />
        ) : null}
        <Input aria-label="Search deals" placeholder="Search deals" value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="md:w-56" />
        <Select
          aria-label="Owner"
          className="md:w-40"
          value={owner}
          onValueChange={(v) => setOwner(v as OwnerFilter)}
          options={[
            { value: "all", label: "Everyone's" },
            { value: "me", label: "My deals" },
          ]}
        />
        <Select
          aria-label="Close date"
          className="md:w-48"
          value={close}
          onValueChange={(v) => setClose(v as CloseFilter)}
          options={[
            { value: "any", label: "Any close date" },
            { value: "month", label: "Closing this month" },
            { value: "quarter", label: "Closing this quarter" },
            { value: "overdue", label: "Close date passed" },
          ]}
        />
        {board.data ? (
          <p className="type-label text-fg-muted md:ml-auto">
            Open: <b className="font-semibold text-fg">{crmMoneyTotals(board.data.open_totals, settings.currency)}</b> · weighted {crmMoneyTotals(board.data.open_totals, settings.currency, "weighted")}
          </p>
        ) : null}
      </div>
      {board.isPending ? (
        <SkeletonGroup label="Loading the board" className="flex gap-3 overflow-hidden">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-96 w-72 shrink-0" />
          ))}
        </SkeletonGroup>
      ) : board.isError ? (
        <ErrorState title="Couldn't load the board" info={board.error.message} onRetry={() => void board.refetch()} retrying={board.isRefetching} />
      ) : board.data.columns.every((c) => c.count === 0) && (filters.length || q) ? (
        <Card padding="compact">
          <EmptyState icon={<Handshake />} title="No deals match" />
        </Card>
      ) : (
        <div className="-mx-gutter overflow-x-auto px-gutter pb-2">
          <ol className="flex gap-3" aria-label={`${board.data.pipeline.name} stages`}>
            {board.data.columns.map((col) => (
              <li
                key={col.stage.id}
                onDragOver={(e: DragEvent) => {
                  if (!dragging) return;
                  e.preventDefault();
                  e.dataTransfer.dropEffect = "move";
                  setOver(col.stage.id);
                }}
                onDragLeave={() => setOver((o) => (o === col.stage.id ? null : o))}
                onDrop={(e: DragEvent) => {
                  e.preventDefault();
                  setOver(null);
                  if (dragging) doMove(dragging, col.stage);
                  setDragging(null);
                }}
                className={cn(
                  "flex w-72 shrink-0 flex-col gap-2 rounded-lg bg-sunken p-2 transition-colors",
                  over === col.stage.id && dragging && dragging.stage.id !== col.stage.id && "bg-primary-soft",
                )}
              >
                <ColumnHeader col={col} currency={settings.currency} />
                <ColumnCards
                  orgId={orgId}
                  col={col}
                  filters={filters}
                  q={q}
                  stages={board.data.pipeline.stages}
                  onDragStart={setDragging}
                  onDragEnd={() => {
                    setDragging(null);
                    setOver(null);
                  }}
                  onMove={doMove}
                />
              </li>
            ))}
          </ol>
        </div>
      )}
    </section>
  );
}

function ColumnHeader({ col, currency }: { col: CrmBoardColumn; currency: string }) {
  return (
    <div className="flex flex-col gap-0.5 px-2 pt-1">
      <div className="flex items-center gap-2">
        <h2 className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{col.stage.name}</h2>
        <span className="type-caption tabular-nums text-fg-muted">{col.count}</span>
      </div>
      <p className="type-caption tabular-nums text-fg-muted">
        {crmMoneyTotals(col.totals, currency, "amount", { short: true })}
        {col.stage.outcome === "open" ? ` · ${crmMoneyTotals(col.totals, currency, "weighted", { short: true })} weighted (${col.stage.probability}%)` : ""}
      </p>
    </div>
  );
}

function ColumnCards({
  orgId,
  col,
  filters,
  q,
  stages,
  onDragStart,
  onDragEnd,
  onMove,
}: {
  orgId: string;
  col: CrmBoardColumn;
  filters: CrmFilter[];
  q: string;
  stages: CrmStage[];
  onDragStart: (d: CrmDeal) => void;
  onDragEnd: () => void;
  onMove: (d: CrmDeal, s: CrmStage) => void;
}) {
  const [more, setMore] = useState(false);
  const rest = useCrmRecords(
    orgId,
    "deal",
    { filters: [...filters, { key: "stage", op: "is_any", value: [col.stage.id] }], ...(q.trim() ? { q: q.trim() } : {}), sort: "close_date", dir: "asc", limit: 50 },
    { enabled: more },
  );
  const deals = more && rest.data ? rest.data.items : col.deals;
  return (
    <ul className="flex flex-col gap-2" aria-label={`${col.stage.name} deals`}>
      {deals.map((d) => (
        <DealCard key={d.id} deal={d} stages={stages} onDragStart={onDragStart} onDragEnd={onDragEnd} onMove={onMove} />
      ))}
      {col.count === 0 ? <li className="px-2 py-4 text-center type-caption text-fg-subtle">No deals</li> : null}
      {!more && col.next_cursor ? (
        <li>
          <Button variant="ghost" size="sm" fullWidth onClick={() => setMore(true)}>
            Show all {col.count}
          </Button>
        </li>
      ) : null}
      {more && rest.hasNextPage ? (
        <li>
          <Button variant="ghost" size="sm" fullWidth loading={rest.isFetchingNextPage} onClick={() => void rest.fetchNextPage()}>
            Load more
          </Button>
        </li>
      ) : null}
    </ul>
  );
}

function DealCard({ deal, stages, onDragStart, onDragEnd, onMove }: { deal: CrmDeal; stages: CrmStage[]; onDragStart: (d: CrmDeal) => void; onDragEnd: () => void; onMove: (d: CrmDeal, s: CrmStage) => void }) {
  const canMove = deal.my_level === "edit" || deal.my_level === "manage";
  const late = deal.stage.outcome === "open" && !!deal.close_date && deal.close_date < new Date().toISOString().slice(0, 10);
  return (
    <li
      draggable={canMove}
      onDragStart={(e) => {
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", deal.id);
        onDragStart(deal);
      }}
      onDragEnd={onDragEnd}
      className={cn("flex flex-col gap-2 rounded-md border border-border bg-surface p-3", canMove && "cursor-grab active:cursor-grabbing")}
    >
      <div className="flex items-start gap-2">
        <Link href={recordHref("deal", deal.id)} className="min-w-0 flex-1 type-label font-semibold text-fg hover:underline" draggable={false}>
          {deal.name}
        </Link>
        {canMove ? (
          <Menu trigger={<IconButton label={`Move ${deal.name}`} icon={<ArrowRightLeft />} size="sm" />}>
            <MenuLabel>Move to</MenuLabel>
            {stages.map((s) => (
              <MenuItem key={s.id} disabled={s.id === deal.stage.id} onSelect={() => onMove(deal, s)}>
                {s.name}
              </MenuItem>
            ))}
          </Menu>
        ) : null}
      </div>
      {deal.company ? <span className="truncate type-caption text-fg-muted">{deal.company.name}</span> : null}
      <div className="flex items-center gap-2 type-caption text-fg-muted">
        <span className="font-semibold tabular-nums text-fg">{deal.amount !== null ? crmMoney(deal.amount, deal.currency) : "No amount"}</span>
        {deal.close_date ? <span className={cn("tabular-nums", late && "font-semibold text-danger-strong")}>{dayLabel(deal.close_date)}</span> : null}
        {deal.owner ? <AppAvatar name={deal.owner.name} src={deal.owner.avatar_url} size="xs" className="ml-auto" /> : null}
      </div>
    </li>
  );
}
