"use client";

import { isTrackReference, useSpeakingParticipants, useTracks, type TrackReferenceOrPlaceholder } from "@livekit/components-react";
import { cn } from "@scaffoldapp/ui-web";
import { Track } from "livekit-client";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { CallLayout } from "./session";
import { Tile } from "./Tile";

/** Most tiles a grid shows; the rest are counted on a last tile (the people panel lists everyone). */
const MAX_GRID = 16;

function gridCols(n: number): string {
  if (n <= 1) return "grid-cols-1";
  if (n === 2) return "grid-cols-1 sm:grid-cols-2";
  if (n <= 4) return "grid-cols-2";
  if (n <= 9) return "grid-cols-2 md:grid-cols-3";
  return "grid-cols-2 md:grid-cols-3 lg:grid-cols-4";
}

const key = (r: TrackReferenceOrPlaceholder) => `${r.participant.identity}:${r.source}`;

/**
 * The video area. A screen share takes the stage with everyone in a strip beside it
 * (below on phones). Otherwise: auto (2 people = the other person large with you in a
 * corner, more = grid), grid, or speaker (whoever spoke last, large).
 */
export function Stage({ layout, overlay, onShowPeople }: { layout: CallLayout; overlay?: ReactNode; onShowPeople: () => void }) {
  const refs = useTracks(
    [
      { source: Track.Source.Camera, withPlaceholder: true },
      { source: Track.Source.ScreenShare, withPlaceholder: false },
    ],
    { onlySubscribed: false },
  );
  const cameras = useMemo(() => refs.filter((r) => r.source === Track.Source.Camera), [refs]);
  const screens = useMemo(() => refs.filter((r) => r.source === Track.Source.ScreenShare && isTrackReference(r)), [refs]);
  const speakers = useSpeakingParticipants();
  const [lastSpeaker, setLastSpeaker] = useState<string | null>(null);
  useEffect(() => {
    const remote = speakers.find((p) => !p.isLocal);
    if (remote) setLastSpeaker(remote.identity);
  }, [speakers]);

  const remoteCams = cameras.filter((r) => !r.participant.isLocal);
  const localCam = cameras.find((r) => r.participant.isLocal);

  let body: ReactNode;
  const screen = screens[0];
  if (screen) {
    body = (
      <div className="flex h-full min-h-0 flex-col gap-2 md:flex-row">
        <Tile trackRef={screen} featured className="min-h-0 flex-1" />
        <Strip refs={[...screens.slice(1), ...cameras]} />
      </div>
    );
  } else if ((layout === "speaker" && cameras.length > 1) || (layout === "auto" && remoteCams.length === 1)) {
    const featured = remoteCams.find((r) => r.participant.identity === lastSpeaker) ?? remoteCams[0] ?? localCam;
    const others = cameras.filter((r) => r !== featured);
    body =
      layout === "auto" && localCam && featured ? (
        <div className="relative h-full min-h-0">
          <Tile trackRef={featured} featured className="size-full" />
          <Tile trackRef={localCam} className="absolute right-3 bottom-3 aspect-video w-32 shadow-lg sm:w-48 lg:w-64" />
        </div>
      ) : (
        <div className="flex h-full min-h-0 flex-col gap-2 md:flex-row">
          {featured && <Tile trackRef={featured} featured className="min-h-0 flex-1" />}
          <Strip refs={others} />
        </div>
      );
  } else {
    const shown = cameras.length > MAX_GRID ? cameras.slice(0, MAX_GRID - 1) : cameras;
    const hidden = cameras.length - shown.length;
    const n = shown.length + (hidden ? 1 : 0);
    body = (
      <div className={cn("grid h-full min-h-0 gap-2", gridCols(n), n <= 6 ? "auto-rows-fr" : "content-start overflow-y-auto")}>
        {shown.map((r) => (
          <Tile key={key(r)} trackRef={r} featured={n <= 2} className={n > 6 ? "aspect-video" : undefined} />
        ))}
        {hidden > 0 && (
          <button
            type="button"
            onClick={onShowPeople}
            className="flex items-center justify-center rounded-lg bg-ink-900 type-title-sm text-ink-50 hover:bg-ink-800 focus-visible:outline-2 focus-visible:outline-ring"
          >
            {hidden} more
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="relative h-full min-h-0 min-w-0 flex-1">
      {body}
      {overlay}
    </div>
  );
}

/** A scrolling strip of small tiles: a column on desktop, a row on phones. */
function Strip({ refs }: { refs: TrackReferenceOrPlaceholder[] }) {
  if (refs.length === 0) return null;
  return (
    <div className="flex h-28 shrink-0 gap-2 overflow-x-auto md:h-auto md:w-56 md:flex-col md:overflow-x-hidden md:overflow-y-auto lg:w-64" aria-label="Participants">
      {refs.map((r) => (
        <Tile key={key(r)} trackRef={r} className="aspect-video h-full shrink-0 md:h-auto md:w-full" />
      ))}
    </div>
  );
}
