import {
  callsDeniedMessage,
  errorMessage,
  isApiError,
  useCallsJoinInfo,
  useCallsKnockStatus,
  useCancelCallsKnock,
  useJoinCallsMeeting,
  useKnockCallsMeeting,
  useMe,
  type CallsJoinInfo,
  type CallsJoinToken,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, IconButton, Screen, Spinner, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Ban, CalendarX, Hourglass, Lock, Mic, MicOff, PhoneOff, UserX, Video, VideoOff } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
import { View } from "react-native";
import { CallView, type MobileCallEnd } from "./CallView";

type Step = { kind: "prejoin" } | { kind: "knocking"; lobbyId: string } | { kind: "call"; token: CallsJoinToken; camera: boolean; mic: boolean } | { kind: "over"; why: MobileCallEnd | "denied" };

const OVER: Record<MobileCallEnd | "denied", { title: string; icon: typeof PhoneOff; rejoin: boolean }> = {
  left: { title: "You left the call", icon: PhoneOff, rejoin: true },
  ended: { title: "The call ended", icon: PhoneOff, rejoin: false },
  removed: { title: "A host removed you from the call", icon: UserX, rejoin: false },
  elsewhere: { title: "You joined from another device", icon: VideoOff, rejoin: true },
  failed: { title: "The connection dropped", icon: VideoOff, rejoin: true },
  denied: { title: "A host didn't let you in", icon: Ban, rejoin: true },
};

function titleOf(info: CallsJoinInfo, meId: string | undefined): string {
  if (info.title) return info.title;
  if (info.kind === "call") {
    const names = [info.meeting?.host, ...(info.meeting?.invitees.map((i) => i.user) ?? [])].filter((u) => u && u.id !== meId).map((u) => u!.display_name);
    if (names.length) return names.length === 1 ? `Call with ${names[0]}` : `Call with ${names[0]} and ${names.length - 1} ${names.length === 2 ? "other" : "others"}`;
    return info.host_name ? `Call with ${info.host_name}` : "Call";
  }
  return "Meeting";
}

/** `meet/[code]`: pre-join (camera and mic choices), ask to join, the call, and a closing screen. Dev builds only (native WebRTC). */
export function MeetScreen() {
  const theme = useTheme();
  const { code, autojoin } = useLocalSearchParams<{ code: string; autojoin?: string }>();
  const info = useCallsJoinInfo(code);
  const me = useMe();
  const join = useJoinCallsMeeting();
  const knock = useKnockCallsMeeting();
  const cancel = useCancelCallsKnock();
  const [step, setStep] = useState<Step>({ kind: "prejoin" });
  const [camera, setCamera] = useState(true);
  const [mic, setMic] = useState(true);
  const [problem, setProblem] = useState<string | null>(null);
  const auto = useRef(false);
  const d = info.data;

  const doJoin = () => {
    if (!d) return;
    setProblem(null);
    join.mutate(d.meeting_id, {
      onSuccess: (token) => setStep({ kind: "call", token, camera, mic }),
      onError: (e) => {
        setProblem(isApiError(e) && e.code === "consent_required" ? "Accept the updated terms to join calls." : errorMessage(e, "Couldn't join. Try again."));
        setStep({ kind: "prejoin" });
        void info.refetch();
      },
    });
  };
  const doKnock = () => {
    if (!d) return;
    setProblem(null);
    knock.mutate(d.meeting_id, {
      onSuccess: (entry) => setStep({ kind: "knocking", lobbyId: entry.id }),
      onError: (e) => setProblem(errorMessage(e, "Couldn't ask to join. Try again.")),
    });
  };

  useEffect(() => {
    if (!d || auto.current) return;
    auto.current = true;
    if (d.lobby?.status === "waiting") setStep({ kind: "knocking", lobbyId: d.lobby.id });
    else if (autojoin === "1" && (d.mode === "direct" || d.mode === "host" || d.mode === "cohost")) doJoin();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [d]);

  const header = <Stack.Screen options={{ headerShown: step.kind !== "call", title: "", presentation: "fullScreenModal", gestureEnabled: step.kind !== "call" }} />;

  if (info.isPending) {
    return (
      <Screen centered edges={["bottom"]}>
        {header}
        <Spinner label="Loading meeting" />
      </Screen>
    );
  }
  if (info.isError) {
    return (
      <Screen centered edges={["bottom"]}>
        {header}
        {info.error.status === 404 ? (
          <EmptyState icon={CalendarX} title="Meeting not found" info="Check the code or link." />
        ) : (
          <ErrorState title="Couldn't load the meeting" info={info.error.message} onRetry={() => void info.refetch()} retrying={info.isFetching} />
        )}
      </Screen>
    );
  }
  const data = info.data;
  const title = titleOf(data, me.data?.id);

  if (step.kind === "call") {
    return (
      <>
        {header}
        <CallView
          token={step.token}
          meetingId={data.meeting_id}
          title={title}
          camera={step.camera}
          mic={step.mic}
          onEnd={(why) => {
            setStep({ kind: "over", why });
            void info.refetch();
          }}
        />
      </>
    );
  }
  if (step.kind === "over") {
    const o = OVER[step.why];
    return (
      <Screen centered edges={["bottom"]}>
        {header}
        <EmptyState
          icon={o.icon}
          title={o.title}
          action={
            <View style={{ gap: theme.space[3], alignSelf: "stretch" }}>
              {o.rejoin && <Button onPress={() => (step.why === "denied" ? doKnock() : doJoin())}>{step.why === "denied" ? "Ask again" : "Rejoin"}</Button>}
              <Button variant="secondary" onPress={() => router.back()}>
                Close
              </Button>
            </View>
          }
        />
      </Screen>
    );
  }
  if (step.kind === "knocking") {
    return (
      <Screen centered edges={["bottom"]}>
        {header}
        <KnockWatcher lobbyId={step.lobbyId} onAdmitted={doJoin} onDenied={() => setStep({ kind: "over", why: "denied" })} onExpired={() => (setStep({ kind: "prejoin" }), setProblem("Nobody answered your request. Ask again."))} />
        <View style={{ alignItems: "center", gap: theme.space[6] }} accessibilityLiveRegion="polite">
          <Hourglass size={40} color={theme.colors.primaryStrong} />
          <Title align="center">Waiting for a host to let you in to {title}</Title>
          <Button variant="ghost" loading={cancel.isPending} onPress={() => cancel.mutate(step.lobbyId, { onSettled: () => setStep({ kind: "prejoin" }) })}>
            Cancel
          </Button>
        </View>
      </Screen>
    );
  }
  if (data.mode === "denied") {
    return (
      <Screen centered edges={["bottom"]}>
        {header}
        <EmptyState icon={data.reason === "locked" ? Lock : data.reason === "canceled" ? CalendarX : Ban} title={callsDeniedMessage(data.reason)} />
      </Screen>
    );
  }
  return (
    <Screen
      edges={["bottom"]}
      scroll
      footer={
        <Button fullWidth loading={join.isPending || knock.isPending} onPress={() => (data.mode === "knock" ? doKnock() : doJoin())}>
          {data.mode === "knock" ? "Ask to join" : "Join now"}
        </Button>
      }
    >
      {header}
      <Title>{title}</Title>
      {data.live ? (
        <Text role="label" tone="success">
          Live now
        </Text>
      ) : null}
      <View style={{ flexDirection: "row", gap: theme.space[4], justifyContent: "center", paddingVertical: theme.space[8] }}>
        <View style={{ alignItems: "center", gap: theme.space[2] }}>
          <IconButton label={mic ? "Join with microphone off" : "Join with microphone on"} icon={mic ? Mic : MicOff} variant={mic ? "secondary" : "danger"} size="xl" onPress={() => setMic((m) => !m)} />
          <Text role="label">{mic ? "Mic on" : "Mic off"}</Text>
        </View>
        <View style={{ alignItems: "center", gap: theme.space[2] }}>
          <IconButton label={camera ? "Join with camera off" : "Join with camera on"} icon={camera ? Video : VideoOff} variant={camera ? "secondary" : "danger"} size="xl" onPress={() => setCamera((c) => !c)} />
          <Text role="label">{camera ? "Camera on" : "Camera off"}</Text>
        </View>
      </View>
      {problem ? <Banner tone="danger" title={problem} /> : null}
    </Screen>
  );
}

function KnockWatcher({ lobbyId, onAdmitted, onDenied, onExpired }: { lobbyId: string; onAdmitted: () => void; onDenied: () => void; onExpired: () => void }) {
  const status = useCallsKnockStatus(lobbyId);
  const s = status.data?.status;
  const fired = useRef(false);
  useEffect(() => {
    if (!s || s === "waiting" || fired.current) return;
    fired.current = true;
    if (s === "admitted") onAdmitted();
    else if (s === "denied" || s === "removed") onDenied();
    else onExpired();
  }, [s, onAdmitted, onDenied, onExpired]);
  return null;
}
