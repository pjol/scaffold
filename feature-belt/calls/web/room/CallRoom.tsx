"use client";

import { RoomAudioRenderer, RoomContext, useAudioPlayback, useChat, useConnectionState, useDataChannel, useLocalParticipant, useParticipants } from "@livekit/components-react";
import {
  CALLS_REACTION_TOPIC,
  decodeCallsReaction,
  encodeCallsReaction,
  errorMessage,
  formatCallsElapsed,
  isCallsHostRole,
  parseCallsParticipantMeta,
  useAdmitCallsLobby,
  useCallsLobby,
  useCallsMeetingRings,
  useCallsMeetingTopic,
  useEndCallsMeeting,
  useNow,
  useRealtimeEvent,
  useSetCallsGuestHand,
  useSetCallsHand,
  useStopCallsRinging,
  type CallsLobbyEntry,
  type CallsReaction,
} from "@scaffoldapp/shared";
import { Avatar, Banner, Button, ConfirmDialog, ErrorState, IconButton, Sheet, Spinner, toast, useIsDesktop } from "@scaffoldapp/ui-web";
import { ConnectionState, DisconnectReason, LogLevel, Room, RoomEvent, setLogLevel, VideoPresets, type Participant } from "livekit-client";
import { Copy, Volume2, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { copyMeetingLink } from "../MeetingDialogs";
import type { CallsDevicePrefs } from "../storage";
import { Controls, matchShortcut, toggleMedia } from "./Controls";
import { HostSettingsDialog } from "./HostSettings";
import { ChatPanel, PeoplePanel } from "./Panels";
import { ReactionsLayer, type FloatingReaction } from "./Reactions";
import { CallSessionContext, useCallSession, type CallEnd, type CallLayout, type CallPanel, type CallSession } from "./session";
import { Stage } from "./Stage";

// LiveKit logs connection chatter at info and warns about expected teardown races
// (renegotiation while a removed participant disconnects); keep real errors only.
setLogLevel(LogLevel.error);

function endFor(reason: DisconnectReason | undefined): CallEnd {
  switch (reason) {
    case DisconnectReason.CLIENT_INITIATED:
      return "left";
    case DisconnectReason.PARTICIPANT_REMOVED:
      return "removed";
    case DisconnectReason.ROOM_DELETED:
    case DisconnectReason.ROOM_CLOSED:
      return "ended";
    case DisconnectReason.DUPLICATE_IDENTITY:
      return "elsewhere";
    default:
      return "failed";
  }
}

/**
 * Connects to the room with the token and renders the call. The Room is created here
 * (not with LiveKit's prefab container) so the UI is ours and connect/disconnect follow
 * this component's lifetime; LiveKit's hooks read it from RoomContext.
 */
export function CallRoom({ session, prefs, onEnd }: { session: CallSession; prefs: CallsDevicePrefs; onEnd: (why: CallEnd) => void }) {
  const [room, setRoom] = useState<Room | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const onEndRef = useRef(onEnd);
  onEndRef.current = onEnd;

  useEffect(() => {
    const r = new Room({
      adaptiveStream: true,
      dynacast: true,
      videoCaptureDefaults: { resolution: VideoPresets.h720.resolution, ...(prefs.videoInput ? { deviceId: prefs.videoInput } : {}) },
      audioCaptureDefaults: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, ...(prefs.audioInput ? { deviceId: prefs.audioInput } : {}) },
      ...(prefs.audioOutput ? { audioOutput: { deviceId: prefs.audioOutput } } : {}),
      publishDefaults: { simulcast: true },
    });
    let live = true;
    r.on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
      if (live) onEndRef.current(endFor(reason));
    });
    setRoom(r);
    setFailed(null);
    // Connect on the next tick: React's development double-mount unmounts at once,
    // and a connection started and aborted in the same tick only adds noise.
    const start = setTimeout(() => {
      r.connect(session.token.url, session.token.token)
        .then(async () => {
          if (!live) return;
          if (prefs.mic) await toggleMedia("mic", (on) => r.localParticipant.setMicrophoneEnabled(on), true);
          if (prefs.camera) await toggleMedia("camera", (on) => r.localParticipant.setCameraEnabled(on), true);
        })
        .catch((err: unknown) => {
          if (live) setFailed(err instanceof Error ? err.message : "Couldn't connect.");
        });
    }, 0);
    return () => {
      live = false;
      clearTimeout(start);
      r.removeAllListeners();
      void r.disconnect();
    };
    // A new token means a new connection; device choices only apply at connect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.token.token]);

  if (failed) {
    return (
      <div className="flex h-dvh items-center justify-center px-gutter">
        <ErrorState title="Couldn't connect to the call" info="Check your connection and try again." onRetry={() => onEndRef.current("failed")} retryLabel="Back" />
      </div>
    );
  }
  if (!room) return null;
  return (
    <RoomContext.Provider value={room}>
      <CallSessionContext.Provider value={session}>
        <CallUI room={room} />
        <RoomAudioRenderer />
      </CallSessionContext.Provider>
    </RoomContext.Provider>
  );
}

function CallUI({ room }: { room: Room }) {
  const s = useCallSession();
  const desktop = useIsDesktop();
  const state = useConnectionState(room);
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled } = useLocalParticipant();
  const participants = useParticipants();
  const [panel, setPanel] = useState<CallPanel>(null);
  const [layout, setLayout] = useState<CallLayout>("auto");
  const [settings, setSettings] = useState(false);
  const [ending, setEnding] = useState(false);
  const [reactions, setReactions] = useState<FloatingReaction[]>([]);
  const nextReaction = useRef(0);

  // Your role can change mid-call (made co-host); the server updates your metadata.
  const myRole = localParticipant.metadata ? parseCallsParticipantMeta(localParticipant.metadata).role : s.role;
  const isHost = isCallsHostRole(myRole) && !s.guest;
  const myHand = !!localParticipant.attributes["hand"];

  useCallsMeetingTopic(s.guest ? null : s.meetingId);
  const lobby = useCallsLobby(s.meetingId, isHost);
  const admit = useAdmitCallsLobby(s.meetingId);
  const rings = useCallsMeetingRings(s.meetingId, !s.guest);
  const endCall = useEndCallsMeeting(s.meetingId);
  const stopRinging = useStopCallsRinging();
  const setHand = useSetCallsHand(s.meetingId);
  const setGuestHand = useSetCallsGuestHand();

  // Chat lives here so it survives the panel closing; unread counts while it's closed.
  const chat = useChat();
  const [seen, setSeen] = useState(0);
  useEffect(() => {
    if (panel === "chat") setSeen(chat.chatMessages.length);
  }, [panel, chat.chatMessages.length]);
  const unread = panel === "chat" ? 0 : chat.chatMessages.filter((m, i) => i >= seen && !m.from?.isLocal).length;

  const float = useCallback((emoji: CallsReaction, name: string) => {
    const id = nextReaction.current++;
    setReactions((rs) => [...rs.slice(-20), { id, emoji, name, x: Math.random() }]);
    setTimeout(() => setReactions((rs) => rs.filter((r) => r.id !== id)), 3200);
  }, []);
  const { send: sendData } = useDataChannel(CALLS_REACTION_TOPIC, (msg) => {
    const emoji = decodeCallsReaction(msg.payload);
    if (emoji) float(emoji, msg.from?.name || "Someone");
  });
  const react = (emoji: CallsReaction) => {
    float(emoji, "You");
    void sendData(encodeCallsReaction(emoji), { reliable: true }).catch(() => undefined);
  };

  const toggleHand = () => {
    const raised = !myHand;
    const onError = (e: unknown) => toast.error(errorMessage(e, "Couldn't change your hand."));
    if (s.guest) setGuestHand.mutate({ lobbyId: s.guest.lobbyId, key: s.guest.key, raised }, { onError });
    else setHand.mutate({ raised }, { onError });
  };

  const leave = () => {
    if (rings.some((r) => r.status === "ringing") && participants.length <= 1 && !s.guest) stopRinging.mutate(s.meetingId);
    void room.disconnect();
  };

  // Hosts hear about knocks and raised hands even with the panel closed, and on joining,
  // about anyone already waiting.
  useRealtimeEvent("calls.lobby", (e) => {
    if (!isHost || e.data.meeting_id !== s.meetingId || e.data.reason !== "knock") return;
    toast(`${e.data.entry.name} wants to join`, { action: { label: "Review", onClick: () => setPanel("people") } });
  });
  const waitingNow = isHost ? (lobby.data?.items ?? []) : [];
  const toldWaiting = useRef(false);
  useEffect(() => {
    if (!isHost || !lobby.data || toldWaiting.current) return;
    toldWaiting.current = true;
    const n = lobby.data.items.length;
    if (n > 0) toast(n === 1 ? `${lobby.data.items[0]?.name ?? "Someone"} is waiting to join` : `${n} people are waiting to join`, { action: { label: "Review", onClick: () => setPanel("people") } });
  }, [isHost, lobby.data]);
  const admitWaiting = (ids: string[] | "all") =>
    admit.mutate(ids === "all" ? { all: true } : { ids }, { onError: (e) => toast.error(errorMessage(e, "Couldn't let them in. Try again.")) });
  useEffect(() => {
    if (!isHost) return undefined;
    const onAttrs = (changed: Record<string, string>, p: Participant) => {
      if (changed["hand"] && !p.isLocal) toast(`${p.name || "Someone"} raised their hand`);
    };
    room.on(RoomEvent.ParticipantAttributesChanged, onAttrs);
    return () => {
      room.off(RoomEvent.ParticipantAttributesChanged, onAttrs);
    };
  }, [room, isHost]);

  // Shortcuts: ⌘/Ctrl+D mic, +E camera, ⌘/Ctrl+Alt+H hand, +C chat, +P people.
  const latest = useRef({ toggleHand, isMicrophoneEnabled, isCameraEnabled, panel });
  latest.current = { toggleHand, isMicrophoneEnabled, isCameraEnabled, panel };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const l = latest.current;
      const lp = room.localParticipant;
      const act = (fn: () => void) => {
        e.preventDefault();
        fn();
      };
      if (matchShortcut(e, "d")) act(() => void toggleMedia("mic", (on) => lp.setMicrophoneEnabled(on), !l.isMicrophoneEnabled));
      else if (matchShortcut(e, "e")) act(() => void toggleMedia("camera", (on) => lp.setCameraEnabled(on), !l.isCameraEnabled));
      else if (matchShortcut(e, "h", true)) act(l.toggleHand);
      else if (matchShortcut(e, "c", true)) act(() => setPanel(l.panel === "chat" ? null : "chat"));
      else if (matchShortcut(e, "p", true)) act(() => setPanel(l.panel === "people" ? null : "people"));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [room]);

  const alone = participants.length <= 1;
  const ringing = rings.filter((r) => r.status === "ringing");
  const panelTitle = panel === "people" ? `People (${participants.length})` : "Chat";
  const panelBody = panel === "people" ? <PeoplePanel isHost={isHost} /> : panel === "chat" ? <ChatPanel messages={chat.chatMessages} send={(t) => chat.send(t)} sending={chat.isSending} /> : null;

  return (
    <div className="flex h-dvh flex-col bg-canvas text-fg">
      <CallHeader state={state} />
      <div className="flex min-h-0 flex-1 gap-3 px-3">
        <Stage
          layout={layout}
          onShowPeople={() => setPanel("people")}
          overlay={
            <>
              <ReactionsLayer items={reactions} />
              {alone && (
                <AloneCard
                  ringing={ringing.map((r) => r.callee)}
                  waiting={waitingNow}
                  admitting={admit.isPending}
                  onAdmit={admitWaiting}
                  onReview={() => setPanel("people")}
                  onCopy={() => void copyMeetingLink(s.joinUrl)}
                />
              )}
            </>
          }
        />
        {desktop && panel && (
          <aside className="flex w-96 shrink-0 flex-col gap-4 overflow-hidden rounded-lg border border-border bg-surface p-4" aria-label={panelTitle}>
            <div className="flex items-center gap-2">
              <h2 className="flex-1 type-title-sm text-fg">{panelTitle}</h2>
              <IconButton label="Close panel" icon={<X />} variant="ghost" size="sm" onClick={() => setPanel(null)} />
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">{panelBody}</div>
          </aside>
        )}
      </div>
      <div className="pb-safe">
        <Controls
          panel={panel}
          onPanel={setPanel}
          layout={layout}
          onLayout={setLayout}
          unreadChat={unread}
          waiting={lobby.data?.items.length ?? 0}
          participants={participants.length}
          isHost={isHost}
          handRaised={myHand}
          onToggleHand={toggleHand}
          onReact={react}
          onCopyLink={() => void copyMeetingLink(s.joinUrl)}
          onSettings={() => setSettings(true)}
          onLeave={leave}
          onEndForAll={() => setEnding(true)}
        />
      </div>
      {!desktop && (
        <Sheet open={!!panel} onOpenChange={(o) => !o && setPanel(null)} title={panelTitle}>
          <div className="flex min-h-64 flex-col">{panelBody}</div>
        </Sheet>
      )}
      {isHost && <HostSettingsDialog open={settings} onOpenChange={setSettings} />}
      <ConfirmDialog
        open={ending}
        onOpenChange={setEnding}
        title="End the call for everyone?"
        info="Everyone is disconnected. The link keeps working for a new call."
        confirmLabel="End for everyone"
        tone="danger"
        onConfirm={() => endCall.mutateAsync()}
        errorText={(e) => errorMessage(e, "Couldn't end the call. Try again.")}
      />
    </div>
  );
}

/** Title, a running timer, connection problems, and a prompt when the browser holds back sound. */
function CallHeader({ state }: { state: ConnectionState }) {
  const s = useCallSession();
  const [startedAt] = useState(() => Date.now());
  const now = useNow(1000);
  const { canPlayAudio, startAudio } = useAudioPlayback();
  return (
    <div className="pt-safe">
    <div className="flex flex-col gap-2 px-3 pt-3">
      <div className="flex min-h-control items-center gap-3">
        <h1 className="min-w-0 flex-1 truncate type-title-sm text-fg">{s.title}</h1>
        <span className="shrink-0 type-label tabular-nums text-fg-muted">{formatCallsElapsed((now - startedAt) / 1000)}</span>
        <Button variant="ghost" size="sm" leftIcon={<Copy />} className="hidden sm:inline-flex" onClick={() => void copyMeetingLink(s.joinUrl)}>
          {s.code}
        </Button>
      </div>
      {state === ConnectionState.Reconnecting || state === ConnectionState.SignalReconnecting ? (
        <Banner tone="warning" icon={<Spinner size={16} />}>
          Reconnecting… Your connection dropped; the call continues when it's back.
        </Banner>
      ) : null}
      {!canPlayAudio && (
        <Banner tone="info" icon={<Volume2 aria-hidden />} action={<Button size="sm" variant="soft" onClick={() => void startAudio()}>Turn on sound</Button>}>
          Your browser paused the call's sound.
        </Banner>
      )}
    </div>
    </div>
  );
}

/** When you're the only one here: who's waiting to join (hosts), who's being rung, or the link to share. */
function AloneCard({
  ringing,
  waiting,
  admitting,
  onAdmit,
  onReview,
  onCopy,
}: {
  ringing: { id: string; display_name: string; avatar_url: string | null }[];
  waiting: CallsLobbyEntry[];
  admitting: boolean;
  onAdmit: (ids: string[] | "all") => void;
  onReview: () => void;
  onCopy: () => void;
}) {
  const s = useCallSession();
  const first = waiting[0];
  return (
    // Below the toasts, which open at the top too.
    <div className="pointer-events-none absolute inset-x-0 top-20 flex justify-center px-4">
      <div className="pointer-events-auto flex w-full max-w-sm flex-col items-center gap-4 rounded-xl border border-border bg-surface-raised p-6 text-center shadow-lg">
        {first ? (
          <>
            <div className="flex -space-x-3">
              {waiting.slice(0, 4).map((e) => (
                <Avatar key={e.id} name={e.name} src={e.user?.avatar_url ?? null} size="lg" ring />
              ))}
            </div>
            <p className="type-title-sm text-fg" role="status">
              {waiting.length === 1 ? `${first.name} is waiting to join` : `${waiting.length} waiting to join`}
            </p>
            <div className="flex flex-wrap justify-center gap-3">
              <Button variant="secondary" onClick={onReview}>
                Review
              </Button>
              <Button loading={admitting} onClick={() => onAdmit(waiting.length === 1 ? [first.id] : "all")}>
                {waiting.length === 1 ? "Admit" : "Admit all"}
              </Button>
            </div>
          </>
        ) : ringing.length > 0 ? (
          <>
            <div className="flex -space-x-3">
              {ringing.slice(0, 4).map((u) => (
                <Avatar key={u.id} name={u.display_name} src={u.avatar_url} size="lg" ring />
              ))}
            </div>
            <p className="type-title-sm text-fg">
              Calling {ringing.length === 1 ? ringing[0]?.display_name : `${ringing.length} people`}…
            </p>
          </>
        ) : (
          <>
            <p className="type-title-sm text-fg">You're the only one here</p>
            {s.kind === "meeting" && (
              <Button variant="secondary" leftIcon={<Copy />} onClick={onCopy}>
                Copy the link to invite people
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  );
}
