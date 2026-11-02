import {
  docsPlatformName,
  docsTitle,
  errorMessage,
  useActiveOrg,
  useDocsConnectedSearch,
  useDocsConnectedTree,
  useDocsSources,
  type DocsConnectedItem,
} from "@scaffoldapp/shared";
import { EmptyState, ErrorState, Input, Screen, SegmentedControl, SkeletonRow, SourceBadge, Text, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { ChevronDown, ChevronRight, FileText, Search, Table2 } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";

function ItemRow({ item, depth, expandable }: { item: DocsConnectedItem; depth: number; expandable: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const [open, setOpen] = useState(false);
  const kids = useDocsConnectedTree(item.connection_id, { parent: item.id, enabled: open });
  const canOpen = expandable && item.kind === "page";
  return (
    <View>
      <View style={{ flexDirection: "row", alignItems: "center", paddingLeft: depth * theme.space[4] }}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={open ? `Collapse ${docsTitle(item.title)}` : `Expand ${docsTitle(item.title)}`}
          accessibilityState={{ expanded: open }}
          disabled={!canOpen}
          onPress={() => setOpen((o) => !o)}
          hitSlop={8}
          style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center", opacity: canOpen ? 1 : 0 }}
        >
          {open ? <ChevronDown size={18} color={c.textMuted} /> : <ChevronRight size={18} color={c.textMuted} />}
        </Pressable>
        <Pressable
          accessibilityRole="link"
          onPress={() => router.push(item.link as never)}
          style={({ pressed }) => ({ flex: 1, flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: theme.tap.base, backgroundColor: pressed ? c.sunken : "transparent", borderRadius: theme.radius.sm })}
        >
          {item.icon ? <Text role="body">{item.icon}</Text> : item.kind === "database" ? <Table2 size={18} color={c.textMuted} /> : <FileText size={18} color={c.textMuted} />}
          <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
            {docsTitle(item.title)}
          </Text>
          <SourceBadge source={item.source} iconOnly />
        </Pressable>
      </View>
      {open ? (
        kids.isPending ? (
          <SkeletonRow />
        ) : kids.data?.items.length ? (
          kids.data.items.map((k) => <ItemRow key={k.id} item={k} depth={depth + 1} expandable />)
        ) : (
          <Text role="caption" tone="subtle" style={{ paddingLeft: (depth + 1) * theme.space[4] + theme.tap.base }}>
            No pages inside
          </Text>
        )
      ) : null}
    </View>
  );
}

/** docs/c/[conn]: one connected account: search, then its tree (Notion) or Recent / Shared with me (Google). */
export function ConnectedSourceScreen() {
  const theme = useTheme();
  const { conn } = useLocalSearchParams<{ conn: string }>();
  const { org } = useActiveOrg();
  const sources = useDocsSources(org?.id);
  const source = sources.data?.sources.find((x) => x.id === conn);
  const [q, setQ] = useState("");
  const [section, setSection] = useState<"recent" | "shared">("recent");
  const google = source?.provider === "google";
  const tree = useDocsConnectedTree(conn, { ...(google ? { section } : {}), orgId: org?.id, enabled: !!source });
  const results = useDocsConnectedSearch(conn, q, org?.id);
  const platform = docsPlatformName(source?.provider);
  const title = source ? `${platform} · ${source.account ?? ""}` : "Connected";
  return (
    <Screen edges={["bottom"]} scroll refreshControl={<RefreshControl refreshing={tree.isRefetching} onRefresh={() => void tree.refetch()} />}>
      <Stack.Screen options={{ title }} />
      {sources.isPending ? (
        <SkeletonRow />
      ) : !source ? (
        <EmptyState icon={FileText} title="This connected account isn't available" />
      ) : (
        <View style={{ gap: theme.space[3] }}>
          <Input value={q} onChangeText={setQ} placeholder={`Search ${platform}`} accessibilityLabel={`Search ${platform}`} leadingIcon={Search} returnKeyType="search" />
          {q.trim() ? (
            results.isPending ? (
              <SkeletonRow />
            ) : results.isError ? (
              <ErrorState title={errorMessage(results.error, `Couldn't search ${platform}.`)} onRetry={() => void results.refetch()} />
            ) : results.data.length === 0 ? (
              <Text role="label" tone="muted">
                Nothing matches in {platform}.
              </Text>
            ) : (
              results.data.map((it) => <ItemRow key={it.id} item={it} depth={0} expandable={false} />)
            )
          ) : (
            <>
              {google ? (
                <SegmentedControl<"recent" | "shared">
                  value={section}
                  onValueChange={setSection}
                  options={[
                    { value: "recent", label: "Recent" },
                    { value: "shared", label: "Shared with me" },
                  ]}
                />
              ) : null}
              {tree.isPending ? (
                <>
                  <SkeletonRow />
                  <SkeletonRow />
                </>
              ) : tree.isError ? (
                <ErrorState title={errorMessage(tree.error, `Couldn't load ${platform}.`)} onRetry={() => void tree.refetch()} />
              ) : tree.data.items.length === 0 ? (
                <EmptyState
                  icon={FileText}
                  title={google ? "No documents yet" : "No pages shared with this app yet"}
                  {...(google ? {} : { info: "In Notion, open a page's ••• menu → Connections and add this app." })}
                />
              ) : (
                tree.data.items.map((it) => <ItemRow key={it.id} item={it} depth={0} expandable={!google} />)
              )}
              {tree.hasNextPage ? (
                <Pressable accessibilityRole="button" onPress={() => void tree.fetchNextPage()} style={{ minHeight: theme.tap.base, justifyContent: "center" }}>
                  <Text role="label" tone="muted">
                    {tree.isFetchingNextPage ? "Loading…" : "Show more"}
                  </Text>
                </Pressable>
              ) : null}
            </>
          )}
        </View>
      )}
    </Screen>
  );
}
