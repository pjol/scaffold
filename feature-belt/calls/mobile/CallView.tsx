import { useChat, useDataChannel, useParticipantAttributes } from "@livekit/components-react";
import { AudioSession, isTrackReference, registerGlobals, RoomContext, useConnectionState, useIsMuted, useIsSpeaking, useLocalParticipant, useParticipantInfo, useParticipants, useTracks, VideoTrack, type TrackReferenceOrPlaceholder } from "@livekit/react-native";
import {
  CALLS_ROLE_LABELS,
  CALLS_SIGNAL_TOPIC,
  callsEndpoints,
  callsHandRaisedAt,
  decodeCallsSignal,
  errorMessage,
  parseCallsRoomMeta,
  useCallsRoomState,
  useEndpoints,
  useInvalidateCallsRoom,
  useJoinCallsBreakout,
  isCallsHostRole,
  parseCallsParticipantMeta,
  useAdmitCallsLobby,
  useCallsLobby,
  useCallsMeetingRings,
  useCallsMeetingTopic,
  useDenyCallsLobby,
  useMuteCallsParticipant,
  useRemoveCallsParticipant,
  useSetCallsHand,
  useStopCallsRinging,
  type CallsJoinToken,
  type CallsRole,
} from "@scaffoldapp/shared";
import { Avatar, Banner, Button, IconButton, Input, ListGroup, ListRow, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { ConnectionState, DisconnectReason, LogLevel, Room, RoomEvent, setLogLevel, Track, type LocalVideoTrack, type Participant } from "livekit-client";
import { BarChart3, Captions, Hand, MessageSquare, Mic, MicOff, MoreHorizontal, PhoneOff, SendHorizontal, SwitchCamera, UserMinus, Users, Video, VideoOff } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { ActivitiesSheet, BreakoutBanner, CaptionsOverlay, RecordingConsent, RecordingPills, useCaptionLines, useRoomMeta } from "./CallExtras";
import { FlatList, Linking, ScrollView, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

// WebRTC for livekit-client on React Native (native module: dev builds only).
registerGlobals();
setLogLevel(LogLevel.error);

export type MobileCallEnd = "left" | "ended" | "removed" | "elsewhere" | "failed";

function endFor(reason?: DisconnectReason): MobileCallEnd {
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

async function setMedia(kind: "mic" | "camera", fn: () => Promise<unknown>) {
  try {
    await fn();
  } catch {
    toast.error(kind === "mic" ? "Microphone access is off." : "Camera access is off.", { action: { label: "Settings", onPress: () => void Linking.openSettings() } });
  }
}

/** LiveKit's TrackSource.MICROPHONE in a participant's permissions. */
const SOURCE_MICROPHONE = 2;

/**
 * The call: video tiles, controls, people (with host controls and the lobby), chat, polls and
 * Q&A, captions from people who caption themselves, the recording notice, and breakout rooms
 * (a new token reconnects to that room, carrying your mic and camera state).
 */
export function CallView({
  token,
  meetingId,
  title,
  camera,
  mic,
  onEnd,
  onSwitch,
  mainToken,
}: {
  token: CallsJoinToken;
  meetingId: string;
  title: string;
  camera: boolean;
  mic: boolean;
  onEnd: (why: MobileCallEnd) => void;
  /** Move to another room of this meeting (breakout or main) with this mic/camera state. */
  onSwitch: (token: CallsJoinToken, state: { mic: boolean; camera: boolean }) => void;
  mainToken: () => Promise<CallsJoinToken>;
}) {
  const [room, setRoom] = useState<Room | null>(null);
  const theme = useTheme();
  const [failed, setFailed] = useState(false);
  const endRef = useRef(onEnd);
  endRef.current = onEnd;
  const switchRef = useRef(onSwitch);
  switchRef.current = onSwitch;
  const mainRef = useRef(mainToken);
  mainRef.current = mainToken;

  // Mic and camera as you left them: updated on publish, mute and unmute (leaving a room unpublishes everything).
  const media = useRef({ mic, camera });
  useEffect(() => {
    const r = new Room({ adaptiveStream: true, dynacast: true });
    let live = true;
    const breakout = token.room !== meetingId;
    const remember = () => {
      if (live) media.current = { mic: r.localParticipant.isMicrophoneEnabled, camera: r.localParticipant.isCameraEnabled };
    };
    r.on(RoomEvent.LocalTrackPublished, remember).on(RoomEvent.TrackMuted, remember).on(RoomEvent.TrackUnmuted, remember);
    r.on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
      if (!live) return;
      if (breakout && (reason === DisconnectReason.ROOM_DELETED || reason === DisconnectReason.ROOM_CLOSED)) {
        mainRef
          .current()
          .then((t) => switchRef.current(t, { ...media.current }))
          .catch(() => endRef.current("ended"));
        return;
      }
      endRef.current(endFor(reason));
    });
    setRoom(r);
    const start = setTimeout(async () => {
      await AudioSession.startAudioSession();
      try {
        await r.connect(token.url, token.token);
        if (!live) return;
        const meta = parseCallsRoomMeta(r.metadata);
        const host = isCallsHostRole(parseCallsParticipantMeta(r.localParticipant.metadata).role);
        let micOn = mic;
        if (micOn && !host && !breakout && (meta.rules.mute_on_entry || !meta.rules.unmute)) {
          micOn = false;
          toast(meta.rules.unmute ? "You joined muted." : "You joined muted. Raise your hand to ask to speak.");
        }
        if (micOn) await setMedia("mic", () => r.localParticipant.setMicrophoneEnabled(true));
        if (camera) await setMedia("camera", () => r.localParticipant.setCameraEnabled(true));
      } catch {
        if (live) setFailed(true);
      }
    }, 0);
    return () => {
      live = false;
      clearTimeout(start);
      r.removeAllListeners();
      void r.disconnect();
      void AudioSession.stopAudioSession();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token.token]);

  if (failed) {
    return (
      <View style={{ flex: 1, justifyContent: "center", padding: theme.space[6] }}>
        <Banner tone="danger" title="Couldn't connect to the call." action={{ label: "Back", onPress: () => endRef.current("failed") }} />
      </View>
    );
  }
  if (!room) return null;
  return (
    <RoomContext.Provider value={room}>
      <CallUI
        room={room}
        meetingId={meetingId}
        title={title}
        role={token.role}
        currentRoom={token.room}
        onSwitch={(t) => onSwitch(t, { ...media.current })}
        mainToken={mainToken}
      />
    </RoomContext.Provider>
  );
}

function CallUI({
  room,
  meetingId,
  title,
  role,
  currentRoom,
  onSwitch,
  mainToken,
}: {
  room: Room;
  meetingId: string;
  title: string;
  role: CallsRole;
  currentRoom: string;
  onSwitch: (t: CallsJoinToken) => void;
  mainToken: () => Promise<CallsJoinToken>;
}) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const state = useConnectionState(room);
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled } = useLocalParticipant();
  const participants = useParticipants();
  const tracks = useTracks([{ source: Track.Source.Camera, withPlaceholder: true }], { onlySubscribed: false });
  const [panel, setPanel] = useState<"people" | "chat" | "activities" | "more" | null>(null);
  // Your role can change mid-call (made co-host); the server updates your metadata.
  const isHost = isCallsHostRole(localParticipant.metadata ? parseCallsParticipantMeta(localParticipant.metadata).role : role);
  const myHand = !!localParticipant.attributes["hand"];
  const hand = useSetCallsHand(meetingId);
  const stop = useStopCallsRinging();
  const rings = useCallsMeetingRings(meetingId);
  const lobby = useCallsLobby(meetingId, isHost);
  const admit = useAdmitCallsLobby(meetingId);
  const chat = useChat();
  useCallsMeetingTopic(meetingId);
  const meta = useRoomMeta();
  const api = useEndpoints(callsEndpoints);
  const target = useMemo(() => ({ meetingId }), [meetingId]);
  const roomState = useCallsRoomState(target);
  const refetchRoom = useInvalidateCallsRoom(target);
  const joinBreakout = useJoinCallsBreakout(target);
  const inBreakout = currentRoom !== meetingId;
  const [captions, setCaptions] = useState(false);
  const lines = useCaptionLines(captions);
  const allowed = localParticipant.permissions?.canPublishSources as number[] | undefined;
  const canUnmute = isHost || meta.rules.unmute || !allowed || allowed.length === 0 || allowed.includes(SOURCE_MICROPHONE);
  const chatAllowed = isHost || meta.rules.chat;
  const [returning, setReturning] = useState(false);
  const goMain = () => {
    setReturning(true);
    mainToken()
      .then(onSwitch)
      .catch((e: unknown) => toast.error(errorMessage(e, "Couldn't get back to the main room.")))
      .finally(() => setReturning(false));
  };
  const goBreakout = (roomId?: string) =>
    joinBreakout.mutate(roomId ?? null, { onSuccess: onSwitch, onError: (e) => toast.error(errorMessage(e, "Couldn't join the breakout room.")) });
  // Back to the main room when the rooms close (the countdown ran out or a host closed them).
  const triedMain = useRef(false);
  useEffect(() => {
    triedMain.current = false;
  }, [currentRoom]);
  const closingAt = meta.breakout?.status === "closing" ? meta.breakout.closing_at : null;
  useEffect(() => {
    if (!inBreakout || returning) return undefined;
    if (meta.meeting_id !== "" && meta.breakout === null) {
      if (!triedMain.current) {
        triedMain.current = true; // once: a failure leaves the button
        goMain();
      }
      return undefined;
    }
    if (!closingAt) return undefined;
    const t = setTimeout(goMain, Math.max(0, Date.parse(closingAt) - Date.now()));
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inBreakout, returning, meta.breakout, meta.meeting_id, closingAt]);
  // Server signals only (participants can't send on this topic as the server).
  useDataChannel(CALLS_SIGNAL_TOPIC, (msg) => {
    if (msg.from) return;
    const sig = decodeCallsSignal(msg.payload);
    if (!sig) return;
    if (sig.type === "polls" || sig.type === "questions" || sig.type === "breakout") refetchRoom();
    else if (sig.type === "broadcast") toast(`${sig.from ?? "A host"}: ${sig.text ?? ""}`);
    else if (sig.type === "ask_unmute") toast(`${sig.from ?? "A host"} asks you to unmute`, { action: { label: "Unmute", onPress: () => void setMedia("mic", () => room.localParticipant.setMicrophoneEnabled(true)) } });
  });
  useEffect(() => {
    refetchRoom();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta.breakout?.id, meta.breakout?.status]);
  const openPolls = (roomState.data?.polls ?? []).filter((p) => p.status === "open" && p.my_choices.length === 0).length;
  const sendChat = (text: string) =>
    chat.send(text).then((m) => {
      if (meta.rules.save_chat) void api.room(target).chat(text).catch(() => undefined);
      return m;
    });

  // On joining, hosts hear about anyone already waiting.
  const waiting = isHost ? (lobby.data?.items ?? []) : [];
  const toldWaiting = useRef(false);
  useEffect(() => {
    if (!isHost || !lobby.data || toldWaiting.current) return;
    toldWaiting.current = true;
    const n = lobby.data.items.length;
    if (n > 0) toast(n === 1 ? `${lobby.data.items[0]?.name ?? "Someone"} is waiting to join` : `${n} people are waiting to join`, { action: { label: "Review", onPress: () => setPanel("people") } });
  }, [isHost, lobby.data]);
  const admitWaiting = () =>
    admit.mutate(waiting.length === 1 && waiting[0] ? { ids: [waiting[0].id] } : { all: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't let them in. Try again.")) });

  const leave = () => {
    if (participants.length <= 1 && rings.some((r) => r.status === "ringing")) stop.mutate(meetingId);
    void room.disconnect();
  };
  const flip = () => {
    const track = localParticipant.getTrackPublication(Track.Source.Camera)?.videoTrack as LocalVideoTrack | undefined;
    const facing = track?.mediaStreamTrack.getSettings().facingMode === "environment" ? "user" : "environment";
    void track?.restartTrack({ facingMode: facing }).catch(() => toast.error("Couldn't switch camera."));
  };

  const remote = tracks.filter((t) => !t.participant.isLocal);
  const local = tracks.find((t) => t.participant.isLocal);
  const ringing = rings.filter((r) => r.status === "ringing");
  const s = theme.space;

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas, paddingTop: insets.top }}>
      <View style={{ flexDirection: "row", alignItems: "center", paddingHorizontal: s[4], paddingVertical: s[2], gap: s[2] }}>
        <Text role="titleSm" numberOfLines={1} style={{ flex: 1 }}>
          {title}
        </Text>
        <RecordingPills meta={meta} />
        {isHost && (lobby.data?.items.length ?? 0) > 0 && (
          <Button size="md" variant="soft" onPress={() => setPanel("people")}>
            {`${lobby.data?.items.length} waiting`}
          </Button>
        )}
      </View>
      {state === ConnectionState.Reconnecting || state === ConnectionState.SignalReconnecting ? <Banner tone="warning" title="Reconnecting…" style={{ marginHorizontal: s[4] }} /> : null}
      <BreakoutBanner meta={meta} mine={isHost || !meta.breakout || roomState.data?.breakout?.id !== meta.breakout.id ? null : (roomState.data?.breakout ?? null)} inBreakout={inBreakout} busy={joinBreakout.isPending || returning} onJoin={goBreakout} onReturn={goMain} />
      <View style={{ flex: 1, padding: s[2] }}>
        {remote.length <= 1 ? (
          <View style={{ flex: 1 }}>
            {remote[0] ? (
              <Tile trackRef={remote[0]} style={{ flex: 1 }} />
            ) : (
              <View style={{ flex: 1, alignItems: "center", justifyContent: "center", gap: s[3], padding: s[4], borderRadius: theme.radius.lg, backgroundColor: theme.colors.sunken }}>
                {waiting.length > 0 ? (
                  <>
                    <Text align="center" accessibilityLiveRegion="polite">
                      {waiting.length === 1 ? `${waiting[0]?.name ?? "Someone"} is waiting to join` : `${waiting.length} waiting to join`}
                    </Text>
                    <View style={{ flexDirection: "row", gap: s[2] }}>
                      <Button size="md" variant="secondary" onPress={() => setPanel("people")}>
                        Review
                      </Button>
                      <Button size="md" loading={admit.isPending} onPress={admitWaiting}>
                        {waiting.length === 1 ? "Admit" : "Admit all"}
                      </Button>
                    </View>
                  </>
                ) : (
                  <Text tone="muted" align="center">
                    {ringing.length ? `Calling ${ringing.map((r) => r.callee.display_name).join(", ")}…` : "You're the only one here"}
                  </Text>
                )}
              </View>
            )}
            {local ? <Tile trackRef={local} style={{ position: "absolute", right: s[3], bottom: s[3], width: s[24] + s[4], height: s[32] + s[8] }} /> : null}
          </View>
        ) : (
          <ScrollView contentContainerStyle={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            {tracks.map((t) => (
              <Tile key={`${t.participant.identity}`} trackRef={t} style={{ width: "48%", aspectRatio: 3 / 4 }} />
            ))}
          </ScrollView>
        )}
        {captions ? <CaptionsOverlay lines={lines} /> : null}
      </View>
      <View style={{ flexDirection: "row", justifyContent: "space-evenly", alignItems: "center", paddingTop: s[2], paddingBottom: insets.bottom + s[3], paddingHorizontal: s[2] }}>
        <IconButton
          label={!isMicrophoneEnabled && !canUnmute ? "Ask to speak" : isMicrophoneEnabled ? "Turn off microphone" : "Turn on microphone"}
          icon={isMicrophoneEnabled ? Mic : MicOff}
          variant={isMicrophoneEnabled ? "secondary" : "danger"}
          size="lg"
          onPress={() => {
            if (!isMicrophoneEnabled && !canUnmute) {
              if (!myHand && !inBreakout) hand.mutate({ raised: true });
              toast("A host has to let you speak. Your hand is up.");
              return;
            }
            void setMedia("mic", () => localParticipant.setMicrophoneEnabled(!isMicrophoneEnabled));
          }}
        />
        <IconButton label={isCameraEnabled ? "Turn off camera" : "Turn on camera"} icon={isCameraEnabled ? Video : VideoOff} variant={isCameraEnabled ? "secondary" : "danger"} size="lg" onPress={() => void setMedia("camera", () => localParticipant.setCameraEnabled(!isCameraEnabled))} />
        <IconButton label={openPolls ? `Polls and Q&A (${openPolls} new)` : "Polls and Q&A"} icon={BarChart3} variant={openPolls ? "primary" : "secondary"} size="lg" onPress={() => setPanel("activities")} />
        <IconButton label={`People (${participants.length})`} icon={Users} variant="secondary" size="lg" onPress={() => setPanel("people")} />
        <IconButton label="Chat" icon={MessageSquare} variant="secondary" size="lg" onPress={() => setPanel("chat")} />
        <IconButton label={myHand ? "More options, hand raised" : "More options"} icon={MoreHorizontal} variant={myHand ? "primary" : "secondary"} size="lg" onPress={() => setPanel("more")} />
        <IconButton label="Leave call" icon={PhoneOff} variant="danger" size="lg" style={{ backgroundColor: theme.colors.danger }} onPress={leave} />
      </View>
      <Sheet open={panel === "people"} onClose={() => setPanel(null)} title={`People (${participants.length})`} snapPoints={["70%"]}>
        <PeopleList meetingId={meetingId} participants={participants} isHost={isHost} />
      </Sheet>
      <Sheet
        open={panel === "chat"}
        onClose={() => setPanel(null)}
        title="Chat"
        info={meta.rules.save_chat ? "Messages are saved with the meeting." : "Only people in the call see messages, and nothing is saved."}
        snapPoints={["70%"]}
      >
        <ChatList messages={chat.chatMessages.map((m) => ({ id: m.id, from: m.from?.isLocal ? "You" : m.from?.name || "Guest", text: m.message }))} send={sendChat} allowed={chatAllowed} />
      </Sheet>
      <Sheet open={panel === "more"} onClose={() => setPanel(null)} title="More" snapPoints={["45%"]}>
        <ListGroup>
          {!inBreakout && (
            <ListRow
              title={myHand ? "Lower hand" : "Raise hand"}
              leading={<Hand size={20} color={theme.colors.text} />}
              onPress={() => (setPanel(null), hand.mutate({ raised: !myHand }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change your hand.")) }))}
            />
          )}
          {isCameraEnabled && <ListRow title="Switch camera" leading={<SwitchCamera size={20} color={theme.colors.text} />} onPress={() => (setPanel(null), flip())} />}
          {meta.captions && <ListRow title={captions ? "Turn off captions" : "Turn on captions"} leading={<Captions size={20} color={theme.colors.text} />} onPress={() => (setPanel(null), setCaptions((c) => !c))} />}
        </ListGroup>
      </Sheet>
      <ActivitiesSheet open={panel === "activities"} onClose={() => setPanel(null)} target={target} isHost={isHost} />
      <RecordingConsent meta={meta} onLeave={leave} />
    </View>
  );
}

function Tile({ trackRef, style }: { trackRef: TrackReferenceOrPlaceholder; style: object }) {
  const theme = useTheme();
  const p = trackRef.participant;
  const info = useParticipantInfo({ participant: p });
  const { attributes } = useParticipantAttributes({ participant: p });
  const meta = parseCallsParticipantMeta(info.metadata);
  const speaking = useIsSpeaking(p);
  const camMuted = useIsMuted(trackRef);
  const micMuted = useIsMuted({ participant: p, source: Track.Source.Microphone });
  const name = info.name || "Guest";
  return (
    <View style={[{ overflow: "hidden", borderRadius: theme.radius.lg, backgroundColor: theme.colors.ink[900], alignItems: "center", justifyContent: "center", borderWidth: 2, borderColor: speaking ? theme.colors.primary : "transparent" }, style]}>
      {isTrackReference(trackRef) && !camMuted ? (
        <VideoTrack trackRef={trackRef} style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0 }} objectFit="cover" mirror={p.isLocal} />
      ) : (
        <Avatar name={name} src={meta.avatar_url ?? null} size="lg" />
      )}
      <View style={{ position: "absolute", left: theme.space[2], bottom: theme.space[2], flexDirection: "row", alignItems: "center", gap: theme.space[1], backgroundColor: theme.colors.overlay, borderRadius: theme.radius.sm, paddingHorizontal: theme.space[2], paddingVertical: theme.space[1] }}>
        {micMuted ? <MicOff size={14} color={theme.colors.danger} /> : null}
        {callsHandRaisedAt(attributes) ? <Hand size={14} color={theme.colors.warning} /> : null}
        <Text role="caption" style={{ color: theme.colors.ink[50] }} numberOfLines={1}>
          {p.isLocal ? "You" : name}
        </Text>
      </View>
    </View>
  );
}

function PeopleList({ meetingId, participants, isHost }: { meetingId: string; participants: Participant[]; isHost: boolean }) {
  const theme = useTheme();
  const lobby = useCallsLobby(meetingId, isHost);
  const admit = useAdmitCallsLobby(meetingId);
  const deny = useDenyCallsLobby(meetingId);
  const mute = useMuteCallsParticipant(meetingId);
  const remove = useRemoveCallsParticipant(meetingId);
  const fail = (msg: string) => (e: unknown) => toast.error(errorMessage(e, msg));
  return (
    <View style={{ gap: theme.space[4] }}>
      {isHost && (lobby.data?.items.length ?? 0) > 0 && (
        <ListGroup title="Waiting to join">
          {lobby.data?.items.map((e) => (
            <ListRow
              key={e.id}
              title={e.name}
              leading={<Avatar name={e.name} src={e.user?.avatar_url ?? null} size="sm" />}
              trailing={
                <View style={{ flexDirection: "row", gap: theme.space[2] }}>
                  <Button size="md" variant="ghost" onPress={() => deny.mutate({ ids: [e.id] }, { onError: fail("Couldn't deny.") })}>
                    Deny
                  </Button>
                  <Button size="md" onPress={() => admit.mutate({ ids: [e.id] }, { onError: fail("Couldn't admit.") })}>
                    Admit
                  </Button>
                </View>
              }
            />
          ))}
        </ListGroup>
      )}
      <ListGroup title="In the call">
        {participants.map((p) => {
          const meta = parseCallsParticipantMeta(p.metadata);
          const manage = isHost && !p.isLocal && meta.role !== "host";
          return (
            <ListRow
              key={p.identity}
              title={`${p.name || "Guest"}${p.isLocal ? " (you)" : ""}`}
              leading={<Avatar name={p.name || "Guest"} src={meta.avatar_url ?? null} size="sm" />}
              info={isCallsHostRole(meta.role) || meta.role === "guest" ? CALLS_ROLE_LABELS[meta.role] : undefined}
              trailing={
                manage ? (
                  <View style={{ flexDirection: "row", gap: theme.space[1] }}>
                    {p.isMicrophoneEnabled && <IconButton label={`Mute ${p.name}`} icon={MicOff} onPress={() => mute.mutate({ identity: p.identity }, { onError: fail("Couldn't mute.") })} />}
                    <IconButton label={`Remove ${p.name}`} icon={UserMinus} variant="danger" onPress={() => remove.mutate(p.identity, { onError: fail("Couldn't remove them.") })} />
                  </View>
                ) : p.isMicrophoneEnabled ? (
                  <Mic size={18} color={theme.colors.textMuted} />
                ) : (
                  <MicOff size={18} color={theme.colors.dangerStrong} />
                )
              }
            />
          );
        })}
      </ListGroup>
    </View>
  );
}

function ChatList({ messages, send, allowed }: { messages: { id: string; from: string; text: string }[]; send: (t: string) => Promise<unknown>; allowed: boolean }) {
  const theme = useTheme();
  const [text, setText] = useState("");
  const submit = () => {
    const body = text.trim();
    if (!body) return;
    send(body)
      .then(() => setText(""))
      .catch(() => toast.error("Couldn't send. Check your connection."));
  };
  return (
    <View style={{ gap: theme.space[3], flex: 1 }}>
      <FlatList
        data={messages}
        keyExtractor={(m) => m.id}
        renderItem={({ item }) => (
          <View style={{ gap: theme.space[0.5], paddingVertical: theme.space[1] }}>
            <Text role="label" weight={600}>
              {item.from}
            </Text>
            <Text>{item.text}</Text>
          </View>
        )}
        ListEmptyComponent={
          <Text tone="muted" align="center">
            No messages yet
          </Text>
        }
      />
      {!allowed ? (
        <Banner tone="info" title="A host turned off chat." />
      ) : (
      <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "center" }}>
        <Input value={text} onChangeText={setText} placeholder="Send a message" accessibilityLabel="Message" containerStyle={{ flex: 1 }} returnKeyType="send" onSubmitEditing={submit} maxLength={2000} />
        <IconButton label="Send" icon={SendHorizontal} variant="primary" onPress={submit} disabled={!text.trim()} />
      </View>
      )}
    </View>
  );
}
