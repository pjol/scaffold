import { CALLS_RECORDING_STATUS_LABELS, callsPeopleNames, formatCallsDuration, formatCallsTime, formatCallsWhen, useCallsCall, useCallsRecording, useCallsTimeZone, useCallsTranscript, useMe, type CallsRecording } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Card, EmptyState, ErrorState, FilePreview, ListGroup, ListRow, Screen, Skeleton, SkeletonRow, Spinner, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Video } from "lucide-react-native";
import { Share, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";

/** One call: when, how long, and everyone's time in it. */
export function CallDetailScreen() {
  const theme = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const call = useCallsCall(id);
  const me = useMe();
  const zone = useCallsTimeZone();

  if (call.isPending) {
    return (
      <Screen edges={["bottom"]} scroll>
        <Stack.Screen options={{ title: "Call" }} />
        <Skeleton style={{ height: theme.space[8], width: "60%" }} />
        {Array.from({ length: 3 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </Screen>
    );
  }
  if (call.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "Call" }} />
        {call.error.status === 404 ? <EmptyState icon={Video} title="Call not found" /> : <ErrorState title="Couldn't load the call" info={call.error.message} onRetry={() => void call.refetch()} retrying={call.isFetching} />}
      </Screen>
    );
  }
  const c = call.data;
  const title = c.meeting.title || `Call with ${callsPeopleNames(c.participants, me.data?.id)}`;
  return (
    <Screen
      edges={["bottom"]}
      scroll
      footer={
        c.can_join ? (
          <Button fullWidth leftIcon={Video} onPress={() => router.push({ pathname: "/meet/[code]", params: { code: c.meeting.code } })}>
            {c.live ? "Join" : "Start again"}
          </Button>
        ) : undefined
      }
    >
      <Stack.Screen
        options={{
          title: "Call",
          headerRight: () => (
            <ResourceMenu
              feature="calls"
              kind="calls.meeting"
              id={c.meeting.id}
              title={title}
              actions={[{ id: "share", label: "Share link", icon: Video, onSelect: () => void Share.share({ message: c.meeting.code }) }]}
            />
          ),
        }}
      />
      <Title>{title}</Title>
      <Card>
        <View style={{ gap: theme.space[3] }}>
          <Fact label="Started" value={formatCallsWhen(c.started_at, zone)} />
          <Fact label="Duration" value={c.live ? "Live now" : formatCallsDuration(c.duration_seconds)} />
          <Fact label="Host" value={c.host.display_name} />
        </View>
      </Card>
      {c.records && c.records.recordings.length > 0 ? (
        <ListGroup title="Recordings">
          {c.records.recordings.map((r) => (
            <RecordingRow key={r.id} r={r} />
          ))}
        </ListGroup>
      ) : null}
      {c.records && c.records.transcript_lines > 0 ? <Transcript sessionId={c.id} /> : null}
      <ListGroup title={`People (${c.participants.length})`}>
        {c.participants.map((p) => (
          <ListRow
            key={p.identity}
            title={`${p.name}${p.user?.id === me.data?.id ? " (you)" : ""}`}
            leading={<Avatar name={p.name} src={p.user?.avatar_url ?? null} size="sm" />}
            trailing={
              <View style={{ flexDirection: "row", gap: theme.space[2], alignItems: "center" }}>
                {p.guest ? <Badge>Guest</Badge> : null}
                <Text role="label" tone="muted" tabular>
                  {p.last_left_at ? formatCallsDuration(p.seconds) : "In the call"}
                </Text>
              </View>
            }
          />
        ))}
      </ListGroup>
    </Screen>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  const theme = useTheme();
  return (
    <View style={{ flexDirection: "row", justifyContent: "space-between", gap: theme.space[2] }}>
      <Text tone="muted">{label}</Text>
      <Text>{value}</Text>
    </View>
  );
}

/** A recording: ready ones open in the player (the in-app browser). */
function RecordingRow({ r }: { r: CallsRecording }) {
  const zone = useCallsTimeZone();
  const one = useCallsRecording(r.status === "ready" ? r.id : null);
  const label = `${formatCallsTime(r.started_at, zone)}${r.duration_seconds ? ` · ${formatCallsDuration(r.duration_seconds)}` : ""}`;
  if (r.status !== "ready") return <ListRow title={label} trailing={<Badge>{CALLS_RECORDING_STATUS_LABELS[r.status]}</Badge>} />;
  if (!one.data?.url) return <ListRow title={label} trailing={one.isError ? <Text tone="muted">Unavailable</Text> : <Spinner />} />;
  return <FilePreview compact url={one.data.url} name={`${r.meeting_title || "Recording"} ${label}.mp4`} mime="video/mp4" size={r.size_bytes} downloadUrl={one.data.download_url} />;
}

function Transcript({ sessionId }: { sessionId: string }) {
  const theme = useTheme();
  const t = useCallsTranscript(sessionId);
  const zone = useCallsTimeZone();
  if (t.isPending) return <SkeletonRow />;
  if (t.isError) return <ErrorState title="Couldn't load the transcript" info={t.error.message} onRetry={() => void t.refetch()} retrying={t.isFetching} />;
  return (
    <ListGroup title="Transcript">
      <View style={{ gap: theme.space[3], paddingVertical: theme.space[4] }}>
        {t.data.lines.map((l, i) => (
          <View key={i} style={{ gap: theme.space[0.5] }}>
            <Text role="label" weight={600}>{`${l.name || "Guest"} · ${formatCallsTime(l.spoken_at, zone)}`}</Text>
            <Text>{l.text}</Text>
          </View>
        ))}
      </View>
    </ListGroup>
  );
}
