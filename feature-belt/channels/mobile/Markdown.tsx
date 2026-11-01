import { isChannelsEmojiOnly, parseChannelsMarkdown, type ChannelsBlock, type ChannelsInline, type ChannelsUser } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import { Fragment, memo, type ReactNode } from "react";
import { Linking, Platform, View, type TextStyle } from "react-native";

/** The system monospace face for code (palettes ship no mono font). */
const MONO = Platform.OS === "ios" ? "Menlo" : "monospace";

export interface ChannelsMarkdownProps {
  body: string;
  /** People the body's mention tokens name (message.mentions). */
  mentions: readonly ChannelsUser[];
  meId?: string | undefined;
  /** Plain one-block rendering for previews (lists and quotes flattened). */
  numberOfLines?: number;
  tone?: "default" | "muted";
}

/** Only http(s) and mailto links open; anything else stays text. */
function safeHref(href: string): string | null {
  return /^(https?:\/\/|mailto:)/i.test(href) ? href : null;
}

/**
 * A message body rendered from the parsed tree (never as HTML): bold, italic, strike,
 * code, links, mentions, emoji, quotes, lists and code blocks. Bodies of 1–3 emoji show large.
 */
export const ChannelsMarkdown = memo(function ChannelsMarkdown({ body, mentions, meId, numberOfLines, tone = "default" }: ChannelsMarkdownProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const names = new Map(mentions.map((u) => [u.id, u]));
  const base: TextStyle = { color: tone === "muted" ? c.textMuted : c.text };

  const inline = (nodes: ChannelsInline[], key = ""): ReactNode[] =>
    nodes.map((n, i) => {
      const k = `${key}${i}`;
      switch (n.t) {
        case "text":
          return <Fragment key={k}>{n.text}</Fragment>;
        case "br":
          return <Fragment key={k}>{"\n"}</Fragment>;
        case "bold":
          return (
            <Text key={k} role="body" weight={700} style={base}>
              {inline(n.children, `${k}.`)}
            </Text>
          );
        case "italic":
          return (
            <Text key={k} role="body" style={[base, { fontStyle: "italic" }]}>
              {inline(n.children, `${k}.`)}
            </Text>
          );
        case "strike":
          return (
            <Text key={k} role="body" style={[base, { textDecorationLine: "line-through" }]}>
              {inline(n.children, `${k}.`)}
            </Text>
          );
        case "code":
          return (
            <Text key={k} role="label" style={{ fontFamily: MONO, color: c.text, backgroundColor: c.sunken }}>
              {` ${n.text} `}
            </Text>
          );
        case "link": {
          const href = safeHref(n.href);
          return (
            <Text
              key={k}
              role="body"
              accessibilityRole={href ? "link" : undefined}
              onPress={href ? () => void Linking.openURL(href) : undefined}
              style={{ color: c.primaryStrong, textDecorationLine: "underline" }}
            >
              {inline(n.children, `${k}.`)}
            </Text>
          );
        }
        case "mention": {
          const m = n.mention;
          const user = m.kind === "user" ? names.get(m.id) : undefined;
          const label = m.kind === "user" ? `@${user?.display_name ?? "someone"}` : m.kind === "group" ? `@${m.handle}` : `@${m.kind}`;
          const me = m.kind === "user" && m.id === meId;
          return (
            <Text key={k} role="body" weight={600} style={{ color: me ? c.warningStrong : c.primaryStrong, backgroundColor: me ? c.warningSoft : c.primarySoft }}>
              {label}
            </Text>
          );
        }
        case "emoji":
          return (
            <Text key={k} accessibilityLabel={n.name}>
              {n.text}
            </Text>
          );
      }
      return null;
    });

  const blocks = parseChannelsMarkdown(body);
  if (numberOfLines) {
    const flat: ChannelsInline[] = [];
    for (const b of blocks) {
      if (flat.length) flat.push({ t: "text", text: " " });
      if (b.t === "p") flat.push(...b.children);
      else if (b.t === "code") flat.push({ t: "code", text: b.text });
      else if (b.t === "list") b.items.forEach((it, i) => flat.push(...(i ? [{ t: "text" as const, text: ", " }] : []), ...it));
      else for (const q of b.children) if (q.t === "p") flat.push(...q.children);
    }
    return (
      <Text role="body" numberOfLines={numberOfLines} style={base}>
        {inline(flat)}
      </Text>
    );
  }
  if (isChannelsEmojiOnly(body)) {
    return (
      <Text role="headline" style={base}>
        {inline(blocks[0]?.t === "p" ? blocks[0].children : [])}
      </Text>
    );
  }

  const block = (b: ChannelsBlock, i: number): ReactNode => {
    switch (b.t) {
      case "p":
        return (
          <Text key={i} role="body" selectable style={base}>
            {inline(b.children, `${i}.`)}
          </Text>
        );
      case "code":
        return (
          <View key={i} style={{ borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: c.sunken, paddingHorizontal: s[3], paddingVertical: s[2] }}>
            <Text role="label" selectable style={{ fontFamily: MONO, color: c.text }}>
              {b.text}
            </Text>
          </View>
        );
      case "quote":
        return (
          <View key={i} style={{ flexDirection: "row", gap: s[3] }}>
            <View style={{ width: s[1], borderRadius: theme.radius.pill, backgroundColor: c.borderStrong }} />
            <View style={{ flex: 1, gap: s[1] }}>{b.children.map(block)}</View>
          </View>
        );
      case "list":
        return (
          <View key={i} style={{ gap: s[1] }}>
            {b.items.map((it, j) => (
              <View key={j} style={{ flexDirection: "row", gap: s[2] }}>
                <Text role="body" tabular style={[base, { minWidth: s[5] }]}>
                  {b.ordered ? `${b.start + j}.` : "•"}
                </Text>
                <Text role="body" selectable style={[base, { flex: 1 }]}>
                  {inline(it, `${i}.${j}.`)}
                </Text>
              </View>
            ))}
          </View>
        );
    }
    return null;
  };

  return <View style={{ gap: s[2] }}>{blocks.map(block)}</View>;
});
