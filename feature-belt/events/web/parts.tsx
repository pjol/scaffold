"use client";

import { eventDateBadge, type EventItem, type EventVisibility, type PublicEvent, type RsvpStatus } from "@scaffoldapp/shared";
import { Badge, cn, type BadgeTone } from "@scaffoldapp/ui-web";
import Image from "next/image";
import { isOptimisable } from "@/lib/images";

export const RSVP_LABEL: Record<RsvpStatus, string> = { going: "Going", maybe: "Maybe", declined: "Can't go", waitlisted: "On the waitlist" };
const RSVP_TONE: Record<RsvpStatus, BadgeTone> = { going: "success", maybe: "info", declined: "neutral", waitlisted: "warning" };

export const VISIBILITY_LABEL: Record<EventVisibility, string> = { public: "Public", members: "Members", invite: "Invite only" };
export const VISIBILITY_INFO: Record<EventVisibility, string> = {
  public: "Anyone with the link can read it, signed in or not. Who's going stays private.",
  members: "Everyone signed in can see it and RSVP.",
  invite: "Only hosts, people you invite and people with the invite link can see it.",
};

/** Whether the event's end time has passed. */
export function hasEnded(event: Pick<EventItem | PublicEvent, "ends_at">, now = Date.now()): boolean {
  return new Date(event.ends_at).getTime() <= now;
}

/** "12 going", "12 of 20 going". */
export function goingText(event: Pick<EventItem, "going_count" | "capacity">): string {
  return event.capacity ? `${event.going_count} of ${event.capacity} going` : `${event.going_count} going`;
}

/** Month and day block for lists. */
export function DateBadge({ iso, timeZone, className }: { iso: string; timeZone: string; className?: string }) {
  const d = eventDateBadge(iso, timeZone);
  return (
    <span aria-hidden className={cn("flex size-14 shrink-0 flex-col items-center justify-center rounded-md bg-primary-soft text-primary-strong", className)}>
      <span className="type-caption font-semibold">{d.month}</span>
      <span className="type-title-sm">{d.day}</span>
    </span>
  );
}

export function RsvpBadge({ status }: { status: RsvpStatus }) {
  return (
    <Badge tone={RSVP_TONE[status]} dot>
      {RSVP_LABEL[status]}
    </Badge>
  );
}

/** Status badges for a list row: cancelled, ended, full, and the viewer's RSVP. */
export function EventBadges({ event }: { event: EventItem }) {
  const ended = hasEnded(event);
  return (
    <span className="flex flex-wrap items-center gap-2">
      {event.status === "cancelled" ? (
        <Badge tone="danger">Cancelled</Badge>
      ) : ended ? (
        <Badge tone="neutral">Ended</Badge>
      ) : event.is_full ? (
        <Badge tone="warning">Full</Badge>
      ) : null}
      {event.my_rsvp && event.my_rsvp.status !== "declined" ? <RsvpBadge status={event.my_rsvp.status} /> : null}
      {event.host_role ? <Badge tone="primary">Hosting</Badge> : null}
    </span>
  );
}

/** Cover photo at 16:9, through next/image when the host is known. */
export function CoverImage({ src, alt, priority, className }: { src: string; alt: string; priority?: boolean; className?: string }) {
  return (
    <div className={cn("relative aspect-video w-full overflow-hidden rounded-lg bg-sunken", className)}>
      <Image src={src} alt={alt} fill sizes="(min-width: 1024px) 768px, 100vw" className="object-cover" priority={priority ?? false} unoptimized={!isOptimisable(src)} />
    </div>
  );
}

/** Save text as a file through the browser's download flow. */
export function downloadText(text: string, filename: string, type: string): void {
  const blob = new Blob([text], { type });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** A file name from a title: "board-game-night.ics". */
export function icsFileName(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
  return `${slug || "event"}.ics`;
}
