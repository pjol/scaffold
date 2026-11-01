import { docsTitle, orderBlocks, useDocsPublicPage } from "@scaffoldapp/shared";
import { EmptyState, ErrorState, ListRow, Screen, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Link2Off } from "lucide-react-native";
import { useMemo } from "react";
import { View } from "react-native";
import { BlockList } from "./Blocks";
import { DatabaseView, ValueView } from "./DatabaseView";

/** p/[token]?page=<id>: a published page, read-only, without an account. */
export function PublicPageScreen() {
  const theme = useTheme();
  const { token, page: pageId } = useLocalSearchParams<{ token: string; page?: string }>();
  const q = useDocsPublicPage(token, pageId ?? null);
  const children = useMemo(() => orderBlocks(new Map((q.data?.blocks ?? []).map((b) => [b.id, b]))).children, [q.data?.blocks]);
  if (q.isPending) {
    return (
      <Screen edges={["bottom"]} scroll>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton height={36} width="70%" />
        <Skeleton height={16} />
      </Screen>
    );
  }
  if (q.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "" }} />
        {q.error.status === 404 ? <EmptyState icon={Link2Off} title="This link doesn't work anymore" /> : <ErrorState title="Couldn't load this page." onRetry={() => void q.refetch()} />}
      </Screen>
    );
  }
  const d = q.data;
  const href = (id: string) => (d.refs.pages[id]?.title ? `/p/${token}?page=${id}` : null);
  return (
    <Screen edges={["bottom"]} scroll>
      <Stack.Screen options={{ title: docsTitle(d.page.title) }} />
      {d.page.icon ? <Text role="hero">{d.page.icon}</Text> : null}
      <Text role="display">{docsTitle(d.page.title)}</Text>
      {d.page.is_row && d.database ? (
        <View style={{ gap: theme.space[1] }}>
          {d.database.schema
            .filter((p) => p.type !== "title")
            .map((p) => (
              <View key={p.id} style={{ flexDirection: "row", gap: theme.space[3], minHeight: theme.tap.sm, alignItems: "center" }}>
                <Text role="label" tone="muted" style={{ width: 110 }}>
                  {p.name}
                </Text>
                <View style={{ flex: 1 }}>
                  <ValueView prop={p} value={d.page.props[p.id]} refs={d.refs} />
                </View>
              </View>
            ))}
        </View>
      ) : null}
      {d.page.kind === "database" ? <DatabaseView db={d.page} views={d.views} editable={false} token={token} /> : <BlockList children={children} refs={d.refs} editable={false} token={token} pageHref={href} />}
      {d.children.length ? (
        <View>
          {d.children.map((c) => (
            <ListRow key={c.id} title={`${c.icon ? `${c.icon} ` : ""}${docsTitle(c.title)}`} onPress={() => router.push(`/p/${token}?page=${c.id}` as never)} />
          ))}
        </View>
      ) : null}
    </Screen>
  );
}
