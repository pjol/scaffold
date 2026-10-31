import { errorMessage, useMyTickets } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, ListGroup, ListRow, Screen, SkeletonRow, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { MessagesSquare, Plus } from "lucide-react-native";
import { RefreshControl, View } from "react-native";
import { TicketStatusBadge } from "./components";

const openNew = () => router.push("/settings/support-requests/new");

/** Settings → Support requests: the caller's tickets, newest first. */
export function TicketListScreen() {
  const theme = useTheme();
  const tickets = useMyTickets();
  const rows = tickets.data?.items ?? [];

  return (
    <Screen edges={["bottom"]} gap={theme.space[4]} refreshControl={<RefreshControl refreshing={tickets.isRefetching && !tickets.isFetchingNextPage} onRefresh={() => void tickets.refetch()} />}>
      <Stack.Screen options={{ title: "Support requests", headerRight: () => <IconButton label="New request" icon={Plus} variant="ghost" onPress={openNew} /> }} />
      {tickets.isPending ? (
        <View>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : tickets.isError ? (
        <ErrorState title="Couldn't load your requests" info={errorMessage(tickets.error)} onRetry={() => void tickets.refetch()} retrying={tickets.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState icon={MessagesSquare} title="No requests yet" action={<Button onPress={openNew}>New request</Button>} />
      ) : (
        <ListGroup title="Your requests" info="Replies also arrive by email.">
          {rows.map((t) => (
            <ListRow
              key={t.id}
              title={t.subject}
              trailing={<TicketStatusBadge status={t.status} />}
              onPress={() => router.push({ pathname: "/settings/support-requests/[id]", params: { id: t.id } })}
            />
          ))}
        </ListGroup>
      )}
      {tickets.hasNextPage ? (
        <Button variant="ghost" loading={tickets.isFetchingNextPage} onPress={() => void tickets.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </Screen>
  );
}
