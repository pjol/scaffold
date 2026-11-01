import { errorMessage, feedKeys, isPinned, useDeleteFeedPost, useMe, useQueryClient, useReactToFeedPost, type FeedPost } from "@scaffoldapp/shared";
import { Avatar, Badge, Card, ConfirmDialog, IconButton, ListRow, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Ban, Flag, Link2, Megaphone, MessageCircle, MoreHorizontal, Pencil, Pin, Share2, Trash2 } from "lucide-react-native";
import { useRef, useState } from "react";
import { Pressable, Share, View } from "react-native";
import { BlockDialog, ReportDialog } from "@/features/moderation";
import { shareableLink } from "./links";
import { MediaGrid, openUrl, ReactButton, ReactionSummary, RichText } from "./parts";
import { timeAgo } from "./time";

export const postHref = (id: string) => `/feed/posts/${id}` as const;

type Next = "share" | "edit" | "delete" | "report" | "block" | null;

/** One post: author line, text, link, media, reactions, the comments link (left out on its own page) and an actions sheet. */
export function PostCard({ post, detail }: { post: FeedPost; detail?: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const me = useMe();
  const qc = useQueryClient();
  const react = useReactToFeedPost();
  const del = useDeleteFeedPost();
  const [actions, setActions] = useState(false);
  const [dialog, setDialog] = useState<Next>(null);
  const next = useRef<Next>(null);
  const mine = me.data?.id === post.author.id;
  const name = post.author.display_name || "Deleted user";
  const pinned = isPinned(post);
  const hidden = () => void qc.invalidateQueries({ queryKey: feedKeys.all() });

  const choose = (n: Next) => {
    next.current = n;
    setActions(false);
  };

  return (
    <Card variant={pinned ? "raised" : "surface"} padding="card" style={{ gap: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "flex-start", gap: s[3] }}>
        <Avatar name={name} src={post.author.avatar_url} size="md" />
        <View style={{ flex: 1, gap: s[0.5] }}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
            <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
              {name}
            </Text>
            {post.kind === "announcement" ? (
              <Badge size="sm" tone="primary" icon={pinned ? Pin : Megaphone}>
                {pinned ? "Pinned" : "Announcement"}
              </Badge>
            ) : null}
          </View>
          <Text role="caption" tone="muted">
            {timeAgo(post.created_at)}
            {post.edited_at ? " · Edited" : ""}
          </Text>
        </View>
        <IconButton label="Post options" icon={MoreHorizontal} variant="ghost" onPress={() => setActions(true)} />
      </View>

      <RichText text={post.body} mentions={post.mentions} />

      {post.link_url ? (
        <Pressable
          accessibilityRole="link"
          onPress={() => openUrl(post.link_url ?? "")}
          style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[3], paddingHorizontal: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border }}
        >
          <Link2 size={18} color={c.textMuted} strokeWidth={2} />
          <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
            {post.link_url}
          </Text>
        </Pressable>
      ) : null}

      {post.media.length > 0 ? <MediaGrid media={post.media} /> : null}

      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], borderTopWidth: 1, borderTopColor: c.border, paddingTop: s[1] }}>
        <ReactButton mine={post.my_reaction} onReact={(reaction) => react.mutate({ post, reaction }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your reaction. Try again.")) })} />
        {detail ? null : (
          <Pressable
            accessibilityRole="button"
            onPress={() => router.push(postHref(post.id))}
            hitSlop={4}
            style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: s[2] }}
          >
            <MessageCircle size={18} color={c.textMuted} strokeWidth={2} />
            <Text role="label" weight={600} tone="muted">
              {post.comment_count === 1 ? "1 comment" : post.comment_count > 0 ? `${post.comment_count} comments` : "Comment"}
            </Text>
          </Pressable>
        )}
        <View style={{ flex: 1 }} />
        <ReactionSummary reactions={post.reactions} total={post.reaction_count} />
      </View>

      <Sheet
        open={actions}
        onClose={() => setActions(false)}
        onDismissed={() => {
          const n = next.current;
          next.current = null;
          if (n === "edit") router.push({ pathname: "/feed/compose", params: { id: post.id } });
          else if (n === "share") void Share.share({ message: shareableLink(postHref(post.id)) }).catch(() => toast.error("Couldn't open sharing. Try again."));
          else if (n) setDialog(n);
        }}
        title="Post"
      >
        <View style={{ gap: s[1] }}>
          <ListRow title="Share post" leading={<Share2 size={20} color={c.textMuted} />} onPress={() => choose("share")} />
          {mine ? (
            <>
              <ListRow title="Edit" leading={<Pencil size={20} color={c.textMuted} />} onPress={() => choose("edit")} />
              <ListRow title="Delete" destructive leading={<Trash2 size={20} color={c.dangerStrong} />} onPress={() => choose("delete")} />
            </>
          ) : (
            <>
              <ListRow title="Report post" leading={<Flag size={20} color={c.textMuted} />} onPress={() => choose("report")} />
              <ListRow title={`Block ${name}`} destructive leading={<Ban size={20} color={c.dangerStrong} />} onPress={() => choose("block")} />
            </>
          )}
        </View>
      </Sheet>
      <ConfirmDialog
        open={dialog === "delete"}
        onClose={() => setDialog(null)}
        title="Delete this post?"
        tone="danger"
        confirmLabel="Delete"
        onConfirm={async () => {
          await del.mutateAsync(post.id);
          toast.success("Post deleted");
          if (detail) router.back();
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the post. Try again.")}
      >
        <Text role="body">Its comments and reactions go with it. It can't be undone.</Text>
      </ConfirmDialog>
      {!mine ? (
        <>
          <ReportDialog
            open={dialog === "report"}
            onClose={() => setDialog(null)}
            targetKind="feed.post"
            targetId={post.id}
            noun="post"
            author={{ id: post.author.id, name }}
            onReported={({ blocked }) => blocked && hidden()}
          />
          <BlockDialog open={dialog === "block"} onClose={() => setDialog(null)} userId={post.author.id} name={name} onBlocked={hidden} />
        </>
      ) : null}
    </Card>
  );
}
