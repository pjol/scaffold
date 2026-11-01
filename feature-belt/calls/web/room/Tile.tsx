"use client";

import {
  isTrackReference,
  useConnectionQualityIndicator,
  useIsMuted,
  useIsSpeaking,
  useParticipantAttributes,
  useParticipantInfo,
  VideoTrack,
  type TrackReferenceOrPlaceholder,
} from "@livekit/components-react";
import { CALLS_ROLE_LABELS, callsHandRaisedAt, isCallsHostRole, parseCallsParticipantMeta } from "@scaffoldapp/shared";
import { Avatar, cn } from "@scaffoldapp/ui-web";
import { ConnectionQuality, Track, type Participant } from "livekit-client";
import { Hand, MicOff, MonitorUp, SignalLow, WifiOff } from "lucide-react";

/** A participant's display facts from the room (server-owned name, role and avatar). */
export function useParticipantFacts(p: Participant) {
  const info = useParticipantInfo({ participant: p });
  const { attributes } = useParticipantAttributes({ participant: p });
  const meta = parseCallsParticipantMeta(info.metadata);
  return {
    name: info.name || (meta.role === "guest" ? "Guest" : "Participant"),
    meta,
    hand: callsHandRaisedAt(attributes),
  };
}

/** One person's video (or their avatar when the camera is off), with name, mic, hand and connection. */
export function Tile({ trackRef, featured, className }: { trackRef: TrackReferenceOrPlaceholder; featured?: boolean; className?: string }) {
  const p = trackRef.participant;
  const facts = useParticipantFacts(p);
  const speaking = useIsSpeaking(p);
  const { quality } = useConnectionQualityIndicator({ participant: p });
  const micMuted = useIsMuted({ participant: p, source: Track.Source.Microphone });
  const screen = trackRef.source === Track.Source.ScreenShare;
  const videoMuted = useIsMuted(trackRef);
  const showVideo = isTrackReference(trackRef) && !videoMuted;
  const you = p.isLocal ? " (you)" : "";
  const hasMic = !!p.getTrackPublication(Track.Source.Microphone);

  return (
    <div
      className={cn(
        "relative isolate flex min-h-0 min-w-0 items-center justify-center overflow-hidden rounded-lg bg-ink-900 text-ink-50 transition-shadow duration-160",
        speaking && !screen ? "ring-2 ring-primary ring-offset-0" : "ring-0",
        className,
      )}
      aria-label={`${facts.name}${you}${screen ? ", screen" : ""}`}
    >
      {showVideo && isTrackReference(trackRef) ? (
        <VideoTrack trackRef={trackRef} className={cn("absolute inset-0 size-full", screen ? "object-contain" : "object-cover", p.isLocal && !screen && "-scale-x-100")} />
      ) : (
        <Avatar name={facts.name} src={facts.meta.avatar_url ?? null} size={featured ? "xl" : "lg"} />
      )}
      {facts.hand && !screen && (
        <span className="absolute top-2 left-2 flex h-8 items-center gap-1.5 rounded-pill bg-warning px-3 text-warning-fg" title="Hand raised">
          <Hand className="size-4" aria-hidden />
          <span className="sr-only">Hand raised</span>
        </span>
      )}
      {(quality === ConnectionQuality.Poor || quality === ConnectionQuality.Lost) && (
        <span className="absolute top-2 right-2 flex size-8 items-center justify-center rounded-pill bg-ink-950/70 text-warning" title={quality === ConnectionQuality.Lost ? "Connection lost" : "Weak connection"}>
          {quality === ConnectionQuality.Lost ? <WifiOff className="size-4" aria-hidden /> : <SignalLow className="size-4" aria-hidden />}
          <span className="sr-only">{quality === ConnectionQuality.Lost ? "Connection lost" : "Weak connection"}</span>
        </span>
      )}
      <span className="absolute inset-x-2 bottom-2 flex">
        <span className="flex min-w-0 max-w-full items-center gap-1.5 rounded-sm bg-ink-950/70 px-2 py-1 type-label text-ink-50">
        {screen ? <MonitorUp className="size-4 shrink-0" aria-hidden /> : null}
        {!screen && hasMic && micMuted ? <MicOff className="size-4 shrink-0 text-danger" aria-label="Muted" /> : null}
        <span className="truncate">
          {screen ? `${facts.name}${p.isLocal ? " (you)" : ""} is presenting` : `${facts.name}${you}`}
        </span>
          {!screen && isCallsHostRole(facts.meta.role) && <span className="shrink-0 text-ink-300">· {CALLS_ROLE_LABELS[facts.meta.role]}</span>}
        </span>
      </span>
    </div>
  );
}
