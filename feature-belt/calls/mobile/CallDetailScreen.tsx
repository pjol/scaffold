import { callsPeopleNames, formatCallsDuration, formatCallsWhen, useCallsCall, useCallsTimeZone, useMe } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Card, EmptyState, ErrorState, ListGroup, ListRow, Screen, Skeleton, SkeletonRow, Text, Title, useTheme } from "@scaffoldapp/ui-native";
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
