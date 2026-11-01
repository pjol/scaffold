import {
  assistantConversationTitle,
  assistantLimitReached,
  errorMessage,
  hasErrorCode,
  useAssistantChat,
  useAssistantConversation,
  useAssistantMessages,
  useAssistantStatus,
  useCreateAssistantConversation,
  useDeleteAssistantConversation,
  type AssistantMessage,
} from "@scaffoldapp/shared";
import { Banner, ConfirmDialog, EmptyState, ErrorState, IconButton, Screen, Skeleton, Spinner, toast, useTheme } from "@scaffoldapp/ui-native";
import { fetch as expoFetch } from "expo/fetch";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Sparkles, Trash2 } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { FlatList, View } from "react-native";
import { Composer } from "./Composer";
import { ConsentDialog } from "./ConsentDialog";
import { MessageItem } from "./MessageItem";

const time = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" });

/**
 * One conversation (route /assistant/[id]; "new" starts one on the first message). Replies
 * stream over `expo/fetch`; leaving the screen stops reading, not the reply, which resumes
 * when the conversation opens again.
 */
export function ThreadScreen() {
  const theme = useTheme();
  const s = theme.space;
  const params = useLocalSearchParams<{ id: string }>();
  const conversationId = params.id && params.id !== "new" ? params.id : undefined;
  const status = useAssistantStatus();
  const conversation = useAssistantConversation(conversationId);
  const messages = useAssistantMessages(conversationId);
  const chat = useAssistantChat(conversationId, { fetch: expoFetch });
  const create = useCreateAssistantConversation();
  const remove = useDeleteAssistantConversation();
  const [consentOpen, setConsentOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [deleting, setDeleting] = useState(false);
  const pending = useRef<string | null>(null);
  const items = useMemo(() => messages.data?.items ?? [], [messages.data]);

  useEffect(() => {
    if (chat.error?.code === "ai_consent_required") {
      void status.refetch();
      setConsentOpen(true);
      chat.clearError();
    }
  }, [chat, status]);

  const lastUserText = items.find((m) => m.role === "user")?.parts.map((p) => p.text).join("") ?? null;

  // `consented` skips the check right after the dialog (this render's status is stale).
  const send = async (text: string, consented = false): Promise<boolean> => {
    if (!status.data?.available) return false;
    if (status.data.consent.required && !consented) {
      pending.current = text;
      setConsentOpen(true);
      return false;
    }
    let id = conversationId;
    if (!id) {
      try {
        id = (await create.mutateAsync()).id;
        router.setParams({ id });
      } catch (e) {
        toast.error(errorMessage(e, "Couldn't start a conversation. Try again."));
        return false;
      }
    }
    pending.current = text;
    // The message is posted as soon as the stream opens; clear the box then, not at the end.
    setDraft("");
    const ok = await chat.send(text, id);
    if (ok) pending.current = null;
    else if (pending.current === text) setDraft((d) => d || text);
    return ok;
  };

  const header = (
    <Stack.Screen
      options={{
        title: assistantConversationTitle(conversation.data),
        headerRight: conversationId ? () => <IconButton label="Delete conversation" icon={Trash2} variant="ghost" onPress={() => setDeleting(true)} /> : undefined,
      }}
    />
  );

  if (status.isPending || (conversationId && (conversation.isPending || messages.isPending))) {
    return (
      <Screen edges={["bottom"]} gap={s[4]}>
        {header}
        <Skeleton width="60%" height={48} style={{ alignSelf: "flex-end" }} />
        <Skeleton shape="text" width="100%" />
        <Skeleton shape="text" width="80%" />
      </Screen>
    );
  }
  if (status.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        <ErrorState title="Couldn't load the assistant" onRetry={() => void status.refetch()} retrying={status.isFetching} />
      </Screen>
    );
  }
  if (conversationId && conversation.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        {hasErrorCode(conversation.error, "not_found") ? (
          <EmptyState icon={Sparkles} title="This conversation doesn't exist" />
        ) : (
          <ErrorState title="Couldn't load this conversation" onRetry={() => void conversation.refetch()} retrying={conversation.isFetching} />
        )}
      </Screen>
    );
  }
  const st = status.data;
  if (!st.available || !st.provider) {
    return (
      <Screen edges={["bottom"]} centered>
        {header}
        <EmptyState icon={Sparkles} title="The assistant isn't available" info="The server has no AI provider configured." />
      </Screen>
    );
  }
  const reached = assistantLimitReached(st.usage);
  const limited = reached !== null || chat.error?.code === "quota_exceeded";
  const otherError = chat.error && chat.error?.code !== "quota_exceeded" && chat.error?.code !== "ai_consent_required" ? chat.error : null;

  return (
    <Screen
      edges={["bottom"]}
      scroll={false}
      padded={false}
      keyboard
      footer={
        <View style={{ gap: s[2] }}>
          {limited ? (
            <Banner
              tone="warning"
              title={`${reached === "messages" ? `You've sent today's ${st.usage.message_limit} messages.` : "You've reached today's usage limit."} Back at ${time.format(new Date(st.usage.resets_at))}.`}
            />
          ) : null}
          {otherError ? <Banner tone="danger" title={otherError.message} onClose={chat.clearError} /> : null}
          <Composer
            value={draft}
            onChange={setDraft}
            onSend={send}
            onStop={() => void chat.stop().catch((e: unknown) => toast.error(errorMessage(e, "Couldn't stop the reply. Try again.")))}
            streaming={chat.streaming}
            disabled={limited}
            maxChars={st.max_message_chars}
          />
        </View>
      }
    >
      {header}
      <FlatList<AssistantMessage>
        inverted
        data={items}
        keyExtractor={(m) => m.id}
        keyboardDismissMode="interactive"
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ paddingHorizontal: s[4], paddingVertical: s[4], gap: s[6], flexGrow: 1 }}
        renderItem={({ item, index }) => (
          <MessageItem message={item} onRetry={index === 0 && lastUserText && !chat.streaming ? () => void send(lastUserText) : undefined} />
        )}
        onEndReached={() => {
          if (messages.hasNextPage && !messages.isFetchingNextPage) void messages.fetchNextPage();
        }}
        onEndReachedThreshold={0.5}
        ListFooterComponent={
          <View style={{ gap: s[4] }}>
            {messages.isFetchingNextPage ? <Spinner label="Loading earlier messages" /> : null}
            {st.mock ? <Banner tone="warning" title="Development mock: replies are scripted and no AI provider is called." /> : null}
          </View>
        }
        ListEmptyComponent={
          <View style={{ flex: 1, justifyContent: "center", transform: [{ scaleY: -1 }] }}>
            <EmptyState icon={Sparkles} title="Ask the assistant" />
          </View>
        }
      />
      <ConsentDialog
        provider={st.provider}
        open={consentOpen}
        onClose={() => setConsentOpen(false)}
        onAccepted={() => {
          const text = pending.current;
          if (text) void send(text, true);
        }}
      />
      {conversationId ? (
        <ConfirmDialog
          open={deleting}
          onClose={() => setDeleting(false)}
          title="Delete this conversation?"
          confirmLabel="Delete"
          tone="danger"
          onConfirm={async () => {
            await remove.mutateAsync(conversationId);
            toast.success("Conversation deleted");
            router.back();
          }}
          errorText={(e) => errorMessage(e, "Couldn't delete. Try again.")}
        >
          It can't be undone.
        </ConfirmDialog>
      ) : null}
    </Screen>
  );
}
