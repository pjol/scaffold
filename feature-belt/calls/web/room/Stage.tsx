"use client";

import { isTrackReference, useSpeakingParticipants, useTracks, type TrackReferenceOrPlaceholder } from "@livekit/components-react";
import { IconButton, cn, useMediaQuery } from "@scaffoldapp/ui-web";
import { Track } from "livekit-client";
import { ChevronLeft, ChevronRight, EyeOff } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { CallLayout } from "./session";
import { Tile } from "./Tile";

function gridCols(n: number): string {
  if (n <= 1) return "grid-cols-1";
  if (n === 2) return "grid-cols-1 sm:grid-cols-2";
  if (n <= 4) return "grid-cols-2";
  if (n <= 9) return "grid-cols-2 md:grid-cols-3";
  return "grid-cols-2 md:grid-cols-3 lg:grid-cols-4";
}

const key = (r: TrackReferenceOrPlaceholder) => `${r.participant.identity}:${r.source}`;

export interface StageProps {
  layout: CallLayout;
  overlay?: ReactNode;
  /** Pinned for you (identity), or null. */
  pinned: string | null;
  onPin: (identity: string | null) => void;
  /** Spotlighted for everyone by a host (identity), or "". */
  spotlight: string;
  /** Show your own tile. */
  selfView: boolean;
  /** Mirror your own camera. */
  mirror: boolean;
}

/**
 * The video area. A screen share takes the stage with everyone in a strip beside it
 * (below on phones). Then whoever you pinned, else whoever a host spotlighted, large.
 * Otherwise: auto (2 people = the other person large with you in a corner, more = grid),
 * grid (16 a page on desktop, 6 on phones, with pages), or speaker (whoever spoke last).
 */
export function Stage({ layout, overlay, pinned, onPin, spotlight, selfView, mirror }: StageProps) {
  const refs = useTracks(
    [
      { source: Track.Source.Camera, withPlaceholder: true },
      { source: Track.Source.ScreenShare, withPlaceholder: false },
    ],
    { onlySubscribed: false },
  );
  const wide = useMediaQuery("(min-width: 768px)");
  const perPage = wide ? 16 : 6;
  const allCameras = useMemo(() => refs.filter((r) => r.source === Track.Source.Camera), [refs]);
  const cameras = useMemo(() => (selfView ? allCameras : allCameras.filter((r) => !r.participant.isLocal)), [allCameras, selfView]);
  const screens = useMemo(() => refs.filter((r) => r.source === Track.Source.ScreenShare && isTrackReference(r)), [refs]);
  const speakers = useSpeakingParticipants();
  const [lastSpeaker, setLastSpeaker] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  useEffect(() => {
    const remote = speakers.find((p) => !p.isLocal);
    if (remote) setLastSpeaker(remote.identity);
  }, [speakers]);

  const tile = (r: TrackReferenceOrPlaceholder, props: { featured?: boolean; className?: string } = {}) => {
    const camera = r.source === Track.Source.Camera;
    const id = r.participant.identity;
    return (
      <Tile
        key={key(r)}
        trackRef={r}
        mirror={mirror}
        pinned={camera && pinned === id}
        spotlit={camera && spotlight === id}
        {...(camera ? { onPin: () => onPin(pinned === id ? null : id) } : {})}
        {...props}
      />
    );
  };

  const remoteCams = allCameras.filter((r) => !r.participant.isLocal);
  const localCam = allCameras.find((r) => r.participant.isLocal);
  const focus = [pinned, spotlight].find((id) => id && allCameras.some((r) => r.participant.identity === id));
  const focusRef = focus ? allCameras.find((r) => r.participant.identity === focus) : undefined;
  const small = "aspect-video h-full shrink-0 md:h-auto md:w-full";

  let body: ReactNode;
  const screen = screens[0];
  if (screen) {
    body = (
      <div className="flex h-full min-h-0 flex-col gap-2 md:flex-row">
        {tile(screen, { featured: true, className: "min-h-0 flex-1" })}
        <Strip>{[...screens.slice(1), ...cameras].map((r) => tile(r, { className: small }))}</Strip>
      </div>
    );
  } else if (focusRef) {
    body = (
      <div className="flex h-full min-h-0 flex-col gap-2 md:flex-row">
        {tile(focusRef, { featured: true, className: "min-h-0 flex-1" })}
        <Strip>{cameras.filter((r) => r !== focusRef).map((r) => tile(r, { className: small }))}</Strip>
      </div>
    );
  } else if ((layout === "speaker" && allCameras.length > 1) || (layout === "auto" && remoteCams.length === 1)) {
    const featured = remoteCams.find((r) => r.participant.identity === lastSpeaker) ?? remoteCams[0] ?? localCam;
    const others = cameras.filter((r) => r !== featured);
    body =
      layout === "auto" && featured ? (
        <div className="relative h-full min-h-0">
          {tile(featured, { featured: true, className: "size-full" })}
          {localCam && selfView && featured !== localCam && tile(localCam, { className: "absolute right-3 bottom-3 aspect-video w-32 shadow-lg sm:w-48 lg:w-64" })}
        </div>
      ) : (
        <div className="flex h-full min-h-0 flex-col gap-2 md:flex-row">
          {featured && tile(featured, { featured: true, className: "min-h-0 flex-1" })}
          <Strip>{others.map((r) => tile(r, { className: small }))}</Strip>
        </div>
      );
  } else if (cameras.length === 0) {
    body = (
      <div className="flex h-full items-center justify-center rounded-lg bg-ink-900 text-ink-300">
        <span className="flex items-center gap-2 type-label">
          <EyeOff className="size-4" aria-hidden />
          Your video is hidden from you
        </span>
      </div>
    );
  } else {
    const pages = Math.max(1, Math.ceil(cameras.length / perPage));
    const current = Math.min(page, pages - 1);
    const shown = cameras.slice(current * perPage, (current + 1) * perPage);
    const n = shown.length;
    body = (
      <div className="flex h-full min-h-0 flex-col gap-2">
        <div className={cn("grid min-h-0 flex-1 gap-2", gridCols(n), n <= 6 ? "auto-rows-fr" : "content-start overflow-y-auto")}>
          {shown.map((r) => tile(r, { featured: n <= 2, ...(n > 6 ? { className: "aspect-video" } : {}) }))}
        </div>
        {pages > 1 && (
          <div className="flex shrink-0 items-center justify-center gap-3" role="navigation" aria-label="Pages of people">
            <IconButton label="Previous page" icon={<ChevronLeft />} variant="secondary" size="sm" disabled={current === 0} onClick={() => setPage(current - 1)} />
            <span className="type-label tabular-nums text-fg-muted">
              {current + 1} / {pages}
            </span>
            <IconButton label="Next page" icon={<ChevronRight />} variant="secondary" size="sm" disabled={current >= pages - 1} onClick={() => setPage(current + 1)} />
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="relative h-full min-h-0 min-w-0 flex-1" data-calls-stage="">
      {body}
      {overlay}
    </div>
  );
}

/** A scrolling strip of small tiles: a column on desktop, a row on phones. */
function Strip({ children }: { children: ReactNode[] }) {
  if (children.length === 0) return null;
  return (
    <div className="flex h-28 shrink-0 gap-2 overflow-x-auto md:h-auto md:w-56 md:flex-col md:overflow-x-hidden md:overflow-y-auto lg:w-64" aria-label="Participants">
      {children}
    </div>
  );
}
