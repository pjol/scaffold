import type { ReposLabel, ReposMention, ReposReaction, ReposReactionEmoji } from "@scaffoldapp/shared";
import { Badge, Sheet, Text, useTheme } from "@scaffoldapp/ui-native";
import { SmilePlus } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";
import { MONO, RepoMarkdown } from "./parts";

/** `<@uuid>` mention tokens written as bold @Names (unknown people as @someone). */
export function withMentions(body: string, mentions: ReposMention[] | undefined): string {
  const names = new Map((mentions ?? []).map((m) => [m.id, m.display_name]));
  return body.replace(/<@([0-9a-f-]{36})>/g, (_m, id: string) => `**@${names.get(id) ?? "someone"}**`);
}

const SUGGESTION = /(^|\n)```suggestion[ \t]*\r?\n([\s\S]*?)(?:\r?\n)?```/;

/**
 * A review comment: Markdown, with its ```suggestion block shown as the lines it would
 * write (applying it happens on the web).
 */
export function CommentBody({ body, repoId, gitRef, mentions, applied }: { body: string; repoId: string; gitRef?: string | undefined; mentions?: ReposMention[] | undefined; applied?: boolean }) {
  const theme = useTheme();
  const m = SUGGESTION.exec(body);
  if (!m) return <RepoMarkdown source={withMentions(body, mentions)} repoId={repoId} gitRef={gitRef} dir="" />;
  const before = body.slice(0, m.index + (m[1] ? 1 : 0)).trim();
  const after = body.slice(m.index + m[0].length).trim();
  const lines = m[2] === "" ? [] : m[2]!.replace(/\r\n/g, "\n").split("\n");
  return (
    <View style={{ gap: theme.space[2] }}>
      {before ? <RepoMarkdown source={withMentions(before, mentions)} repoId={repoId} gitRef={gitRef} dir="" /> : null}
      <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md, overflow: "hidden" }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], paddingHorizontal: theme.space[3], paddingVertical: theme.space[2], backgroundColor: theme.colors.sunken }}>
          <Text role="caption" weight={600} style={{ flexShrink: 1 }}>
            Suggested change
          </Text>
          {applied ? <Badge tone="success">Applied</Badge> : null}
        </View>
        {lines.length === 0 ? (
          <Text role="caption" tone="muted" style={{ paddingHorizontal: theme.space[3], paddingVertical: theme.space[2] }}>
            Deletes these lines.
          </Text>
        ) : (
          lines.map((line, i) => (
            <View key={i} style={{ flexDirection: "row", gap: theme.space[2], paddingHorizontal: theme.space[3], backgroundColor: theme.colors.successSoft }}>
              <Text role="caption" style={{ fontFamily: MONO, color: theme.colors.successStrong }} accessibilityLabel="Added line">
                +
              </Text>
              <Text role="caption" selectable style={{ flex: 1, fontFamily: MONO, color: theme.colors.successStrong }}>
                {line || " "}
              </Text>
            </View>
          ))
        )}
      </View>
      {after ? <RepoMarkdown source={withMentions(after, mentions)} repoId={repoId} gitRef={gitRef} dir="" /> : null}
    </View>
  );
}

/** A label as a Badge in its token tone. */
export function LabelBadge({ label }: { label: ReposLabel }) {
  return <Badge tone={label.color}>{label.name}</Badge>;
}

export const REACTION_GLYPHS: Record<ReposReactionEmoji, string> = {
  "+1": "👍",
  "-1": "👎",
  laugh: "😄",
  hooray: "🎉",
  confused: "😕",
  heart: "❤️",
  rocket: "🚀",
  eyes: "👀",
};

const REACTION_NAMES: Record<ReposReactionEmoji, string> = {
  "+1": "Thumbs up",
  "-1": "Thumbs down",
  laugh: "Laugh",
  hooray: "Hooray",
  confused: "Confused",
  heart: "Heart",
  rocket: "Rocket",
  eyes: "Eyes",
};

/** Reaction counts; with `onToggle`, tapping one adds or removes yours and ＋ picks a new one. */
export function Reactions({ items, onToggle }: { items: ReposReaction[] | undefined; onToggle?: ((emoji: ReposReactionEmoji, add: boolean) => void) | undefined }) {
  const theme = useTheme();
  const [picking, setPicking] = useState(false);
  const list = items ?? [];
  if (list.length === 0 && !onToggle) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
      {list.map((r) => (
        <Pressable
          key={r.emoji}
          disabled={!onToggle}
          onPress={() => onToggle?.(r.emoji, !r.me)}
          accessibilityRole="button"
          accessibilityState={{ selected: r.me }}
          accessibilityLabel={`${REACTION_NAMES[r.emoji]}: ${r.count}${r.me ? ", including you" : ""}`}
          style={{
            flexDirection: "row",
            alignItems: "center",
            gap: theme.space[1],
            minHeight: theme.tap.sm,
            paddingHorizontal: theme.space[3],
            borderRadius: theme.radius.pill,
            borderWidth: 1,
            borderColor: r.me ? theme.colors.primary : theme.colors.border,
            backgroundColor: r.me ? theme.colors.primarySoft : theme.colors.surface,
          }}
        >
          <Text role="label">{REACTION_GLYPHS[r.emoji]}</Text>
          <Text role="label" tabular tone={r.me ? "primary" : "muted"}>
            {r.count}
          </Text>
        </Pressable>
      ))}
      {onToggle ? (
        <Pressable
          onPress={() => setPicking(true)}
          accessibilityRole="button"
          accessibilityLabel="Add a reaction"
          style={{ alignItems: "center", justifyContent: "center", minHeight: theme.tap.sm, minWidth: theme.tap.base, borderRadius: theme.radius.pill, borderWidth: 1, borderColor: theme.colors.border }}
        >
          <SmilePlus size={18} color={theme.colors.textMuted} />
        </Pressable>
      ) : null}
      {onToggle ? (
        <Sheet open={picking} onClose={() => setPicking(false)} title="React" snapPoints={["30%"]}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
            {(Object.keys(REACTION_GLYPHS) as ReposReactionEmoji[]).map((e) => {
              const mine = list.find((r) => r.emoji === e)?.me ?? false;
              return (
                <Pressable
                  key={e}
                  onPress={() => {
                    onToggle(e, !mine);
                    setPicking(false);
                  }}
                  accessibilityRole="button"
                  accessibilityLabel={REACTION_NAMES[e]}
                  accessibilityState={{ selected: mine }}
                  style={{
                    alignItems: "center",
                    justifyContent: "center",
                    width: theme.tap.base,
                    height: theme.tap.base,
                    borderRadius: theme.radius.md,
                    backgroundColor: mine ? theme.colors.primarySoft : theme.colors.sunken,
                  }}
                >
                  <Text role="title">{REACTION_GLYPHS[e]}</Text>
                </Pressable>
              );
            })}
          </View>
        </Sheet>
      ) : null}
    </View>
  );
}
