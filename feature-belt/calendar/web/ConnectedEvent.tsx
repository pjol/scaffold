"use client";

import {
  calendarJoinLabel,
  calendarProviderPlatform,
  calendarProviderProduct,
  errorMessage,
  formatCalendarRange,
  isApiError,
  isConnectionErrorCode,
  sourceProductLabel,
  useCalendarConnectedEvent,
  useDeleteCalendarConnectedEvent,
  useRespondCalendarConnectedEvent,
  type CalendarConnectedEventDetail,
  type CalendarConnectedRef,
  type CalendarResponse,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, ProviderIcon, SegmentedControl, Skeleton, SkeletonGroup, SourceBadge, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, CalendarDays, CalendarX, Copy, ExternalLink, MapPin, Pencil, Repeat, Trash2, UserRound, Users, Video } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { ScopeDialog } from "./EventDialog";
import { ColorDot, ResponseBadge, copyText, useCalendarZone } from "./parts";

function Row({ icon, children, label }: { icon: ReactNode; children: ReactNode; label: string }) {
  return (
    <div className="flex items-start gap-3 type-body text-fg">
      <span className="flex size-6 shrink-0 items-center justify-center text-fg-muted [&_svg]:size-5" aria-label={label} role="img">
        {icon}
      </span>
      <div className="min-w-0 flex-1 break-words">{children}</div>
    </div>
  );
}

const isURL = (v: string) => /^https?:\/\/\S+$/.test(v);

/**
 * An event from a connected calendar (Google, Outlook), read live: when, calendar, the
 * meeting link (Meet, Teams and Zoom open in their own apps), guests and replies, RSVP,
 * and the organizer's edit and delete — all written back to the platform.
 */
export function ConnectedEventPanel({ eventRef, onEdit, onDeleted }: { eventRef: CalendarConnectedRef; onEdit: (e: CalendarConnectedEventDetail) => void; onDeleted: () => void }) {
  const { zone } = useCalendarZone();
  const event = useCalendarConnectedEvent(eventRef);
  const respond = useRespondCalendarConnectedEvent();
  const remove = useDeleteCalendarConnectedEvent();
  const [confirm, setConfirm] = useState<"delete" | "scope" | null>(null);

  if (event.isPending) {
    return (
      <SkeletonGroup label="Loading event" className="flex flex-col gap-4">
        <Skeleton shape="text" className="h-7 w-2/3" />
        <Skeleton shape="text" className="h-5 w-1/2" />
        <Skeleton shape="text" className="h-5 w-1/3" />
        <Skeleton className="h-24" />
      </SkeletonGroup>
    );
  }
  if (event.isError) {
    const err = event.error;
    if (isApiError(err) && err.status === 404) return <EmptyState compact icon={<CalendarX />} title="This event isn't there any more" />;
    const fix = isApiError(err) && isConnectionErrorCode(err.code) ? (err.details?.["fix_url"] as string | undefined) : undefined;
    return (
      <div className="flex flex-col items-center gap-2">
        <ErrorState compact title="Couldn't load this event" info={err.message} onRetry={() => void event.refetch()} retrying={event.isFetching} />
        {fix ? (
          <Button asChild size="sm" variant="secondary">
            <Link href={fix}>Reconnect</Link>
          </Button>
        ) : null}
      </div>
    );
  }
  const e = event.data;
  const product = sourceProductLabel(e.source.provider, e.source.product);
  const owner = e.can_edit;
  const counts = e.attendees.reduce((acc, a) => ({ ...acc, [a.response]: (acc[a.response] ?? 0) + 1 }), {} as Partial<Record<CalendarResponse, number>>);
  const summary = [counts.accepted && `${counts.accepted} going`, counts.tentative && `${counts.tentative} maybe`, counts.declined && `${counts.declined} declined`, counts.needs_action && `${counts.needs_action} awaiting`]
    .filter(Boolean)
    .join(" · ");
  const otherZone = !e.all_day && e.timezone && e.timezone !== zone;
  const platform = calendarProviderPlatform(e.conference_provider);

  const doDelete = async (scope?: "this" | "all") => {
    try {
      await remove.mutateAsync({ ref: eventRef, ...(scope ? { scope } : {}) });
      toast.success(`Deleted from ${product}`);
      onDeleted();
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't delete the event. Try again."));
      throw err;
    }
  };

  const actions = [
    ...(owner ? [{ id: "edit", label: "Edit", icon: Pencil, onSelect: () => onEdit(e) }] : []),
    ...(e.web_url ? [{ id: "open", label: `Open in ${product}`, icon: ExternalLink, onSelect: () => window.open(e.web_url, "_blank", "noopener,noreferrer") }] : []),
    ...(e.conference_url ? [{ id: "copy-video", label: "Copy meeting link", icon: Copy, onSelect: () => void copyText(e.conference_url ?? "", toast) }] : []),
    ...(owner ? [{ id: "delete", label: "Delete", icon: Trash2, destructive: true, onSelect: () => setConfirm(e.recurring ? "scope" : "delete") }] : []),
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start gap-3">
        <ColorDot color={e.color} className="mt-3" />
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <h2 className="break-words type-title text-fg">{e.title}</h2>
          <SourceBadge source={e.source} showAccount className="self-start" />
        </div>
        <ResourceMenu feature={`connectors:${e.connection_id}`} title={`${product} · ${e.source.account}`} actions={actions} />
      </div>

      <div className="flex flex-col gap-3">
        <Row icon={<CalendarClock />} label="When">
          {formatCalendarRange(e, zone)}
          {otherZone ? <span className="block type-label text-fg-muted">{formatCalendarRange(e, e.timezone)} in {e.timezone.replace(/_/g, " ")}</span> : null}
        </Row>
        {e.recurring ? (
          <Row icon={<Repeat />} label="Repeats">
            Part of a repeating event
          </Row>
        ) : null}
        {e.location && e.location !== e.conference_url ? (
          <Row icon={<MapPin />} label="Location">
            {isURL(e.location) ? (
              <a href={e.location} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                {e.location}
              </a>
            ) : (
              e.location
            )}
          </Row>
        ) : null}
        {e.conference_url ? (
          <Row icon={platform ? <ProviderIcon provider={platform} product={calendarProviderProduct(e.conference_provider)} size={20} /> : <Video />} label="Video call">
            <Button asChild size="sm" leftIcon={<ExternalLink />}>
              <a href={e.conference_url} target="_blank" rel="noopener noreferrer">
                {calendarJoinLabel(e.conference_provider)}
              </a>
            </Button>
          </Row>
        ) : null}
        <Row icon={<UserRound />} label="Organizer">
          {e.organizer.self ? `You organize it (${e.source.account})` : `Organized by ${e.organizer.name || e.organizer.email || "someone else"}`}
        </Row>
        <Row icon={<CalendarDays />} label="Calendar">
          {!e.calendar_name || e.calendar_name === e.source.account ? `${product} (main calendar)` : e.calendar_name}
        </Row>
      </div>

      {e.description ? <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{e.description}</p> : null}

      {e.attendees.length > 0 ? (
        <section className="flex flex-col gap-3" aria-label="Guests">
          <div className="flex items-center gap-2 type-label text-fg-muted">
            <Users className="size-4" aria-hidden />
            <span>
              {e.attendees.length} {e.attendees.length === 1 ? "guest" : "guests"}
              {summary ? ` · ${summary}` : ""}
            </span>
          </div>
          <ul className="flex flex-col gap-2">
            {e.attendees.map((a) => (
              <li key={a.email} className="flex min-h-control items-center gap-3">
                <AppAvatar name={a.name || a.email} size="sm" />
                <span className="min-w-0 flex-1 truncate type-body text-fg">
                  {a.name || a.email}
                  {a.self ? <span className="text-fg-muted"> (you)</span> : null}
                </span>
                <ResponseBadge response={a.response} />
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {e.my_response ? (
        e.can_respond ? (
          <div className="flex flex-col gap-2">
            <span className="type-label font-semibold text-fg">Going?{e.recurring ? " (this event)" : ""}</span>
            <SegmentedControl
              aria-label="Your reply"
              value={e.my_response === "needs_action" ? undefined : e.my_response}
              onValueChange={(v) =>
                respond.mutate(
                  { ref: eventRef, response: v },
                  { onSuccess: () => toast.success(`Reply sent through ${product}`), onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply.")) },
                )
              }
              options={[
                { value: "accepted", label: "Yes" },
                { value: "tentative", label: "Maybe" },
                { value: "declined", label: "No" },
              ]}
            />
          </div>
        ) : (
          <p className="type-label text-fg-muted">Reply in {product}, or allow changes to {product} in Settings → Connections.</p>
        )
      ) : owner ? (
        <Button variant="secondary" size="auto" leftIcon={<Pencil />} className="self-start" onClick={() => onEdit(e)}>
          Edit
        </Button>
      ) : null}

      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(o) => setConfirm(o ? "delete" : null)}
        title={`Delete “${e.title}”?`}
        tone="danger"
        confirmLabel="Delete"
        onConfirm={() => doDelete()}
        errorText={(err) => errorMessage(err, "Couldn't delete the event. Try again.")}
      >
        <p className="type-body text-fg">
          {e.attendees.length > 0 ? `It's deleted from ${product}, which tells the guests.` : `It's deleted from ${product}.`}
        </p>
      </ConfirmDialog>
      <ScopeDialog
        open={confirm === "scope"}
        onOpenChange={(o) => setConfirm(o ? "scope" : null)}
        title="Delete repeating event"
        confirmLabel="Delete"
        tone="danger"
        allowFollowing={false}
        onConfirm={(s) => doDelete(s === "all" ? "all" : "this")}
      />
    </div>
  );
}
