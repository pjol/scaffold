import { SUPPORT_BODY_MAX, SUPPORT_CATEGORY_LABEL, errorMessage, useCloseTicket, useNow, useReopenTicket, useReplyTicket, useTicket, type SupportTicketDetail } from "@scaffoldapp/shared";
import { Banner, ErrorState, IconButton, InfoTip, ListGroup, ListRow, Screen, Skeleton, TextArea, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { CircleCheck, Send } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { TicketStatusBadge, ThreadMessage } from "./components";

function Composer({ ticket }: { ticket: SupportTicketDetail }) {
  const theme = useTheme();
  const reply = useReplyTicket(ticket.id);
  const [body, setBody] = useState("");
  const send = () => {
    if (!body.trim()) return;
    reply.mutate(
      { body: body.trim() },
      { onSuccess: () => setBody(""), onError: (e) => toast.error(errorMessage(e, "Couldn't send your reply. Try again.")) },
    );
  };
  return (
    <View style={{ flexDirection: "row", alignItems: "flex-end", gap: theme.space[2] }}>
      <View style={{ flex: 1 }}>
        <TextArea
          value={body}
          onChangeText={setBody}
          maxLength={SUPPORT_BODY_MAX}
          placeholder={ticket.status === "closed" ? "Reply to reopen" : "Reply"}
          accessibilityLabel="Reply"
          style={{ maxHeight: theme.space[32] }}
        />
      </View>
      <IconButton label="Send" icon={Send} variant="primary" disabled={!body.trim() || reply.isPending} onPress={send} />
    </View>
  );
}

/** One support request: the thread (live), reply, close or reopen. */
export function TicketScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { id } = useLocalSearchParams<{ id: string }>();
  const ticket = useTicket(id);
  const close = useCloseTicket();
  const reopen = useReopenTicket();
  const now = useNow(60_000);
  const t = ticket.data;

  return (
    <Screen
      edges={["bottom"]}
      keyboard
      gap={s[4]}
      refreshControl={<RefreshControl refreshing={ticket.isRefetching} onRefresh={() => void ticket.refetch()} />}
      footer={t ? <Composer ticket={t} /> : undefined}
    >
      <Stack.Screen options={{ title: t ? `Request #${t.number}` : "Request" }} />
      {ticket.isPending ? (
        <View style={{ gap: s[4] }}>
          <Skeleton width="70%" height={theme.type.title.lineHeight} />
          <Skeleton height={s[24]} />
          <Skeleton height={s[20]} />
        </View>
      ) : ticket.isError || !t ? (
        <ErrorState
          title={ticket.error?.status === 404 ? "This request doesn't exist" : "Couldn't load this request"}
          info={ticket.error ? errorMessage(ticket.error) : undefined}
          onRetry={() => void ticket.refetch()}
          retrying={ticket.isRefetching}
        />
      ) : (
        <>
          <View style={{ gap: s[2] }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Title style={{ flexShrink: 1 }}>{t.subject}</Title>
              <InfoTip content={`${SUPPORT_CATEGORY_LABEL[t.category]} · opened ${new Date(t.created_at).toLocaleDateString()}`} />
            </View>
            <View style={{ flexDirection: "row" }}>
              <TicketStatusBadge status={t.status} />
            </View>
          </View>
          {t.status === "closed" ? (
            <Banner
              tone="info"
              title="This request is closed."
              action={{
                label: "Reopen",
                onPress: () => reopen.mutate(t.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't reopen. Try again.")) }),
              }}
            />
          ) : null}
          <View style={{ gap: s[3] }}>
            {t.messages.map((m) => (
              <ThreadMessage key={m.id} message={m} now={now} />
            ))}
          </View>
          {t.status !== "closed" ? (
            <ListGroup>
              <ListRow
                title="Close request"
                leading={<CircleCheck size={22} color={theme.colors.textMuted} strokeWidth={2} />}
                chevron={false}
                disabled={close.isPending}
                onPress={() =>
                  close.mutate(t.id, {
                    onSuccess: () => toast.success("Request closed"),
                    onError: (e) => toast.error(errorMessage(e, "Couldn't close. Try again.")),
                  })
                }
              />
            </ListGroup>
          ) : null}
        </>
      )}
    </Screen>
  );
}
