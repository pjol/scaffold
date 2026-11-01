import { useChat, useParticipantAttributes } from "@livekit/components-react";
import { AudioSession, isTrackReference, registerGlobals, RoomContext, useConnectionState, useIsMuted, useIsSpeaking, useLocalParticipant, useParticipantInfo, useParticipants, useTracks, VideoTrack, type TrackReferenceOrPlaceholder } from "@livekit/react-native";
import {
  CALLS_ROLE_LABELS,
  callsHandRaisedAt,
  errorMessage,
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
import { Hand, MessageSquare, Mic, MicOff, PhoneOff, SendHorizontal, SwitchCamera, UserMinus, Users, Video, VideoOff } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
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

/** The call: video tiles, controls, people (with host controls and the lobby) and chat. */
export function CallView({ token, meetingId, title, camera, mic, onEnd }: { token: CallsJoinToken; meetingId: string; title: string; camera: boolean; mic: boolean; onEnd: (why: MobileCallEnd) => void }) {
  const [room, setRoom] = useState<Room | null>(null);
  const theme = useTheme();
  const [failed, setFailed] = useState(false);
  const endRef = useRef(onEnd);
  endRef.current = onEnd;

  useEffect(() => {
    const r = new Room({ adaptiveStream: true, dynacast: true });
    let live = true;
    r.on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
      if (live) endRef.current(endFor(reason));
    });
    setRoom(r);
    const start = setTimeout(async () => {
      await AudioSession.startAudioSession();
      try {
        await r.connect(token.url, token.token);
        if (!live) return;
        if (mic) await setMedia("mic", () => r.localParticipant.setMicrophoneEnabled(true));
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
      <CallUI room={room} meetingId={meetingId} title={title} role={token.role} />
    </RoomContext.Provider>
  );
}

function CallUI({ room, meetingId, title, role }: { room: Room; meetingId: string; title: string; role: CallsRole }) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const state = useConnectionState(room);
  const { localParticipant, isMicrophoneEnabled, isCameraEnabled } = useLocalParticipant();
  const participants = useParticipants();
  const tracks = useTracks([{ source: Track.Source.Camera, withPlaceholder: true }], { onlySubscribed: false });
  const [panel, setPanel] = useState<"people" | "chat" | null>(null);
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
        {isHost && (lobby.data?.items.length ?? 0) > 0 && (
          <Button size="md" variant="soft" onPress={() => setPanel("people")}>
            {`${lobby.data?.items.length} waiting`}
          </Button>
        )}
      </View>
      {state === ConnectionState.Reconnecting || state === ConnectionState.SignalReconnecting ? <Banner tone="warning" title="Reconnecting…" style={{ marginHorizontal: s[4] }} /> : null}
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
      </View>
      <View style={{ flexDirection: "row", justifyContent: "space-evenly", alignItems: "center", paddingTop: s[2], paddingBottom: insets.bottom + s[3], paddingHorizontal: s[2] }}>
        <IconButton label={isMicrophoneEnabled ? "Turn off microphone" : "Turn on microphone"} icon={isMicrophoneEnabled ? Mic : MicOff} variant={isMicrophoneEnabled ? "secondary" : "danger"} size="lg" onPress={() => void setMedia("mic", () => localParticipant.setMicrophoneEnabled(!isMicrophoneEnabled))} />
        <IconButton label={isCameraEnabled ? "Turn off camera" : "Turn on camera"} icon={isCameraEnabled ? Video : VideoOff} variant={isCameraEnabled ? "secondary" : "danger"} size="lg" onPress={() => void setMedia("camera", () => localParticipant.setCameraEnabled(!isCameraEnabled))} />
        {isCameraEnabled && <IconButton label="Switch camera" icon={SwitchCamera} variant="secondary" size="lg" onPress={flip} />}
        <IconButton label={myHand ? "Lower hand" : "Raise hand"} icon={Hand} variant={myHand ? "primary" : "secondary"} size="lg" onPress={() => hand.mutate({ raised: !myHand }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change your hand.")) })} />
        <IconButton label={`People (${participants.length})`} icon={Users} variant="secondary" size="lg" onPress={() => setPanel("people")} />
        <IconButton label="Chat" icon={MessageSquare} variant="secondary" size="lg" onPress={() => setPanel("chat")} />
        <IconButton label="Leave call" icon={PhoneOff} variant="danger" size="lg" style={{ backgroundColor: theme.colors.danger }} onPress={leave} />
      </View>
      <Sheet open={panel === "people"} onClose={() => setPanel(null)} title={`People (${participants.length})`} snapPoints={["70%"]}>
        <PeopleList meetingId={meetingId} participants={participants} isHost={isHost} />
      </Sheet>
      <Sheet open={panel === "chat"} onClose={() => setPanel(null)} title="Chat" info="Only people in the call see messages, and nothing is saved." snapPoints={["70%"]}>
        <ChatList messages={chat.chatMessages.map((m) => ({ id: m.id, from: m.from?.isLocal ? "You" : m.from?.name || "Guest", text: m.message }))} send={(t) => chat.send(t)} />
      </Sheet>
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

function ChatList({ messages, send }: { messages: { id: string; from: string; text: string }[]; send: (t: string) => Promise<unknown> }) {
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
      <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "center" }}>
        <Input value={text} onChangeText={setText} placeholder="Send a message" accessibilityLabel="Message" containerStyle={{ flex: 1 }} returnKeyType="send" onSubmitEditing={submit} maxLength={2000} />
        <IconButton label="Send" icon={SendHorizontal} variant="primary" onPress={submit} disabled={!text.trim()} />
      </View>
    </View>
  );
}
