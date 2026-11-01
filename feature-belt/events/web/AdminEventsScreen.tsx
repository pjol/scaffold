"use client";

import { errorMessage, formatEventDay, formatEventTime, useAdminCancelEvent, useAdminEvents, type AdminEvent, type EventStatus } from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarDays, SearchX } from "lucide-react";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { hasEnded, VISIBILITY_LABEL } from "./parts";

type StatusFilter = "any" | EventStatus;

const STATUS_OPTIONS = [
  { value: "any", label: "Any status" },
  { value: "scheduled", label: "Scheduled" },
  { value: "cancelled", label: "Cancelled" },
];

function StatusBadge({ event }: { event: AdminEvent }) {
  if (event.status === "cancelled") return <Badge tone="danger" dot>Cancelled</Badge>;
  if (hasEnded(event)) return <Badge tone="neutral" dot>Ended</Badge>;
  return <Badge tone="success" dot>Scheduled</Badge>;
}

function CancelDialog({ event, onOpenChange }: { event: AdminEvent | null; onOpenChange: (open: boolean) => void }) {
  const cancel = useAdminCancelEvent();
  const [reason, setReason] = useState("");
  return (
    <Dialog
      open={!!event}
      onOpenChange={(o) => {
        if (!o) setReason("");
        onOpenChange(o);
      }}
      title="Cancel this event?"
      info="Everyone who RSVP'd is told it was cancelled. The cancel and reason go in the audit log."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Keep event
          </Button>
          <Button
            variant="danger"
            loading={cancel.isPending}
            onClick={() =>
              event &&
              cancel.mutate(
                { id: event.id, reason: reason.trim() },
                {
                  onSuccess: () => {
                    toast.success("Event cancelled");
                    setReason("");
                    onOpenChange(false);
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't cancel the event. Try again.")),
                },
              )
            }
          >
            Cancel event
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="type-body text-fg">{event?.title}</p>
        <Field label="Reason" info="For the audit log. Attendees don't see it.">
          <TextArea value={reason} maxLength={500} rows={3} onChange={(e) => setReason(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  );
}

/** Admin → Events: every event (invite-only included), newest start first; search by title or id; cancel (audited). */
export function AdminEventsScreen() {
  const [q, setQ] = useUrlState("q", "");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "any");
  const events = useAdminEvents({ q: q || undefined, status: status === "any" ? undefined : status, limit: 50 });
  const [cancelling, setCancelling] = useState<AdminEvent | null>(null);
  const rows = events.data?.items ?? [];
  const filtered = !!q || status !== "any";

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Events" />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row">
          <SearchInput value={q} onSearch={setQ} placeholder="Title or ID" label="Search events" className="sm:max-w-sm sm:flex-1" />
          <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="sm:w-48" />
        </div>
        <Card padding="none">
          {events.isPending ? (
            <TableSkeleton rows={8} cols={4} />
          ) : events.isError ? (
            <ErrorState compact title="Couldn't load events" info={events.error.message} onRetry={() => void events.refetch()} retrying={events.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <CalendarDays />} title={filtered ? "No matching events" : "No events yet"} />
          ) : (
            <>
              <Table label="Events">
                <THead>
                  <TH>Event</TH>
                  <TH className="hidden md:table-cell">Host</TH>
                  <TH className="hidden sm:table-cell">Starts</TH>
                  <TH className="hidden lg:table-cell">Visibility</TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Going
                  </TH>
                  <TH>Status</TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((e) => (
                    <TR key={e.id}>
                      <TD className="max-w-64">
                        <span className="block truncate font-semibold text-fg">{e.title}</span>
                      </TD>
                      <TD className="hidden md:table-cell">
                        <span className="flex min-w-0 items-center gap-2">
                          <AppAvatar name={e.host.display_name || "?"} src={e.host.avatar_url} size="xs" />
                          <span className="truncate">{e.host.display_name || "Deleted user"}</span>
                        </span>
                      </TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell">
                        {formatEventDay(e.starts_at, e.timezone)}, {formatEventTime(e.starts_at, e.timezone)}
                      </TD>
                      <TD className="hidden text-fg-muted lg:table-cell">{VISIBILITY_LABEL[e.visibility]}</TD>
                      <TD className="hidden lg:table-cell" align="right">
                        {e.capacity ? `${e.going_count}/${e.capacity}` : e.going_count}
                        {e.waitlist_count > 0 ? ` +${e.waitlist_count}` : ""}
                      </TD>
                      <TD>
                        <StatusBadge event={e} />
                      </TD>
                      <TD align="right">
                        {e.status === "scheduled" && !hasEnded(e) ? (
                          <Button variant="danger-ghost" size="sm" onClick={() => setCancelling(e)}>
                            Cancel
                          </Button>
                        ) : null}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={events.hasNextPage} isFetchingNextPage={events.isFetchingNextPage} fetchNextPage={events.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <CancelDialog event={cancelling} onOpenChange={(o) => !o && setCancelling(null)} />
    </div>
  );
}
