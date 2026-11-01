import { filesParseMarkdown, type FilesMdBlock, type FilesMdInline } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import * as Linking from "expo-linking";
import { Fragment, useMemo, type ReactNode } from "react";
import { Platform, ScrollView, View } from "react-native";

/** The system monospace face. */
export const FILES_MONO = Platform.select({ ios: "Menlo", default: "monospace" });

function Inline({ nodes }: { nodes: FilesMdInline[] }): ReactNode {
  return nodes.map((n, i) => {
    switch (n.type) {
      case "text":
        return <Fragment key={i}>{n.text}</Fragment>;
      case "code":
        return (
          <Text key={i} style={{ fontFamily: FILES_MONO }}>
            {n.text}
          </Text>
        );
      case "strong":
        return (
          <Text key={i} weight={600}>
            <Inline nodes={n.children} />
          </Text>
        );
      case "em":
        return (
          <Text key={i} style={{ fontStyle: "italic" }}>
            <Inline nodes={n.children} />
          </Text>
        );
      case "link":
        return (
          <Text key={i} tone="primary" accessibilityRole="link" onPress={() => void Linking.openURL(n.href)} style={{ textDecorationLine: "underline" }}>
            <Inline nodes={n.children} />
          </Text>
        );
    }
  });
}

const HEADING = ["headline", "title", "titleSm", "bodyLg", "body", "body"] as const;

function Block({ block }: { block: FilesMdBlock }) {
  const theme = useTheme();
  switch (block.type) {
    case "heading":
      return (
        <Text role={HEADING[block.level - 1]} weight={600} accessibilityRole="header">
          <Inline nodes={block.children} />
        </Text>
      );
    case "paragraph":
      return (
        <Text>
          <Inline nodes={block.children} />
        </Text>
      );
    case "list":
      return (
        <View style={{ gap: theme.space[1] }}>
          {block.items.map((item, i) => (
            <View key={i} style={{ flexDirection: "row", gap: theme.space[2] }}>
              <Text tone="muted">{block.ordered ? `${block.start + i}.` : "•"}</Text>
              <Text style={{ flex: 1 }}>
                <Inline nodes={item} />
              </Text>
            </View>
          ))}
        </View>
      );
    case "quote":
      return (
        <View style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md, padding: theme.space[3] }}>
          <Text tone="muted">
            <Inline nodes={block.children} />
          </Text>
        </View>
      );
    case "code":
      return (
        <ScrollView horizontal style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ padding: theme.space[3] }}>
          <Text role="label" style={{ fontFamily: FILES_MONO }}>
            {block.text}
          </Text>
        </ScrollView>
      );
    case "table":
      return (
        <ScrollView horizontal>
          <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md }}>
            {[block.header, ...block.rows].map((row, r) => (
              <View key={r} style={{ flexDirection: "row", borderTopWidth: r ? 1 : 0, borderColor: theme.colors.border, backgroundColor: r ? undefined : theme.colors.sunken }}>
                {row.map((cell, c) => (
                  <Text key={c} role="label" weight={r ? undefined : 600} style={{ minWidth: 96, paddingHorizontal: theme.space[3], paddingVertical: theme.space[2] }}>
                    <Inline nodes={cell} />
                  </Text>
                ))}
              </View>
            ))}
          </View>
        </ScrollView>
      );
    case "rule":
      return <View style={{ height: 1, backgroundColor: theme.colors.border }} />;
  }
}

/** Markdown as native text (never HTML). */
export function FilesMarkdown({ text }: { text: string }) {
  const theme = useTheme();
  const blocks = useMemo(() => filesParseMarkdown(text), [text]);
  return (
    <View style={{ gap: theme.space[3] }}>
      {blocks.map((b, i) => (
        <Block key={i} block={b} />
      ))}
    </View>
  );
}
