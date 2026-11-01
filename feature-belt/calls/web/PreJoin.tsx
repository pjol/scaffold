"use client";

import { useMediaDevices, usePreviewTracks } from "@livekit/components-react";
import { Avatar, Badge, Banner, Button, Field, IconButton, Input, Select } from "@scaffoldapp/ui-web";
import { createAudioAnalyser, Track, type LocalAudioTrack, type LocalVideoTrack } from "livekit-client";
import { Mic, MicOff, Video, VideoOff } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { mediaProblem, mediaProblemText, type CallsMediaProblem } from "./media";
import { readDevicePrefs, writeDevicePrefs, type CallsDevicePrefs } from "./storage";

export interface PreJoinProps {
  title: string;
  /** "Join now" or "Ask to join". */
  action: "join" | "knock";
  live?: boolean;
  /** Guests type a name; people with accounts don't. */
  name?: { value: string; onChange: (v: string) => void };
  /** Shown on the preview when the camera is off. */
  displayName: string;
  avatarUrl?: string | null;
  busy?: boolean;
  problem?: ReactNode;
  onJoin: (prefs: CallsDevicePrefs) => void;
  /** Extra content under the button (a "Sign in instead" link). */
  footer?: ReactNode;
}

/**
 * Before joining: a camera preview with a microphone level, camera and mic toggles, device
 * pickers, and the join button. Choices are remembered on this browser. A blocked or
 * missing device says so and the person can still join without it.
 */
export function PreJoin({ title, action, live, name, displayName, avatarUrl, busy, problem, onJoin, footer }: PreJoinProps) {
  const [prefs, setPrefs] = useState<CallsDevicePrefs>(() => readDevicePrefs());
  const [camProblem, setCamProblem] = useState<CallsMediaProblem | null>(null);
  const [micProblem, setMicProblem] = useState<CallsMediaProblem | null>(null);
  const update = (patch: Partial<CallsDevicePrefs>) =>
    setPrefs((p) => {
      const next = { ...p, ...patch };
      writeDevicePrefs(next);
      return next;
    });

  const options = useMemo(
    () => ({
      audio: prefs.mic ? (prefs.audioInput ? { deviceId: prefs.audioInput } : true) : false,
      video: prefs.camera ? (prefs.videoInput ? { deviceId: prefs.videoInput } : true) : false,
    }),
    [prefs.mic, prefs.camera, prefs.audioInput, prefs.videoInput],
  );
  const tracks = usePreviewTracks(options, (err) => {
    const p = mediaProblem(err);
    const msg = err.message.toLowerCase();
    // usePreviewTracks asks for both at once; attribute the error to what was asked for.
    if (prefs.camera && (msg.includes("video") || !prefs.mic)) setCamProblem(p);
    else if (prefs.mic) setMicProblem(p);
  });
  const video = tracks?.find((t) => t.kind === Track.Kind.Video) as LocalVideoTrack | undefined;
  const audio = tracks?.find((t) => t.kind === Track.Kind.Audio) as LocalAudioTrack | undefined;
  useEffect(() => {
    if (video) setCamProblem(null);
    if (audio) setMicProblem(null);
  }, [video, audio]);

  const granted = !!video || !!audio;
  const cameras = useMediaDevices({ kind: "videoinput" });
  const mics = useMediaDevices({ kind: "audioinput" });
  const named = (list: MediaDeviceInfo[], fallback: string) =>
    list.filter((d) => d.deviceId).map((d, i) => ({ value: d.deviceId, label: d.label || `${fallback} ${i + 1}` }));

  const submit = (e: FormEvent) => {
    e.preventDefault();
    onJoin({ ...prefs, camera: prefs.camera && camProblem === null, mic: prefs.mic && micProblem === null });
  };
  const needsName = !!name && !name.value.trim();

  return (
    <div className="mx-auto grid w-full max-w-content items-center gap-section px-gutter py-section lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <div className="flex flex-col gap-3">
        <div className="relative aspect-video w-full overflow-hidden rounded-xl bg-ink-900">
          {video ? (
            <PreviewVideo track={video} />
          ) : (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-ink-100">
              <Avatar name={displayName || "You"} src={avatarUrl ?? null} size="xl" />
              <span className="type-label">{prefs.camera ? (camProblem ? "Camera unavailable" : "Starting camera…") : "Camera is off"}</span>
            </div>
          )}
          <div className="absolute inset-x-0 bottom-0 flex items-center justify-center gap-3 p-4">
            <IconButton
              label={prefs.mic ? "Turn off microphone" : "Turn on microphone"}
              icon={prefs.mic ? <Mic /> : <MicOff />}
              variant={prefs.mic ? "secondary" : "danger"}
              size="lg"
              shape="circle"
              tooltip
              className={prefs.mic ? "bg-ink-50/90 text-ink-950 hover:bg-ink-50" : "bg-danger text-danger-fg hover:bg-danger"}
              onClick={() => (setMicProblem(null), update({ mic: !prefs.mic }))}
            />
            <IconButton
              label={prefs.camera ? "Turn off camera" : "Turn on camera"}
              icon={prefs.camera ? <Video /> : <VideoOff />}
              variant={prefs.camera ? "secondary" : "danger"}
              size="lg"
              shape="circle"
              tooltip
              className={prefs.camera ? "bg-ink-50/90 text-ink-950 hover:bg-ink-50" : "bg-danger text-danger-fg hover:bg-danger"}
              onClick={() => (setCamProblem(null), update({ camera: !prefs.camera }))}
            />
          </div>
          {audio && <MicLevel track={audio} />}
        </div>
        {camProblem && <Banner tone="warning">{mediaProblemText("camera", camProblem)}</Banner>}
        {micProblem && <Banner tone="warning">{mediaProblemText("microphone", micProblem)}</Banner>}
        {granted && (
          <div className="grid gap-3 sm:grid-cols-2">
            {mics.length > 0 && (
              <Field label="Microphone">
                <Select value={audio?.mediaStreamTrack.getSettings().deviceId ?? prefs.audioInput ?? ""} onValueChange={(v) => update({ audioInput: v })} options={named(mics, "Microphone")} />
              </Field>
            )}
            {cameras.length > 0 && (
              <Field label="Camera">
                <Select value={video?.mediaStreamTrack.getSettings().deviceId ?? prefs.videoInput ?? ""} onValueChange={(v) => update({ videoInput: v })} options={named(cameras, "Camera")} />
              </Field>
            )}
          </div>
        )}
      </div>
      <form onSubmit={submit} className="flex flex-col items-stretch gap-6 lg:items-start">
        <div className="flex flex-col gap-2">
          <h1 className="flex flex-wrap items-center gap-3 type-headline text-fg">
            <span className="min-w-0 break-words">{title}</span>
            {live && (
              <Badge tone="success" dot>
                Live
              </Badge>
            )}
          </h1>
        </div>
        {name && (
          <Field label="Your name" required>
            <Input value={name.value} onChange={(e) => name.onChange(e.target.value)} maxLength={60} autoComplete="name" placeholder="Alex Morgan" autoFocus />
          </Field>
        )}
        {problem}
        <Button type="submit" size="lg" loading={busy} disabled={needsName} className="lg:min-w-48">
          {action === "join" ? "Join now" : "Ask to join"}
        </Button>
        {footer}
      </form>
    </div>
  );
}

function PreviewVideo({ track }: { track: LocalVideoTrack }) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    track.attach(el);
    return () => {
      track.detach(el);
    };
  }, [track]);
  return <video ref={ref} className="absolute inset-0 size-full -scale-x-100 object-cover" muted playsInline autoPlay aria-label="Your camera preview" />;
}

/** A small live level so people see their microphone works. */
function MicLevel({ track }: { track: LocalAudioTrack }) {
  const [level, setLevel] = useState(0);
  useEffect(() => {
    const { calculateVolume, cleanup } = createAudioAnalyser(track, { cloneTrack: true });
    const t = setInterval(() => setLevel(calculateVolume()), 120);
    return () => {
      clearInterval(t);
      void cleanup();
    };
  }, [track]);
  const bars = [
    { at: 0.05, h: "h-1" },
    { at: 0.12, h: "h-2" },
    { at: 0.2, h: "h-3" },
    { at: 0.3, h: "h-4" },
  ];
  return (
    <div className="absolute top-3 left-3 flex h-6 items-end gap-0.5 rounded-sm bg-ink-950/60 px-1.5 py-1" role="meter" aria-label="Microphone level" aria-valuemin={0} aria-valuemax={1} aria-valuenow={Math.round(level * 100) / 100}>
      {bars.map((b) => (
        <span key={b.at} className={`w-1 rounded-xs ${b.h} ${level > b.at ? "bg-success" : "bg-ink-400"}`} />
      ))}
    </div>
  );
}
