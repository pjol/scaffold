import { docsPlain, docsPlatformName, useDocsResolveConnected, type DocsBlock } from "@scaffoldapp/shared";
import { ProviderIcon, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Linking, Pressable, View } from "react-native";

/**
 * A connected-page block on mobile: the Notion page or Google Doc as this viewer sees it
 * with their own connection (title and opening lines; tap opens it in Docs), or how to
 * get access: connect, reconnect, or ask for access on the platform.
 */
export function ConnectedPageCard({ b, orgId, token }: { b: DocsBlock; orgId: string | null; token: string | null }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const res = useDocsResolveConnected(token ? null : b.props, orgId);
  const platform = docsPlatformName(b.props.provider);
  const title = b.props.title || "Untitled";
  const card = { gap: s[2], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: c.surface } as const;
  const header = (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
      <ProviderIcon provider={b.props.provider ?? ""} {...(b.props.provider === "google" ? { product: "docs" } : {})} size={16} color={c.textMuted} />
      <Text role="label" weight={600} numberOfLines={1} style={{ flex: 1 }}>
        {title}
      </Text>
    </View>
  );
  const openUrl = () => b.props.url && /^https:/i.test(b.props.url) && void Linking.openURL(b.props.url);
  if (token || res.isError) {
    return (
      <Pressable accessibilityRole="link" accessibilityLabel={`${title}, open in ${platform}`} onPress={openUrl} style={({ pressed }) => [card, pressed && { backgroundColor: c.sunken }]}>
        {header}
      </Pressable>
    );
  }
  if (res.isPending) {
    return (
      <View style={card}>
        {header}
        <Skeleton height={14} width="70%" />
      </View>
    );
  }
  const r = res.data;
  if (r.state === "ok" && r.item) {
    const item = r.item;
    const lines = r.blocks
      .map((x) => docsPlain(x.props.text))
      .filter(Boolean)
      .slice(0, 3);
    return (
      <Pressable accessibilityRole="link" accessibilityLabel={`${title}, from ${platform}`} onPress={() => router.push(item.link as never)} style={({ pressed }) => [card, pressed && { backgroundColor: c.sunken }]}>
        {header}
        {lines.map((l, i) => (
          <Text key={i} role="label" tone="muted" numberOfLines={2}>
            {l}
          </Text>
        ))}
      </Pressable>
    );
  }
  const message =
    r.state === "connect"
      ? `Connect ${platform} to view this page here.`
      : r.state === "reconnect"
        ? `Your ${platform} connection needs to be renewed.`
        : r.state === "blocked"
          ? `Your workspace doesn't allow ${platform} here.`
          : `Your ${platform} account can't open this page.`;
  const action =
    r.state === "connect"
      ? { label: `Connect ${platform}`, go: () => r.offer && router.push(r.offer.connect_url as never) }
      : r.state === "reconnect"
        ? { label: "Reconnect", go: () => r.fix_url && router.push(r.fix_url as never) }
        : r.state === "no_access"
          ? { label: "Ask for access", go: openUrl }
          : null;
  return (
    <View style={card}>
      {header}
      <Text role="label" tone="muted">
        {message}
      </Text>
      {action ? (
        <Pressable accessibilityRole="button" onPress={action.go} hitSlop={8} style={{ alignSelf: "flex-start", minHeight: theme.tap.base, justifyContent: "center" }}>
          <Text role="label" weight={600} style={{ color: c.primaryStrong }}>
            {action.label}
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}
