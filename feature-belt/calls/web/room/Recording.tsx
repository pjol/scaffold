"use client";

import { parseCallsRoomMeta, type CallsRoomMeta } from "@scaffoldapp/shared";
import { Button, Dialog } from "@scaffoldapp/ui-web";
import { useRoomInfo } from "@livekit/components-react";
import { Captions, Circle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

/** The room's server-owned metadata (recording, spotlight, rules, breakout rooms). */
export function useRoomMeta(): CallsRoomMeta {
  const info = useRoomInfo();
  return useMemo(() => parseCallsRoomMeta(info.metadata), [info.metadata]);
}

/** Header pills: recording and transcript in progress. */
export function RecordingPills({ meta }: { meta: CallsRoomMeta }) {
  if (!meta.recording && !meta.transcribing) return null;
  return (
    <span className="flex shrink-0 items-center gap-2" role="status">
      {meta.recording && (
        <span className="flex h-7 items-center gap-1.5 rounded-pill bg-danger px-3 type-label font-semibold text-danger-fg">
          <Circle className="size-2.5 fill-current motion-safe:animate-pulse" aria-hidden />
          {meta.recording.status === "starting" ? "Starting recording" : "Recording"}
        </span>
      )}
      {meta.transcribing && (
        <span className="flex h-7 items-center gap-1.5 rounded-pill bg-warning-soft px-3 type-label font-semibold text-warning-strong">
          <Captions className="size-4" aria-hidden />
          <span className="hidden sm:inline">Transcript on</span>
          <span className="sm:hidden">Transcript</span>
        </span>
      )}
    </span>
  );
}

/**
 * Tells everyone in the call when it starts being recorded or transcribed (and anyone who
 * joins while it is), once per recording: staying is agreeing, Leave ends their part.
 */
// Acknowledged for this page (moving to a breakout room and back doesn't ask again).
const acknowledged = new Set<string>();

export function RecordingConsent({ meta, onLeave }: { meta: CallsRoomMeta; onLeave: () => void }) {
  const [, setTick] = useState(0);
  const [seenTranscript, setSeenTranscript] = useState(() => acknowledged.has(`transcript:${meta.meeting_id}`));
  const recording = meta.recording && !acknowledged.has(meta.recording.id) ? meta.recording : null;
  const transcript = meta.transcribing && !seenTranscript;
  useEffect(() => {
    if (!meta.transcribing) {
      acknowledged.delete(`transcript:${meta.meeting_id}`); // a later transcript asks again
      setSeenTranscript(false);
    }
  }, [meta.transcribing, meta.meeting_id]);
  if (!recording && !transcript) return null;
  const title = recording && transcript ? "This call is being recorded and transcribed" : recording ? "This call is being recorded" : "This call's transcript is being saved";
  const ack = () => {
    if (meta.recording) acknowledged.add(meta.recording.id);
    if (meta.transcribing) {
      acknowledged.add(`transcript:${meta.meeting_id}`);
      setSeenTranscript(true);
    }
    setTick((n) => n + 1);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && ack()}
      title={title}
      size="sm"
      info="Hosts decide who can open recordings and transcripts afterwards: people in the call, invitees, or everyone in the workspace."
      footer={
        <>
          <Button variant="ghost" onClick={onLeave}>
            Leave call
          </Button>
          <Button onClick={ack}>Got it</Button>
        </>
      }
    >
      <p className="type-body text-fg">
        {recording ? "Everyone's video, audio and shared screens are recorded. " : ""}
        {transcript ? "Captions people turn on for their own speech are saved as text. " : ""}
        Staying in the call means you agree.
      </p>
    </Dialog>
  );
}
