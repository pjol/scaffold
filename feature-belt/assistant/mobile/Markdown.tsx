import { parseMarkdown, safeHref, type MdBlock, type MdInline } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import * as WebBrowser from "expo-web-browser";
import { Fragment, memo, useMemo, type ReactNode } from "react";
import { Linking, ScrollView, View } from "react-native";

function open(href: string) {
  if (!safeHref(href)) return;
  if (href.startsWith("mailto:")) void Linking.openURL(href);
  else void WebBrowser.openBrowserAsync(href);
}

/**
 * Renders an assistant reply from the shared Markdown tree with Text components only: no
 * markup from the model is interpreted, links open in the in-app browser (http/https) or
 * the mail app. Text is selectable for copying.
 */
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const theme = useTheme();
  const blocks = useMemo(() => parseMarkdown(text), [text]);
  return <View style={{ gap: theme.space[3] }}>{blocks.map((b, i) => <Block key={i} block={b} />)}</View>;
});

function Block({ block }: { block: MdBlock }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  switch (block.type) {
    case "paragraph":
      return <Text selectable>{inline(block.children, c.primaryStrong)}</Text>;
    case "heading":
      return (
        <Text selectable role={block.level === 1 ? "titleSm" : "body"} weight={600}>
          {inline(block.children, c.primaryStrong)}
        </Text>
      );
    case "list":
      return (
        <View style={{ gap: s[1] }}>
          {block.items.map((item, i) => (
            <View key={i} style={{ flexDirection: "row", gap: s[2] }}>
              <Text tone="muted" tabular style={{ minWidth: s[5] }}>
                {block.ordered ? `${block.start + i}.` : "•"}
              </Text>
              <Text selectable style={{ flex: 1 }}>
                {inline(item, c.primaryStrong)}
              </Text>
            </View>
          ))}
        </View>
      );
    case "quote":
      return (
        <View style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md, paddingHorizontal: s[4], paddingVertical: s[3] }}>
          <Text selectable tone="muted">
            {inline(block.children, c.primaryStrong)}
          </Text>
        </View>
      );
    case "code":
      return (
        <ScrollView horizontal style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ paddingHorizontal: s[4], paddingVertical: s[3] }}>
          <Text selectable role="label" style={{ fontFamily: "Menlo" }}>
            {block.text}
          </Text>
        </ScrollView>
      );
    case "rule":
      return <View style={{ height: 1, backgroundColor: c.border }} />;
  }
}

function inline(nodes: MdInline[], linkColor: string): ReactNode {
  return nodes.map((n, i) => <Fragment key={i}>{inlineNode(n, linkColor)}</Fragment>);
}

function inlineNode(n: MdInline, linkColor: string): ReactNode {
  switch (n.type) {
    case "text":
      return n.text;
    case "strong":
      return <Text weight={600}>{inline(n.children, linkColor)}</Text>;
    case "em":
      return <Text style={{ fontStyle: "italic" }}>{inline(n.children, linkColor)}</Text>;
    case "code":
      return <Text style={{ fontFamily: "Menlo" }}>{n.text}</Text>;
    case "link":
      return (
        <Text accessibilityRole="link" style={{ color: linkColor, textDecorationLine: "underline" }} onPress={() => open(n.href)}>
          {inline(n.children, linkColor)}
        </Text>
      );
  }
}
