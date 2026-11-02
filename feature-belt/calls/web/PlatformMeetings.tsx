"use client";

import { formatCallsWhen, useCallsTimeZone, type CallsPlatformMeeting } from "@scaffoldapp/shared";
import { Button, ProviderIcon, SourceBadge } from "@scaffoldapp/ui-web";
import { CalendarDays, Copy, ExternalLink, Video } from "lucide-react";
import { useRouter } from "next/navigation";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { copyMeetingLink } from "./MeetingDialogs";

const PROVIDERS: Record<string, { label: string; platform: string; product: string | null }> = {
  google_meet: { label: "Google Meet", platform: "google", product: "meet" },
  teams: { label: "Microsoft Teams", platform: "microsoft", product: "teams" },
  zoom: { label: "Zoom", platform: "zoom", product: null },
};

/** "Google Meet", "Zoom", "Microsoft Teams". */
export function platformMeetingLabel(provider: string): string {
  return PROVIDERS[provider]?.label ?? "Video call";
}

/**
 * A Meet, Teams or Zoom meeting from the person's calendars, beside the app's own: the
 * provider's mark, a source badge when it comes from a connected calendar, and "Join on …",
 * which opens the platform's app or web client (those meetings don't run inside this app).
 */
export function PlatformMeetingRow({ meeting: m, past }: { meeting: CallsPlatformMeeting; past?: boolean }) {
  const router = useRouter();
  const zone = useCallsTimeZone();
  const p = PROVIDERS[m.provider];
  const startsSoon = !past && Date.parse(m.starts_at) - Date.now() < 10 * 60 * 1000 && Date.parse(m.ends_at) > Date.now();
  const actions = [
    { id: "copy", label: "Copy link", icon: Copy, onSelect: () => void copyMeetingLink(m.join_url) },
    ...(m.link ? [{ id: "calendar", label: "Open in Calendar", icon: CalendarDays, onSelect: () => router.push(m.link!) }] : []),
  ];
  return (
    <li className="flex min-h-row items-center gap-3 px-4 py-3">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-sunken text-fg">
        {p ? <ProviderIcon provider={p.platform} product={p.product} size={20} /> : <Video className="size-5" aria-hidden />}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate type-body font-semibold text-fg">{m.title || "Meeting"}</span>
          <SourceBadge source={m.source} className="hidden sm:inline-flex" />
        </span>
        <span className="line-clamp-2 break-words type-label text-fg-muted">
          {formatCallsWhen(m.starts_at, zone)} · {platformMeetingLabel(m.provider)}
          {m.organizer ? ` · ${m.organizer}` : ""}
        </span>
      </div>
      {!past ? (
        <Button asChild size="sm" variant={startsSoon ? "primary" : "secondary"} leftIcon={<ExternalLink />}>
          <a href={m.join_url} target="_blank" rel="noopener noreferrer" aria-label={`Join ${m.title || "the meeting"} on ${platformMeetingLabel(m.provider)}`}>
            <span className="hidden sm:inline">Join on {platformMeetingLabel(m.provider)}</span>
            <span className="sm:hidden">Join</span>
          </a>
        </Button>
      ) : null}
      {m.source?.type === "connected" ? (
        <ResourceMenu feature={`connectors:${m.source.connection_id}`} title={m.title || "Meeting"} actions={actions} />
      ) : (
        <ResourceMenu feature="calendar" kind="calendar.event" id={m.id} title={m.title || "Meeting"} actions={actions} />
      )}
    </li>
  );
}
