import {
  buildMentions,
  errorMessage,
  FEED_LIMITS,
  feedKeys,
  mentionedPeople,
  threadComments,
  useCreateFeedComment,
  useDeleteFeedComment,
  useFeedComments,
  useFeedPost,
  useFeedTopic,
  useMe,
  useQueryClient,
  useReactToFeedComment,
  useUpdateFeedComment,
  type FeedComment,
  type FeedPost,
} from "@scaffoldapp/shared";
import { Avatar, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, ListRow, Screen, Sheet, Skeleton, Text, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Ban, Flag, MessageCircle, MoreHorizontal, Pencil, Reply, Send, Trash2, X } from "lucide-react-native";
import { useRef, useState, type RefObject } from "react";
import { Pressable, RefreshControl, View, type TextInput } from "react-native";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { MentionInput, type MentionPerson } from "./MentionInput";
import { ReactButton, ReactionSummary, RichText } from "./parts";
import { PostCard } from "./PostCard";
import { timeAgo } from "./time";

const PLACEHOLDER: Record<Exclude<FeedComment["status"], "visible">, string> = {
  deleted: "Comment deleted",
  removed: "Removed by moderators",
  hidden: "Hidden",
};

type Mode = { kind: "new" } | { kind: "reply"; comment: FeedComment } | { kind: "edit"; comment: FeedComment };
type Next = "edit" | "delete" | "report" | "block" | null;

function CommentRow({ comment, post, onReply, onEdit }: { comment: FeedComment; post: FeedPost; onReply: (c: FeedComment) => void; onEdit: (c: FeedComment) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const me = useMe();
  const qc = useQueryClient();
  const react = useReactToFeedComment();
  const del = useDeleteFeedComment();
  const [actions, setActions] = useState(false);
  const [dialog, setDialog] = useState<Next>(null);
  const next = useRef<Next>(null);

  if (comment.status !== "visible" || !comment.author) {
    return (
      <Text role="label" tone="muted" style={{ fontStyle: "italic", paddingVertical: s[2] }}>
        {PLACEHOLDER[comment.status === "visible" ? "hidden" : comment.status]}
      </Text>
    );
  }
  const author = comment.author;
  const name = author.display_name || "Deleted user";
  const mine = me.data?.id === author.id;
  const canDelete = mine || me.data?.id === post.author.id;
  const hidden = () => void qc.invalidateQueries({ queryKey: feedKeys.all() });
  const choose = (n: Next) => {
    next.current = n;
    setActions(false);
  };

  return (
    <View style={{ flexDirection: "row", gap: s[3] }}>
      <Avatar name={name} src={author.avatar_url} size="sm" />
      <View style={{ flex: 1, gap: s[1] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
            {name}
          </Text>
          <Text role="caption" tone="muted" style={{ flex: 1 }} numberOfLines={1}>
            {timeAgo(comment.created_at)}
            {comment.edited_at ? " · Edited" : ""}
          </Text>
          <IconButton label="Comment options" icon={MoreHorizontal} variant="ghost" onPress={() => setActions(true)} />
        </View>
        <RichText text={comment.body} mentions={comment.mentions} />
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
          <ReactButton mine={comment.my_reaction} onReact={(reaction) => react.mutate({ comment, reaction }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your reaction. Try again.")) })} />
          <Pressable accessibilityRole="button" onPress={() => onReply(comment)} hitSlop={4} style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: s[2] }}>
            <Reply size={18} color={c.textMuted} strokeWidth={2} />
            <Text role="label" weight={600} tone="muted">
              Reply
            </Text>
          </Pressable>
          <View style={{ flex: 1 }} />
          <ReactionSummary reactions={comment.reactions} total={comment.reaction_count} />
        </View>
      </View>

      <Sheet
        open={actions}
        onClose={() => setActions(false)}
        onDismissed={() => {
          const n = next.current;
          next.current = null;
          if (n === "edit") onEdit(comment);
          else if (n) setDialog(n);
        }}
        title="Comment"
      >
        <View style={{ gap: s[1] }}>
          {mine ? <ListRow title="Edit" leading={<Pencil size={20} color={c.textMuted} />} onPress={() => choose("edit")} /> : null}
          {!mine ? <ListRow title="Report comment" leading={<Flag size={20} color={c.textMuted} />} onPress={() => choose("report")} /> : null}
          {!mine ? <ListRow title={`Block ${name}`} destructive leading={<Ban size={20} color={c.dangerStrong} />} onPress={() => choose("block")} /> : null}
          {canDelete ? <ListRow title="Delete" destructive leading={<Trash2 size={20} color={c.dangerStrong} />} onPress={() => choose("delete")} /> : null}
        </View>
      </Sheet>
      <ConfirmDialog
        open={dialog === "delete"}
        onClose={() => setDialog(null)}
        title="Delete this comment?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync({ id: comment.id, postId: post.id });
          toast.success("Comment deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the comment. Try again.")}
      >
        <Text role="body">It can't be undone.</Text>
      </ConfirmDialog>
      {!mine ? (
        <>
          <ReportDialog
            open={dialog === "report"}
            onClose={() => setDialog(null)}
            targetKind="feed.comment"
            targetId={comment.id}
            noun="comment"
            author={{ id: author.id, name }}
            onReported={({ blocked }) => blocked && hidden()}
          />
          <BlockDialog open={dialog === "block"} onClose={() => setDialog(null)} userId={author.id} name={name} onBlocked={hidden} />
        </>
      ) : null}
    </View>
  );
}

function Composer({ post, mode, onMode, inputRef }: { post: FeedPost; mode: Mode; onMode: (m: Mode) => void; inputRef: RefObject<TextInput | null> }) {
  const theme = useTheme();
  const s = theme.space;
  const create = useCreateFeedComment();
  const update = useUpdateFeedComment();
  const editing = mode.kind === "edit" ? mode.comment : null;
  const [body, setBody] = useState("");
  const [people, setPeople] = useState<MentionPerson[]>([]);
  const [seeded, setSeeded] = useState<string | null>(null);
  if (editing && seeded !== editing.id) {
    setSeeded(editing.id);
    setBody(editing.body);
    setPeople(mentionedPeople(editing.mentions));
  }
  if (!editing && seeded) setSeeded(null);
  const busy = create.isPending || update.isPending;
  const over = body.length > FEED_LIMITS.commentChars;

  const submit = async () => {
    if (!body.trim() || over || busy) return;
    const mentions = buildMentions(body, people);
    try {
      if (editing) await update.mutateAsync({ id: editing.id, body, mentions });
      else {
        const parent = mode.kind === "reply" ? (mode.comment.parent_id ?? mode.comment.id) : null;
        await create.mutateAsync({ postId: post.id, body, mentions, parent_id: parent });
      }
      setBody("");
      setPeople([]);
      onMode({ kind: "new" });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save your comment. Try again."));
    }
  };

  const label = mode.kind === "reply" ? `Replying to ${mode.comment.author?.display_name || "a comment"}` : mode.kind === "edit" ? "Editing your comment" : null;
  return (
    <View style={{ gap: s[2] }}>
      {label ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="caption" tone="muted" style={{ flex: 1 }} numberOfLines={1}>
            {label}
          </Text>
          <IconButton
            label={mode.kind === "edit" ? "Cancel editing" : "Cancel reply"}
            icon={X}
            variant="ghost"
            onPress={() => {
              if (mode.kind === "edit") {
                setBody("");
                setPeople([]);
              }
              onMode({ kind: "new" });
            }}
          />
        </View>
      ) : null}
      <View style={{ flexDirection: "row", alignItems: "flex-end", gap: s[2] }}>
        <View style={{ flex: 1 }}>
          <MentionInput ref={inputRef} value={body} onChange={setBody} people={people} onPeopleChange={setPeople} placeholder={mode.kind === "reply" ? "Write a reply" : "Write a comment"} accessibilityLabel="Comment" />
        </View>
        <IconButton label={editing ? "Save" : "Send"} icon={Send} variant="primary" loading={busy} disabled={!body.trim() || over} onPress={() => void submit()} />
      </View>
    </View>
  );
}

/** A post and its comments (one level of replies), with the comment box pinned above the keyboard. */
export function PostScreen({ id }: { id: string }) {
  const theme = useTheme();
  const s = theme.space;
  useFeedTopic();
  const post = useFeedPost(id);
  const comments = useFeedComments(id);
  const [mode, setMode] = useState<Mode>({ kind: "new" });
  const input = useRef<TextInput>(null);
  const threads = threadComments(comments.data?.items ?? []);

  const reply = (c: FeedComment) => {
    setMode({ kind: "reply", comment: c });
    setTimeout(() => input.current?.focus(), 50);
  };
  const edit = (c: FeedComment) => {
    setMode({ kind: "edit", comment: c });
    setTimeout(() => input.current?.focus(), 50);
  };

  if (post.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <View style={{ gap: s[3] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton height={20} width="50%" />
          <Skeleton height={120} />
        </View>
      </Screen>
    );
  }
  if (post.isError) {
    return (
      <Screen edges={["bottom"]}>
        {post.error.status === 404 ? (
          <EmptyState
            icon={MessageCircle}
            title="This post isn't available"
            action={
              <Button variant="secondary" onPress={() => (router.canGoBack() ? router.back() : router.replace("/feed"))}>
                Back to the feed
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load the post" info={errorMessage(post.error)} onRetry={() => void post.refetch()} retrying={post.isRefetching} />
        )}
      </Screen>
    );
  }

  return (
    <Screen
      keyboard
      edges={["bottom"]}
      gap={s[6]}
      refreshControl={<RefreshControl refreshing={comments.isRefetching && !comments.isFetchingNextPage} onRefresh={() => void Promise.all([post.refetch(), comments.refetch()])} />}
      footer={<Composer post={post.data} mode={mode} onMode={setMode} inputRef={input} />}
    >
      <PostCard post={post.data} detail />
      <View style={{ gap: s[3] }}>
        <TitleSm>Comments</TitleSm>
        {comments.isPending ? (
          <View style={{ gap: s[3] }} accessibilityLabel="Loading comments">
            <Skeleton height={48} />
            <Skeleton height={48} />
          </View>
        ) : comments.isError ? (
          <ErrorState compact title="Couldn't load comments" info={errorMessage(comments.error)} onRetry={() => void comments.refetch()} retrying={comments.isRefetching} />
        ) : threads.length === 0 ? (
          <EmptyState compact icon={MessageCircle} title="No comments yet" />
        ) : (
          <View style={{ gap: s[4] }}>
            {threads.map(({ comment, replies }) => (
              <View key={comment.id} style={{ gap: s[3] }}>
                <CommentRow comment={comment} post={post.data} onReply={reply} onEdit={edit} />
                {replies.length > 0 ? (
                  <View style={{ marginLeft: s[4], paddingLeft: s[4], borderLeftWidth: 1, borderLeftColor: theme.colors.border, gap: s[3] }}>
                    {replies.map((r) => (
                      <CommentRow key={r.id} comment={r} post={post.data} onReply={reply} onEdit={edit} />
                    ))}
                  </View>
                ) : null}
              </View>
            ))}
          </View>
        )}
        {comments.hasNextPage ? (
          <Button variant="ghost" size="md" loading={comments.isFetchingNextPage} onPress={() => void comments.fetchNextPage()}>
            Load more comments
          </Button>
        ) : null}
      </View>
    </Screen>
  );
}
