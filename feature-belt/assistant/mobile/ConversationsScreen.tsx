import { assistantConversationTitle, useAssistantConversations, useAssistantStatus } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, ListGroup, ListRow, Screen, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { MessageSquarePlus, MessagesSquare, Sparkles } from "lucide-react-native";
import { RefreshControl, View } from "react-native";

const relative = (iso: string) => {
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins} min`;
  if (mins < 24 * 60) return `${Math.round(mins / 60)} h`;
  return `${Math.round(mins / (24 * 60))} d`;
};

/** The user's assistant conversations and a button to start one. */
export function ConversationsScreen() {
  const theme = useTheme();
  const status = useAssistantStatus();
  const list = useAssistantConversations({ limit: 30 });
  const rows = list.data?.items ?? [];
  const unavailable = status.data && !status.data.available;

  return (
    <Screen
      edges={["bottom"]}
      gap={theme.space[6]}
      refreshControl={<RefreshControl refreshing={list.isRefetching} onRefresh={() => void list.refetch()} />}
      footer={
        unavailable ? undefined : (
          <Button leftIcon={MessageSquarePlus} onPress={() => router.push("/assistant/new")}>
            New conversation
          </Button>
        )
      }
    >
      {unavailable ? (
        <EmptyState icon={Sparkles} title="The assistant isn't available" info="The server has no AI provider configured." />
      ) : list.isPending ? (
        <View style={{ gap: theme.space[2] }}>
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} height={theme.tap.base} />
          ))}
        </View>
      ) : list.isError ? (
        <ErrorState title="Couldn't load conversations" onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={MessagesSquare} title="No conversations yet" />
      ) : (
        <ListGroup>
          {rows.map((c) => (
            <ListRow
              key={c.id}
              title={assistantConversationTitle(c)}
              trailing={
                <Text role="caption" tone="muted">
                  {c.running ? "Replying" : relative(c.last_message_at)}
                </Text>
              }
              onPress={() => router.push({ pathname: "/assistant/[id]", params: { id: c.id } })}
            />
          ))}
          {list.hasNextPage ? <ListRow title="Show more" chevron={false} onPress={() => void list.fetchNextPage()} /> : null}
        </ListGroup>
      )}
    </Screen>
  );
}
