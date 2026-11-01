import {
  docsTimeAgo,
  errorMessage,
  useDocsComments,
  useDocsCreateComment,
  useDocsPage,
  useDocsResolveComment,
  type DocsComment,
  type DocsRefs,
} from "@scaffoldapp/shared";
import { Avatar, Button, EmptyState, ErrorState, Screen, SegmentedControl, SkeletonRow, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { MessageSquare } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { RichText } from "./RichText";

function Item({ c, refs }: { c: DocsComment; refs: DocsRefs }) {
  const theme = useTheme();
  if (c.deleted) {
    return (
      <Text role="label" tone="subtle" style={{ fontStyle: "italic" }}>
        Comment deleted
      </Text>
    );
  }
  return (
    <View style={{ flexDirection: "row", gap: theme.space[2] }}>
      <Avatar name={c.author?.name ?? "Deleted user"} src={c.author?.avatar_url} size="sm" />
      <View style={{ flex: 1, gap: theme.space[0.5] }}>
        <Text role="label" weight={600}>
          {c.author?.name ?? "Deleted user"}{" "}
          <Text role="caption" tone="muted">
            {docsTimeAgo(c.created_at)}
          </Text>
        </Text>
        <RichText spans={c.body} refs={refs} role="body" />
      </View>
    </View>
  );
}

function Thread({ t, pageId, refs, canComment }: { t: DocsComment; pageId: string; refs: DocsRefs; canComment: boolean }) {
  const theme = useTheme();
  const [reply, setReply] = useState("");
  const create = useDocsCreateComment(pageId);
  const resolve = useDocsResolveComment(pageId);
  return (
    <View style={{ gap: theme.space[3], padding: theme.space[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border, backgroundColor: theme.colors.surface }}>
      {t.quote ? (
        <Text role="label" numberOfLines={2} style={{ backgroundColor: theme.colors.warningSoft, paddingHorizontal: theme.space[2], paddingVertical: theme.space[1], borderRadius: theme.radius.sm }}>
          {t.quote}
        </Text>
      ) : null}
      <Item c={t} refs={refs} />
      {t.replies.map((r) => (
        <Item key={r.id} c={r} refs={refs} />
      ))}
      {canComment && !t.resolved_at ? (
        <View style={{ gap: theme.space[2] }}>
          <TextArea value={reply} onChangeText={setReply} placeholder="Reply" accessibilityLabel="Reply" maxLength={4000} />
          <View style={{ flexDirection: "row", gap: theme.space[2] }}>
            <Button
              size="md"
              variant="secondary"
              style={{ flex: 1 }}
              loading={resolve.isPending}
              onPress={() => resolve.mutate({ id: t.id, resolved: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't resolve the thread.")) })}
            >
              Resolve
            </Button>
            <Button
              size="md"
              style={{ flex: 1 }}
              disabled={!reply.trim()}
              loading={create.isPending}
              onPress={() =>
                create.mutate(
                  { thread_id: t.id, body: [{ text: reply.trim() }] },
                  { onSuccess: () => setReply(""), onError: (e) => toast.error(errorMessage(e, "Couldn't send the reply.")) },
                )
              }
            >
              Reply
            </Button>
          </View>
        </View>
      ) : null}
      {canComment && t.resolved_at ? (
        <Button size="md" variant="ghost" onPress={() => resolve.mutate({ id: t.id, resolved: false })}>
          Reopen
        </Button>
      ) : null}
    </View>
  );
}

/** docs/[id]/comments: threads on a page; reply, resolve, reopen, new comment. */
export function CommentsScreen() {
  const theme = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const page = useDocsPage(id);
  const [status, setStatus] = useState<"open" | "resolved">("open");
  const threads = useDocsComments(id, status);
  const create = useDocsCreateComment(id);
  const [text, setText] = useState("");
  const level = page.data?.page.my_level;
  const canComment = (level === "comment" || level === "edit" || level === "manage") && !page.data?.page.deleted_at;
  const refs = page.data?.refs ?? { users: {}, pages: {} };
  return (
    <Screen
      edges={["bottom"]}
      scroll
      keyboard
      footer={
        canComment ? (
          <View style={{ gap: theme.space[2] }}>
            <TextArea value={text} onChangeText={setText} placeholder="Add a comment" accessibilityLabel="Add a comment" maxLength={4000} />
            <Button
              fullWidth
              disabled={!text.trim()}
              loading={create.isPending}
              onPress={() =>
                create.mutate({ body: [{ text: text.trim() }] }, { onSuccess: () => setText(""), onError: (e) => toast.error(errorMessage(e, "Couldn't post the comment.")) })
              }
            >
              Comment
            </Button>
          </View>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: "Comments" }} />
      <SegmentedControl<"open" | "resolved">
        value={status}
        onValueChange={setStatus}
        options={[
          { value: "open", label: "Open" },
          { value: "resolved", label: "Resolved" },
        ]}
      />
      {threads.isPending ? (
        <>
          <SkeletonRow />
          <SkeletonRow />
        </>
      ) : threads.isError ? (
        <ErrorState title="Couldn't load comments." info={errorMessage(threads.error)} onRetry={() => void threads.refetch()} />
      ) : threads.data.length === 0 ? (
        <EmptyState icon={MessageSquare} title={status === "open" ? "No open comments" : "No resolved comments"} />
      ) : (
        threads.data.map((t) => <Thread key={t.id} t={t} pageId={id} refs={refs} canComment={canComment} />)
      )}
    </Screen>
  );
}
