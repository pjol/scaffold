"use client";

import {
  CRM_TICKET_PRIORITY_LABEL,
  errorMessage,
  useCrmMoveTicket,
  useCrmPipelines,
  useCrmRecords,
  useCrmTicketBoard,
  useTopic,
  topic,
  type CrmFilter,
  type CrmStage,
  type CrmTicket,
  type CrmTicketBoardColumn,
  type CrmTicketPriority,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, IconButton, Input, Menu, MenuItem, MenuLabel, Select, Skeleton, SkeletonGroup, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowRightLeft, LifeBuoy, List, Plus } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type DragEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { CreateRecordDialog } from "./CreateRecordDialog";
import { PriorityBadge, dayLabel, pipelinesFor, recordHref } from "./parts";

/** /crm/tickets/board: a ticket pipeline's statuses as columns; drag a card (or use its menu) to move it. */
export function CrmTicketBoardScreen() {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame
      actions={
        <>
          <Button asChild variant="secondary" size="auto" leftIcon={<List />}>
            <Link href="/crm/tickets">Table</Link>
          </Button>
          <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
            New ticket
          </Button>
        </>
      }
    >
      <Board />
      <CreateRecordDialog type="ticket" open={creating} onOpenChange={setCreating} />
    </CrmFrame>
  );
}

type OwnerFilter = "all" | "me";

function Board() {
  const { orgId } = useCrm();
  const pipelines = pipelinesFor(useCrmPipelines(orgId).data, "ticket");
  const [pipelineId, setPipelineId] = useUrlState("pipeline", "");
  const [owner, setOwner] = useUrlState<OwnerFilter>("owner", "all");
  const [priority, setPriority] = useUrlState<"any" | CrmTicketPriority>("priority", "any");
  const [q, setQ] = useState("");
  const filters = useMemo<CrmFilter[]>(
    () => [...(owner === "me" ? [{ key: "owner", op: "is_any" as const, value: ["me"] }] : []), ...(priority !== "any" ? [{ key: "priority", op: "is_any" as const, value: [priority] }] : [])],
    [owner, priority],
  );
  const params = useMemo(() => ({ filters, ...(q.trim() ? { q: q.trim() } : {}) }), [filters, q]);
  const board = useCrmTicketBoard(orgId, pipelineId || undefined, params);
  useTopic(board.data ? topic("crm", board.data.pipeline.id) : null);
  const move = useCrmMoveTicket(orgId);
  const [dragging, setDragging] = useState<CrmTicket | null>(null);
  const [over, setOver] = useState<string | null>(null);

  const doMove = (t: CrmTicket, stage: CrmStage) => {
    if (t.stage.id === stage.id) return;
    if (t.my_level !== "edit" && t.my_level !== "manage") {
      toast.error("You can't edit this ticket.");
      return;
    }
    move.mutate({ ticketId: t.id, stageId: stage.id }, { onSuccess: () => toast.success(`${t.name} → ${stage.name}`), onError: (e) => toast.error(errorMessage(e, "Couldn't move the ticket.")) });
  };

  return (
    <section className="flex flex-col gap-4" aria-label="Tickets board">
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        {pipelines.length > 1 ? (
          <Select
            aria-label="Pipeline"
            className="md:w-56"
            value={pipelineId || pipelines[0]?.id}
            onValueChange={(v) => setPipelineId(v === pipelines[0]?.id ? "" : v)}
            options={pipelines.map((p) => ({ value: p.id, label: p.name }))}
          />
        ) : null}
        <Input aria-label="Search tickets" placeholder="Search tickets" value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="md:w-56" />
        <Select
          aria-label="Owner"
          className="md:w-40"
          value={owner}
          onValueChange={(v) => setOwner(v as OwnerFilter)}
          options={[
            { value: "all", label: "Everyone's" },
            { value: "me", label: "My tickets" },
          ]}
        />
        <Select
          aria-label="Priority"
          className="md:w-40"
          value={priority}
          onValueChange={(v) => setPriority(v as "any" | CrmTicketPriority)}
          options={[{ value: "any", label: "Any priority" }, ...(Object.keys(CRM_TICKET_PRIORITY_LABEL) as CrmTicketPriority[]).map((k) => ({ value: k, label: CRM_TICKET_PRIORITY_LABEL[k] }))]}
        />
        {board.data ? (
          <p className="type-label text-fg-muted md:ml-auto">
            Open: <b className="font-semibold text-fg">{board.data.columns.filter((c) => c.stage.outcome === "open").reduce((n, c) => n + c.count, 0)}</b>
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
        <Card padding="compact">
          <EmptyState icon={<LifeBuoy />} title="Couldn't load the board" info={board.error.message} action={<Button size="auto" variant="secondary" onClick={() => void board.refetch()}>Try again</Button>} />
        </Card>
      ) : board.data.columns.every((c) => c.count === 0) ? (
        <Card padding="compact">
          <EmptyState icon={<LifeBuoy />} title={filters.length || q ? "No tickets match" : "No tickets yet"} />
        </Card>
      ) : (
        <div className="-mx-gutter overflow-x-auto px-gutter pb-2">
          <ol className="flex gap-3" aria-label={`${board.data.pipeline.name} statuses`}>
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
                className={cn("flex w-72 shrink-0 flex-col gap-2 rounded-lg bg-sunken p-2 transition-colors", over === col.stage.id && dragging && dragging.stage.id !== col.stage.id && "bg-primary-soft")}
              >
                <div className="flex items-center gap-2 px-2 pt-1">
                  <h2 className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{col.stage.name}</h2>
                  <span className="type-caption tabular-nums text-fg-muted">{col.count}</span>
                </div>
                <Cards
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

function Cards({
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
  col: CrmTicketBoardColumn;
  filters: CrmFilter[];
  q: string;
  stages: CrmStage[];
  onDragStart: (t: CrmTicket) => void;
  onDragEnd: () => void;
  onMove: (t: CrmTicket, s: CrmStage) => void;
}) {
  const [more, setMore] = useState(false);
  const rest = useCrmRecords(
    orgId,
    "ticket",
    { filters: [...filters, { key: "stage", op: "is_any", value: [col.stage.id] }], ...(q.trim() ? { q: q.trim() } : {}), sort: "priority", dir: "desc", limit: 50 },
    { enabled: more },
  );
  const tickets = more && rest.data ? rest.data.items : col.deals;
  return (
    <ul className="flex flex-col gap-2" aria-label={`${col.stage.name} tickets`}>
      {tickets.map((t) => (
        <TicketCard key={t.id} ticket={t} stages={stages} onDragStart={onDragStart} onDragEnd={onDragEnd} onMove={onMove} />
      ))}
      {col.count === 0 ? <li className="px-2 py-4 text-center type-caption text-fg-subtle">No tickets</li> : null}
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

function TicketCard({ ticket: t, stages, onDragStart, onDragEnd, onMove }: { ticket: CrmTicket; stages: CrmStage[]; onDragStart: (t: CrmTicket) => void; onDragEnd: () => void; onMove: (t: CrmTicket, s: CrmStage) => void }) {
  const canMove = t.my_level === "edit" || t.my_level === "manage";
  return (
    <li
      draggable={canMove}
      onDragStart={(e) => {
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", t.id);
        onDragStart(t);
      }}
      onDragEnd={onDragEnd}
      className={cn("flex flex-col gap-2 rounded-md border border-border bg-surface p-3", canMove && "cursor-grab active:cursor-grabbing")}
    >
      <div className="flex items-start gap-2">
        <Link href={recordHref("ticket", t.id)} className="min-w-0 flex-1 type-label font-semibold text-fg hover:underline" draggable={false}>
          {t.name}
        </Link>
        {canMove ? (
          <Menu trigger={<IconButton label={`Move ${t.name}`} icon={<ArrowRightLeft />} size="sm" />}>
            <MenuLabel>Move to</MenuLabel>
            {stages.map((s) => (
              <MenuItem key={s.id} disabled={s.id === t.stage.id} onSelect={() => onMove(t, s)}>
                {s.name}
              </MenuItem>
            ))}
          </Menu>
        ) : null}
      </div>
      {t.company ? <span className="truncate type-caption text-fg-muted">{t.company.name}</span> : null}
      <div className="flex items-center gap-2 type-caption text-fg-muted">
        <PriorityBadge priority={t.priority} />
        <span className="tabular-nums">Opened {dayLabel(t.created_at)}</span>
        {t.owner ? <AppAvatar name={t.owner.name} src={t.owner.avatar_url} size="xs" className="ml-auto" /> : null}
      </div>
    </li>
  );
}
