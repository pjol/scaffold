"use client";

import { RoomAudioRenderer, RoomContext, useAudioPlayback, useChat, useConnectionState, useDataChannel, useLocalParticipant, useParticipants, useTracks } from "@livekit/components-react";
import {
  CALLS_REACTION_TOPIC,
  CALLS_SIGNAL_TOPIC,
  callsEndpoints,
  decodeCallsReaction,
  decodeCallsSignal,
  encodeCallsReaction,
  errorMessage,
  formatCallsElapsed,
  isCallsHostRole,
  parseCallsParticipantMeta,
  parseCallsRoomMeta,
  useAdmitCallsLobby,
  useCallsLobby,
  useCallsMeetingRings,
  useCallsMeetingTopic,
  useCallsRoomState,
  useEndCallsMeeting,
  useEndpoints,
  useInvalidateCallsRoom,
  useJoinCallsBreakout,
  useNow,
  useRealtimeEvent,
  useSetCallsGuestHand,
  useSetCallsHand,
  useSetCallsTranscribing,
  useStartCallsRecording,
  useStopCallsRecording,
  useStopCallsRinging,
  type CallsJoinToken,
  type CallsLobbyEntry,
  type CallsReaction,
  type CallsRoomMeta,
} from "@scaffoldapp/shared";
import { Avatar, Banner, Button, ConfirmDialog, ErrorState, IconButton, Sheet, Spinner, toast, useIsDesktop } from "@scaffoldapp/ui-web";
import { ConnectionState, DisconnectReason, LocalAudioTrack, LogLevel, Room, RoomEvent, setLogLevel, Track, VideoPresets, type Participant } from "livekit-client";
import { Copy, Mic, MicOff, PhoneOff, Video, VideoOff, Volume2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { copyMeetingLink } from "../MeetingDialogs";
import { readDevicePrefs, writeDevicePrefs, type CallsDevicePrefs } from "../storage";
import { ActivitiesPanel } from "./Activities";
import { BreakoutBar, BreakoutPrompt } from "./Breakouts";
import { CaptionChoiceDialog, CaptionsOverlay, useCaptions } from "./Captions";
import { Controls, matchShortcut, toggleMedia } from "./Controls";
import { HostSettingsDialog } from "./HostSettings";
import { ChatPanel, PeoplePanel } from "./Panels";
import { PipPortal, pipSupported, usePictureInPicture } from "./pip";
import { ReactionsLayer, type FloatingReaction } from "./Reactions";
import { RecordingConsent, RecordingPills, useRoomMeta } from "./Recording";
import { CallSessionContext, inBreakoutRoom, useCallSession, useRoomTarget, type CallActivity, type CallEnd, type CallLayout, type CallPanel, type CallSession } from "./session";
import { ShortcutsDialog } from "./Shortcuts";
import { Stage } from "./Stage";
import { StatsDialog } from "./Stats";
import { Tile } from "./Tile";

// LiveKit logs connection chatter at info and warns about expected teardown races
// (renegotiation while a removed participant disconnects); keep real errors only.
setLogLevel(LogLevel.error);

/** LiveKit's TrackSource.MICROPHONE in a participant's permissions. */
const SOURCE_MICROPHONE = 2;

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
 * this component's lifetime; LiveKit's hooks read it from RoomContext. A new token (a
 * breakout room, or back to the main room) reconnects, carrying your mic and camera state.
 */
export function CallRoom({ session, prefs, onEnd }: { session: CallSession; prefs: CallsDevicePrefs; onEnd: (why: CallEnd) => void }) {
  const [room, setRoom] = useState<Room | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const onEndRef = useRef(onEnd);
  onEndRef.current = onEnd;
  // Mic and camera as they were when moving between rooms (null on the first connect).
  const carry = useRef<{ mic: boolean; camera: boolean } | null>(null);
  const sessionRef = useRef(session);
  sessionRef.current = session;
  const roomRef = useRef<Room | null>(null);

  // Your mic and camera as you left them. Updated on publish, mute and unmute only: leaving
  // a room unpublishes every track, which isn't you turning them off.
  const media = useRef({ mic: false, camera: false });
  const switchRoom = useCallback((token: CallsJoinToken) => {
    carry.current = { ...media.current };
    sessionRef.current.switchRoom(token);
  }, []);
  const inner = useMemo<CallSession>(() => ({ ...session, switchRoom }), [session, switchRoom]);

  useEffect(() => {
    const p = { ...readDevicePrefs(), ...prefs };
    const r = new Room({
      adaptiveStream: true,
      dynacast: true,
      videoCaptureDefaults: { resolution: VideoPresets.h720.resolution, ...(p.videoInput ? { deviceId: p.videoInput } : {}) },
      audioCaptureDefaults: { echoCancellation: p.echoCancellation, noiseSuppression: p.noiseSuppression, autoGainControl: p.autoGainControl, ...(p.audioInput ? { deviceId: p.audioInput } : {}) },
      ...(p.audioOutput ? { audioOutput: { deviceId: p.audioOutput } } : {}),
      publishDefaults: { simulcast: true },
    });
    roomRef.current = r;
    let live = true;
    const remember = () => {
      if (live) media.current = { mic: r.localParticipant.isMicrophoneEnabled, camera: r.localParticipant.isCameraEnabled };
    };
    r.on(RoomEvent.LocalTrackPublished, remember).on(RoomEvent.TrackMuted, remember).on(RoomEvent.TrackUnmuted, remember);
    const breakout = session.token.room !== session.meetingId;
    r.on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
      if (!live) return;
      // A breakout room closing sends you back to the main room, not out of the call.
      if (breakout && (reason === DisconnectReason.ROOM_DELETED || reason === DisconnectReason.ROOM_CLOSED)) {
        sessionRef.current
          .mainToken()
          .then((t) => switchRoom(t))
          .catch(() => onEndRef.current("ended"));
        return;
      }
      onEndRef.current(endFor(reason));
    });
    setRoom(r);
    setFailed(null);
    // Connect on the next tick: React's development double-mount unmounts at once,
    // and a connection started and aborted in the same tick only adds noise.
    const start = setTimeout(() => {
      r.connect(session.token.url, session.token.token)
        .then(async () => {
          if (!live) return;
          const meta = parseCallsRoomMeta(r.metadata);
          const role = parseCallsParticipantMeta(r.localParticipant.metadata).role;
          const host = isCallsHostRole(role);
          const was = carry.current;
          let mic = was ? was.mic : p.mic;
          if (!was && mic && !host && (meta.rules.mute_on_entry || !meta.rules.unmute)) {
            mic = false;
            toast(meta.rules.unmute ? "You joined muted. Unmute when you want to talk." : "You joined muted. Raise your hand to ask to speak.");
          }
          if (mic) await toggleMedia("mic", (on) => r.localParticipant.setMicrophoneEnabled(on), true);
          if (was ? was.camera : p.camera) await toggleMedia("camera", (on) => r.localParticipant.setCameraEnabled(on), true);
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
      <CallSessionContext.Provider value={inner}>
        <CallUI room={room} />
        <RoomAudioRenderer />
      </CallSessionContext.Provider>
    </RoomContext.Provider>
  );
}

function useFullscreen() {
  const [on, setOn] = useState(false);
  useEffect(() => {
    const f = () => setOn(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", f);
    return () => document.removeEventListener("fullscreenchange", f);
  }, []);
  const toggle = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    else void document.documentElement.requestFullscreen?.().catch(() => toast.error("Your browser didn't go full screen."));
  }, []);
  return { on, toggle };
}

function CallUI({ room }: { room: Room }) {
  const s = useCallSession();
  const target = useRoomTarget();
  const api = useEndpoints(callsEndpoints);
  const desktop = useIsDesktop();
  const state = useConnectionState(room);
  const meta = useRoomMeta();
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled } = useLocalParticipant();
  const participants = useParticipants();
  const [panel, setPanel] = useState<CallPanel>(null);
  const [activity, setActivity] = useState<CallActivity>("polls");
  const [layout, setLayout] = useState<CallLayout>("auto");
  const [settings, setSettings] = useState(false);
  const [ending, setEnding] = useState(false);
  const [stats, setStats] = useState(false);
  const [shortcuts, setShortcuts] = useState(false);
  const [reactions, setReactions] = useState<FloatingReaction[]>([]);
  const [pinned, setPinned] = useState<string | null>(null);
  const [view, setView] = useState(() => readDevicePrefs());
  const nextReaction = useRef(0);
  const breakout = inBreakoutRoom(s);

  // Your role can change mid-call (made co-host); the server updates your metadata.
  const myRole = localParticipant.metadata ? parseCallsParticipantMeta(localParticipant.metadata).role : s.role;
  const isHost = isCallsHostRole(myRole) && !s.guest;
  const myHand = !!localParticipant.attributes["hand"];

  // Whether you may unmute: the rules, or a host let you (your publish permissions).
  const [, setPermTick] = useState(0);
  useEffect(() => {
    const f = (_: unknown, p: Participant) => p.isLocal && setPermTick((n) => n + 1);
    room.on(RoomEvent.ParticipantPermissionsChanged, f);
    return () => {
      room.off(RoomEvent.ParticipantPermissionsChanged, f);
    };
  }, [room]);
  const allowed = localParticipant.permissions?.canPublishSources;
  const canUnmute = isHost || meta.rules.unmute || !allowed || allowed.length === 0 || (allowed as number[]).includes(SOURCE_MICROPHONE);

  useCallsMeetingTopic(s.guest ? null : s.meetingId);
  const lobby = useCallsLobby(s.meetingId, isHost && !breakout);
  const admit = useAdmitCallsLobby(s.meetingId);
  const rings = useCallsMeetingRings(s.meetingId, !s.guest);
  const endCall = useEndCallsMeeting(s.meetingId);
  const stopRinging = useStopCallsRinging();
  const setHand = useSetCallsHand(s.meetingId);
  const setGuestHand = useSetCallsGuestHand();
  const roomState = useCallsRoomState(target);
  const refetchRoom = useInvalidateCallsRoom(target);
  const joinBreakout = useJoinCallsBreakout(target);
  const startRec = useStartCallsRecording(s.meetingId);
  const stopRec = useStopCallsRecording(s.meetingId);
  const transcribe = useSetCallsTranscribing(s.meetingId);
  const [returning, setReturning] = useState(false);

  // Chat lives here so it survives the panel closing; unread counts while it's closed.
  const chat = useChat();
  const [seen, setSeen] = useState(0);
  useEffect(() => {
    if (panel === "chat") setSeen(chat.chatMessages.length);
  }, [panel, chat.chatMessages.length]);
  const unread = panel === "chat" ? 0 : chat.chatMessages.filter((m, i) => i >= seen && !m.from?.isLocal).length;
  const chatAllowed = isHost || meta.rules.chat;
  const sendChat = (text: string) =>
    chat.send(text).then((m) => {
      if (meta.rules.save_chat) void api.room(target).chat(text).catch(() => undefined);
      return m;
    });

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
  const askToSpeak = () => {
    if (breakout) {
      toast("A host has to let you speak.");
      return;
    }
    if (!myHand) toggleHand();
    toast("Your hand is up so a host can let you speak.");
  };

  const leave = () => {
    if (rings.some((r) => r.status === "ringing") && participants.length <= 1 && !s.guest) stopRinging.mutate(s.meetingId);
    void room.disconnect();
  };

  // Server signals (only the server sends on this topic: messages from participants are ignored).
  const knownPolls = useRef<Set<string> | null>(null);
  useDataChannel(CALLS_SIGNAL_TOPIC, (msg) => {
    if (msg.from) return;
    const sig = decodeCallsSignal(msg.payload);
    if (!sig) return;
    switch (sig.type) {
      case "polls":
      case "questions":
      case "breakout":
        refetchRoom();
        break;
      case "broadcast":
        toast(`${sig.from ?? "A host"}: ${sig.text ?? ""}`, { duration: 15_000 });
        break;
      case "ask_unmute":
        toast(`${sig.from ?? "A host"} asks you to unmute`, {
          duration: 10_000,
          action: { label: "Unmute", onClick: () => void toggleMedia("mic", (on) => room.localParticipant.setMicrophoneEnabled(on), true) },
        });
        break;
      case "help":
        if (isHost && sig.room_id) {
          const roomId = sig.room_id;
          const name = meta.breakout?.rooms.find((r) => r.id === roomId)?.name ?? "a breakout room";
          toast(`${sig.from ?? "Someone"} asks for help in ${name}`, {
            duration: 20_000,
            action: { label: "Join", onClick: () => joinBreakout.mutate(roomId, { onSuccess: (t) => s.switchRoom(t), onError: (e) => toast.error(errorMessage(e, "Couldn't join that room.")) }) },
          });
        }
        break;
    }
  });

  // A poll you haven't seen: a toast, and a badge on Polls and Q&A.
  const openPolls = (roomState.data?.polls ?? []).filter((p) => p.status === "open" && p.my_choices.length === 0);
  useEffect(() => {
    if (!roomState.data) return;
    const ids = new Set(roomState.data.polls.filter((p) => p.status === "open").map((p) => p.id));
    if (knownPolls.current && !isHost) {
      for (const p of roomState.data.polls) {
        if (p.status === "open" && !knownPolls.current.has(p.id) && p.my_choices.length === 0 && !(panel === "activities" && activity === "polls")) {
          toast(`New poll: ${p.question}`, { action: { label: "Vote", onClick: () => (setActivity("polls"), setPanel("activities")) } });
        }
      }
    }
    knownPolls.current = ids;
  }, [roomState.data, isHost, panel, activity]);
  const pendingQuestions = isHost ? (roomState.data?.questions ?? []).filter((q) => q.status === "pending").length : 0;
  const activityBadge = panel === "activities" ? 0 : (isHost ? 0 : openPolls.length) + pendingQuestions;

  // Breakout rooms: your assignment in the main room; in a breakout room, back when it closes.
  // The room metadata is pushed and authoritative: a stale assignment for closed rooms is ignored.
  const fetched = roomState.data?.breakout ?? null;
  const mine = fetched && meta.breakout && meta.breakout.id === fetched.id ? { ...fetched, status: meta.breakout.status } : null;
  useEffect(() => {
    refetchRoom();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta.breakout?.id, meta.breakout?.status]);
  const goMain = useCallback(() => {
    setReturning(true);
    s.mainToken()
      .then((t) => s.switchRoom(t))
      .catch((e: unknown) => toast.error(errorMessage(e, "Couldn't get back to the main room.")))
      .finally(() => setReturning(false));
  }, [s]);
  const goBreakout = useCallback(
    (roomId?: string) => joinBreakout.mutate(roomId ?? null, { onSuccess: (t) => s.switchRoom(t), onError: (e) => toast.error(errorMessage(e, "Couldn't join the breakout room.")) }),
    [joinBreakout, s],
  );
  const closingLeft = meta.breakout?.status === "closing" && meta.breakout.closing_at ? Date.parse(meta.breakout.closing_at) - Date.now() : null;
  const now = useNow(1000, breakout);
  const autoMoved = useRef<string | null>(null); // each automatic move is tried once
  useEffect(() => {
    if (!breakout || returning) return;
    const closed = meta.breakout === null && meta.meeting_id !== "";
    const timeUp = closingLeft !== null && closingLeft <= 0;
    const move = closed || timeUp ? "main" : !isHost && mine?.room && mine.room.id !== s.token.room ? mine.room.id : null;
    if (!move || autoMoved.current === `${s.token.room}>${move}`) return;
    autoMoved.current = `${s.token.room}>${move}`;
    if (move === "main") goMain();
    else goBreakout(move); // a host moved you
  }, [breakout, returning, meta.breakout, meta.meeting_id, closingLeft, now, mine, isHost, s.token.room, goMain, goBreakout]);

  // Captions: show everyone's; caption your own speech if you choose to.
  const [captionsOn, setCaptionsOn] = useState(false);
  const [speakOwn, setSpeakOwn] = useState<boolean | null>(null);
  const [asking, setAsking] = useState(false);
  const toggleCaptions = () => {
    if (captionsOn) return setCaptionsOn(false);
    setCaptionsOn(true);
    if (speakOwn === null) setAsking(true);
  };
  const lines = useCaptions({ show: captionsOn, speak: meta.captions && !!speakOwn, transcribing: meta.transcribing, target });
  // While the transcript is saved, ask people who haven't chosen whether to caption themselves.
  useEffect(() => {
    if (meta.transcribing && meta.captions && speakOwn === null) setAsking(true);
  }, [meta.transcribing, meta.captions, speakOwn]);

  // View and microphone sound (remembered on this browser).
  const fullscreen = useFullscreen();
  const pip = usePictureInPicture();
  const setPref = (patch: Partial<CallsDevicePrefs>) =>
    setView((v) => {
      const next = { ...v, ...patch };
      writeDevicePrefs({ ...readDevicePrefs(), ...patch });
      return next;
    });
  const setAudio = (k: "noiseSuppression" | "echoCancellation" | "autoGainControl", on: boolean) => {
    setPref({ [k]: on });
    const constraints = { noiseSuppression: view.noiseSuppression, echoCancellation: view.echoCancellation, autoGainControl: view.autoGainControl, [k]: on };
    room.options.audioCaptureDefaults = { ...room.options.audioCaptureDefaults, ...constraints };
    const track = room.localParticipant.getTrackPublication(Track.Source.Microphone)?.track;
    if (track instanceof LocalAudioTrack) void track.restartTrack({ ...room.options.audioCaptureDefaults, ...constraints }).catch(() => toast.error("Couldn't change the microphone sound."));
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

  // Shortcuts: ⌘/Ctrl+D mic, +E camera; ⌘/Ctrl+Alt+H hand, +C chat, +P people, +A activities, +K captions, +F full screen; ? the list.
  const latest = useRef({ toggleHand, toggleCaptions, isMicrophoneEnabled, isCameraEnabled, panel, canUnmute, askToSpeak, fullscreen, meta });
  latest.current = { toggleHand, toggleCaptions, isMicrophoneEnabled, isCameraEnabled, panel, canUnmute, askToSpeak, fullscreen, meta };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const l = latest.current;
      const lp = room.localParticipant;
      const act = (fn: () => void) => {
        e.preventDefault();
        fn();
      };
      const typing = e.target instanceof HTMLElement && (e.target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(e.target.tagName));
      if (matchShortcut(e, "d")) act(() => (!l.isMicrophoneEnabled && !l.canUnmute ? l.askToSpeak() : void toggleMedia("mic", (on) => lp.setMicrophoneEnabled(on), !l.isMicrophoneEnabled)));
      else if (matchShortcut(e, "e")) act(() => void toggleMedia("camera", (on) => lp.setCameraEnabled(on), !l.isCameraEnabled));
      else if (matchShortcut(e, "h", true)) act(l.toggleHand);
      else if (matchShortcut(e, "c", true)) act(() => setPanel(l.panel === "chat" ? null : "chat"));
      else if (matchShortcut(e, "p", true)) act(() => setPanel(l.panel === "people" ? null : "people"));
      else if (matchShortcut(e, "a", true)) act(() => setPanel(l.panel === "activities" ? null : "activities"));
      else if (matchShortcut(e, "k", true) && l.meta.captions) act(l.toggleCaptions);
      else if (matchShortcut(e, "f", true)) act(l.fullscreen.toggle);
      else if (e.key === "?" && !typing && !e.metaKey && !e.ctrlKey) act(() => setShortcuts(true));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [room]);

  const alone = participants.length <= 1 && !breakout;
  const ringing = rings.filter((r) => r.status === "ringing");
  const panelTitle = panel === "people" ? `People (${participants.length})` : panel === "activities" ? "Polls and Q&A" : "Chat";
  const focus = { pinned, onPin: setPinned, spotlight: meta.spotlight };
  const panelBody =
    panel === "people" ? (
      <PeoplePanel isHost={isHost && !breakout} focus={focus} />
    ) : panel === "chat" ? (
      <ChatPanel messages={chat.chatMessages} send={sendChat} sending={chat.isSending} allowed={chatAllowed} saved={meta.rules.save_chat} />
    ) : panel === "activities" ? (
      <ActivitiesPanel isHost={isHost} tab={activity} onTab={setActivity} />
    ) : null;
  const recordingOn = !!meta.recording;

  return (
    <div className="flex h-dvh flex-col bg-canvas text-fg">
      <CallHeader state={state} meta={meta} />
      {(breakout || (mine && !isHost)) && (
        <div className="px-3 pb-2">
          {breakout ? <BreakoutBar meta={meta} returning={returning} onReturn={goMain} /> : mine && <BreakoutPrompt mine={mine} joining={joinBreakout.isPending} onJoin={goBreakout} />}
        </div>
      )}
      <div className="flex min-h-0 flex-1 gap-3 px-3">
        <Stage
          layout={layout}
          pinned={pinned}
          onPin={setPinned}
          spotlight={meta.spotlight}
          selfView={view.selfView}
          mirror={view.mirror}
          overlay={
            <>
              <ReactionsLayer items={reactions} />
              {captionsOn && <CaptionsOverlay lines={lines} />}
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
          waiting={breakout ? 0 : (lobby.data?.items.length ?? 0)}
          participants={participants.length}
          isHost={isHost}
          handRaised={myHand}
          onToggleHand={toggleHand}
          onReact={react}
          onCopyLink={() => void copyMeetingLink(s.joinUrl)}
          onSettings={() => setSettings(true)}
          onLeave={leave}
          onEndForAll={() => setEnding(true)}
          rules={meta.rules}
          canUnmute={canUnmute}
          onAskToSpeak={askToSpeak}
          inBreakout={breakout}
          activityBadge={activityBadge}
          captions={{ available: meta.captions, on: captionsOn, toggle: toggleCaptions }}
          recording={{
            can: meta.can_record && !breakout,
            on: recordingOn,
            busy: startRec.isPending || stopRec.isPending,
            toggle: () =>
              recordingOn
                ? stopRec.mutate(undefined, { onSuccess: () => toast.success("Recording stopped. It's ready in a few minutes."), onError: (e) => toast.error(errorMessage(e, "Couldn't stop the recording.")) })
                : startRec.mutate(undefined, { onError: (e) => toast.error(errorMessage(e, "Couldn't start recording.")) }),
          }}
          transcript={{
            can: meta.transcripts && !breakout,
            on: meta.transcribing,
            busy: transcribe.isPending,
            toggle: () => transcribe.mutate(!meta.transcribing, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the transcript.")) }),
          }}
          view={{
            selfView: view.selfView,
            mirror: view.mirror,
            fullscreen: fullscreen.on,
            pip: pip.active,
            toggleSelfView: () => setPref({ selfView: !view.selfView }),
            toggleMirror: () => setPref({ mirror: !view.mirror }),
            toggleFullscreen: fullscreen.toggle,
            openPip: pipSupported() ? () => (pip.active ? pip.close() : void pip.open()) : null,
          }}
          audio={{ noiseSuppression: view.noiseSuppression, echoCancellation: view.echoCancellation, autoGainControl: view.autoGainControl, set: setAudio }}
          onStats={() => setStats(true)}
          onShortcuts={() => setShortcuts(true)}
        />
      </div>
      {!desktop && (
        <Sheet open={!!panel} onOpenChange={(o) => !o && setPanel(null)} title={panelTitle}>
          <div className="flex min-h-64 flex-col">{panelBody}</div>
        </Sheet>
      )}
      {isHost && <HostSettingsDialog open={settings} onOpenChange={setSettings} />}
      <RecordingConsent meta={meta} onLeave={leave} />
      <CaptionChoiceDialog
        open={asking}
        onChoose={(speak) => {
          setAsking(false);
          if (speak !== null) setSpeakOwn(speak);
        }}
      />
      <StatsDialog room={room} open={stats} onOpenChange={setStats} />
      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
      <PipPortal win={pip.win}>
        <PipView pinned={pinned} spotlight={meta.spotlight} onLeave={leave} />
      </PipPortal>
      <ConfirmDialog
        open={ending}
        onOpenChange={setEnding}
        title="End the call for everyone?"
        info="Everyone is disconnected, breakout rooms too. The link keeps working for a new call."
        confirmLabel="End for everyone"
        tone="danger"
        onConfirm={() => endCall.mutateAsync()}
        errorText={(e) => errorMessage(e, "Couldn't end the call. Try again.")}
      />
    </div>
  );
}

/** The small always-on-top window: who is speaking (or pinned), with mic, camera and leave. */
function PipView({ pinned, spotlight, onLeave }: { pinned: string | null; spotlight: string; onLeave: () => void }) {
  const refs = useTracks([{ source: Track.Source.Camera, withPlaceholder: true }, { source: Track.Source.ScreenShare, withPlaceholder: false }], { onlySubscribed: false });
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled } = useLocalParticipant();
  const screen = refs.find((r) => r.source === Track.Source.ScreenShare);
  const cams = refs.filter((r) => r.source === Track.Source.Camera);
  const speaking = cams.find((r) => !r.participant.isLocal && r.participant.isSpeaking);
  const pick = screen ?? cams.find((r) => r.participant.identity === pinned) ?? cams.find((r) => r.participant.identity === spotlight) ?? speaking ?? cams.find((r) => !r.participant.isLocal) ?? cams[0];
  return (
    <div className="flex h-dvh flex-col gap-2 bg-canvas p-2">
      {pick ? <Tile trackRef={pick} featured className="min-h-0 flex-1" /> : <div className="flex-1" />}
      <div className="flex justify-center gap-2">
        <IconButton
          label={isMicrophoneEnabled ? "Turn off microphone" : "Turn on microphone"}
          icon={isMicrophoneEnabled ? <Mic /> : <MicOff />}
          variant={isMicrophoneEnabled ? "secondary" : "danger"}
          onClick={() => void toggleMedia("mic", (on) => localParticipant.setMicrophoneEnabled(on), !isMicrophoneEnabled)}
        />
        <IconButton
          label={isCameraEnabled ? "Turn off camera" : "Turn on camera"}
          icon={isCameraEnabled ? <Video /> : <VideoOff />}
          variant={isCameraEnabled ? "secondary" : "danger"}
          onClick={() => void toggleMedia("camera", (on) => localParticipant.setCameraEnabled(on), !isCameraEnabled)}
        />
        <IconButton label="Leave call" icon={<PhoneOff />} variant="danger" onClick={onLeave} />
      </div>
    </div>
  );
}

/** Title, a running timer, connection problems, and a prompt when the browser holds back sound. */
function CallHeader({ state, meta }: { state: ConnectionState; meta: CallsRoomMeta }) {
  const s = useCallSession();
  const [startedAt] = useState(() => Date.now());
  const now = useNow(1000);
  const { canPlayAudio, startAudio } = useAudioPlayback();
  return (
    <div className="pt-safe">
    <div className="flex flex-col gap-2 px-3 pt-3">
      <div className="flex min-h-control items-center gap-3">
        <h1 className="min-w-0 flex-1 truncate type-title-sm text-fg">{s.title}</h1>
        <RecordingPills meta={meta} />
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
