import {
  errorMessage,
  filePreviewParseMarkdown,
  filePreviewSafeHref,
  forumAge,
  forumCount,
  forumLabelText,
  forumMentionParts,
  useForumVote,
  useMe,
  type FilePreviewMdBlock,
  type FilePreviewMdInline,
  type ForumAuthor,
  type ForumContentStatus,
  type ForumPostSummary,
  type ForumVoteValue,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Banner, IconButton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { ArrowBigDown, ArrowBigUp, ExternalLink, Lock, MessageSquare, Pin } from "lucide-react-native";
import { useMemo } from "react";
import { Pressable, View } from "react-native";

export const forumPostHref = (id: string) => `/forum/posts/${id}` as Href;
export const forumCommunityHref = (slug: string) => `/forum/c/${slug}` as Href;

export function openForumUrl(url: string): void {
  void WebBrowser.openBrowserAsync(url).catch(() => undefined);
}

function Plain({ text, mentions }: { text: string; mentions: readonly ForumAuthor[] }) {
  const theme = useTheme();
  return (
    <>
      {forumMentionParts(text, mentions).map((p, i) =>
        p.mention ? (
          <Text key={i} role="body" weight={600} style={{ color: theme.colors.primaryStrong }}>
            {p.text}
          </Text>
        ) : (
          p.text
        ),
      )}
    </>
  );
}

function Inline({ nodes, mentions }: { nodes: FilePreviewMdInline[]; mentions: readonly ForumAuthor[] }) {
  const theme = useTheme();
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.type) {
          case "text":
            return <Plain key={i} text={n.text} mentions={mentions} />;
          case "code":
            return (
              <Text key={i} role="label" style={{ backgroundColor: theme.colors.sunken }}>
                {n.text}
              </Text>
            );
          case "strong":
            return (
              <Text key={i} role="body" weight={600}>
                <Inline nodes={n.children} mentions={mentions} />
              </Text>
            );
          case "em":
            return (
              <Text key={i} role="body" style={{ fontStyle: "italic" }}>
                <Inline nodes={n.children} mentions={mentions} />
              </Text>
            );
          case "link": {
            const href = filePreviewSafeHref(n.href);
            return href && /^https?:/i.test(href) ? (
              <Text key={i} role="body" accessibilityRole="link" onPress={() => openForumUrl(href)} style={{ color: theme.colors.primaryStrong, textDecorationLine: "underline" }}>
                <Inline nodes={n.children} mentions={mentions} />
              </Text>
            ) : (
              <Inline key={i} nodes={n.children} mentions={mentions} />
            );
          }
        }
      })}
    </>
  );
}

function Block({ b, mentions }: { b: FilePreviewMdBlock; mentions: readonly ForumAuthor[] }) {
  const theme = useTheme();
  const s = theme.space;
  switch (b.type) {
    case "heading":
      return (
        <Text role={b.level <= 2 ? "titleSm" : "body"} weight={600}>
          <Inline nodes={b.children} mentions={mentions} />
        </Text>
      );
    case "paragraph":
      return (
        <Text role="body">
          <Inline nodes={b.children} mentions={mentions} />
        </Text>
      );
    case "list":
      return (
        <View style={{ gap: s[1] }}>
          {b.items.map((it, i) => (
            <View key={i} style={{ flexDirection: "row", gap: s[2] }}>
              <Text role="body" tone="muted">
                {b.ordered ? `${b.start + i}.` : "•"}
              </Text>
              <Text role="body" style={{ flex: 1 }}>
                <Inline nodes={it} mentions={mentions} />
              </Text>
            </View>
          ))}
        </View>
      );
    case "quote":
      return (
        <View style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md, paddingHorizontal: s[3], paddingVertical: s[2] }}>
          <Text role="body" tone="muted">
            <Inline nodes={b.children} mentions={mentions} />
          </Text>
        </View>
      );
    case "code":
      return (
        <View style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md, padding: s[3] }}>
          <Text role="label">
            {b.text}
          </Text>
        </View>
      );
    case "table":
      return (
        <View style={{ gap: s[1] }}>
          {[b.header, ...b.rows].map((row, i) => (
            <Text key={i} role="label" weight={i === 0 ? 600 : undefined}>
              {row.map((cell, j) => (
                <Text key={j} role="label">
                  {j > 0 ? " · " : ""}
                  <Inline nodes={cell} mentions={mentions} />
                </Text>
              ))}
            </Text>
          ))}
        </View>
      );
    case "rule":
      return <View style={{ height: 1, backgroundColor: theme.colors.border }} />;
  }
}

/** Markdown from a post or comment, as native text (never HTML); "@Name" of mentioned people stands out. */
export function ForumMarkdown({ text, mentions = [] }: { text: string; mentions?: readonly ForumAuthor[] }) {
  const theme = useTheme();
  const blocks = useMemo(() => filePreviewParseMarkdown(text, 400), [text]);
  if (blocks.length === 0) return null;
  return (
    <View style={{ gap: theme.space[3] }}>
      {blocks.map((b, i) => (
        <Block key={i} b={b} mentions={mentions} />
      ))}
    </View>
  );
}

/** Up and down votes with the score between; tapping your vote again clears it. Your own items show the score only. */
export function ForumVoteButtons({
  kind,
  id,
  postId,
  authorId,
  score,
  vote,
  disabled: off,
}: {
  kind: "post" | "comment";
  id: string;
  postId: string;
  authorId: string | null | undefined;
  score: number;
  vote: ForumVoteValue;
  disabled?: boolean;
}) {
  const theme = useTheme();
  const c = theme.colors;
  const me = useMe();
  const v = useForumVote();
  const disabled = off || (!!authorId && me.data?.id === authorId);
  const cast = (value: 1 | -1) =>
    v.mutate({ target_kind: kind, target_id: id, postId, value: vote === value ? 0 : value }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save your vote. Try again.")) });
  return (
    <View style={{ flexDirection: "row", alignItems: "center" }}>
      <IconButton
        label={vote === 1 ? "Remove upvote" : "Upvote"}
        accessibilityState={{ selected: vote === 1, disabled }}
        icon={<ArrowBigUp size={20} strokeWidth={2} color={vote === 1 ? c.primaryStrong : c.textMuted} fill={vote === 1 ? c.primaryStrong : "transparent"} />}
        disabled={disabled}
        onPress={() => cast(1)}
      />
      <Text role="label" weight={600} tabular style={{ minWidth: theme.space[6], textAlign: "center", color: vote === 1 ? c.primaryStrong : vote === -1 ? c.dangerStrong : c.text }}>
        {score}
      </Text>
      <IconButton
        label={vote === -1 ? "Remove downvote" : "Downvote"}
        accessibilityState={{ selected: vote === -1, disabled }}
        icon={<ArrowBigDown size={20} strokeWidth={2} color={vote === -1 ? c.dangerStrong : c.textMuted} fill={vote === -1 ? c.dangerStrong : "transparent"} />}
        disabled={disabled}
        onPress={() => cast(-1)}
      />
    </View>
  );
}

/** Pinned, locked, status and label badges. */
export function ForumBadges({ pinned, locked, status, labels }: { pinned?: boolean; locked?: boolean; status: ForumContentStatus; labels: string[] }) {
  const theme = useTheme();
  if (!pinned && !locked && status === "visible" && labels.length === 0) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[1] }}>
      {pinned ? (
        <Badge size="sm" tone="primary" icon={Pin}>
          Pinned
        </Badge>
      ) : null}
      {locked ? (
        <Badge size="sm" tone="neutral" icon={Lock}>
          Locked
        </Badge>
      ) : null}
      {status === "held" ? (
        <Badge size="sm" tone="warning">
          In review
        </Badge>
      ) : null}
      {status === "removed" ? (
        <Badge size="sm" tone="danger">
          Removed
        </Badge>
      ) : null}
      {labels.map((l) => (
        <Badge key={l} size="sm" tone="neutral">
          {forumLabelText(l)}
        </Badge>
      ))}
    </View>
  );
}

/** Why a held or removed item isn't public, for its author and staff. */
export function ForumModerationBanner({ status, reasons, noun }: { status: ForumContentStatus; reasons: string[]; noun: "post" | "comment" }) {
  if (status !== "held" && status !== "removed") return null;
  const why = reasons.length > 0 ? ` ${reasons.join(". ")}.` : "";
  return status === "held" ? (
    <Banner tone="warning" title={`Waiting for review: only you and moderators can see this ${noun}.${why}`} />
  ) : (
    <Banner tone="danger" title={`Removed by moderators: only you and staff can see this ${noun}.${why}`} />
  );
}

/** One post in a list: votes, where and by whom, title, link domain, excerpt, comments. */
export function ForumPostRow({ post, showCommunity = true }: { post: ForumPostSummary; showCommunity?: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const name = post.author?.display_name || "Deleted user";
  return (
    <View style={{ gap: s[2], paddingVertical: s[4] }}>
      <View style={{ flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: s[2] }}>
        {showCommunity ? (
          <Pressable accessibilityRole="link" hitSlop={8} onPress={() => router.push(forumCommunityHref(post.community.slug))}>
            <Text role="caption" weight={600}>
              {post.community.title}
            </Text>
          </Pressable>
        ) : null}
        {post.author ? <Avatar name={name} src={post.author.avatar_url} size="xs" /> : null}
        <Text role="caption" tone="muted" numberOfLines={1} style={{ flexShrink: 1 }}>
          {`${post.author ? `${name} · ` : ""}${forumAge(post.created_at)}`}
        </Text>
      </View>
      <Pressable accessibilityRole="link" onPress={() => router.push(forumPostHref(post.id))} style={{ gap: s[1] }}>
        <Text role="body" weight={600}>
          {post.title}
        </Text>
        {post.kind === "link" && post.domain ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
            <ExternalLink size={14} color={c.primaryStrong} />
            <Text role="caption" style={{ color: c.primaryStrong }}>
              {post.domain}
            </Text>
          </View>
        ) : null}
        {post.excerpt ? (
          <Text role="label" tone="muted" numberOfLines={2}>
            {post.excerpt}
          </Text>
        ) : null}
      </Pressable>
      <ForumBadges pinned={post.pinned} locked={post.locked} status={post.status} labels={post.labels} />
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <ForumVoteButtons kind="post" id={post.id} postId={post.id} authorId={post.author?.id} score={post.score} vote={post.viewer_vote} disabled={post.status !== "visible"} />
        <Pressable
          accessibilityRole="link"
          onPress={() => router.push(forumPostHref(post.id))}
          style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: s[2] }}
        >
          <MessageSquare size={18} color={c.textMuted} strokeWidth={2} />
          <Text role="label" weight={600} tone="muted">
            {forumCount(post.comment_count, "comment", "comments")}
          </Text>
        </Pressable>
      </View>
    </View>
  );
}
