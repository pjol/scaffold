import {
  errorMessage,
  filesConnectedPath,
  filesFormatSize,
  filesIsTextPreview,
  filesKindLabel,
  filesParseCsv,
  isConnectionErrorCode,
  sourceProductLabel,
  useActiveOrg,
  useApi,
  useDebouncedValue,
  useFilesConnectedChildren,
  useFilesConnectedDrives,
  useFilesConnectedItem,
  useFilesConnectedPreviewUrl,
  useFilesConnectedSearch,
  useFilesConnectedText,
  useFilesCopyToFiles,
  useFilesCreateShortcut,
  useFilesSources,
  type FilesConnectedItem,
  type FilesConnectedSource,
  type PagedQueryResult,
} from "@scaffoldapp/shared";
import {
  Banner,
  Button,
  ConnectPrompt,
  ConnectedSection,
  EmptyState,
  ErrorState,
  Input,
  ListGroup,
  ListRow,
  ProviderIcon,
  Screen,
  Skeleton,
  SkeletonRow,
  SourceBadge,
  Text,
  connectedSectionItems,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router, Stack, useLocalSearchParams } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { Copy, Download, ExternalLink, FolderOpen, HardDrive, Link2, Search, Unplug, Users } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Linking, Pressable, RefreshControl, ScrollView, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { filesOpenConnectedInBrowser, filesSaveOrShareConnected } from "./actions";
import { filesIconTone, filesItemIcon } from "./icons";
import { FILES_MONO, FilesMarkdown } from "./Markdown";

const routeOf = (path: string) => path.slice(1) as never;

function product(item: Pick<FilesConnectedItem, "source">) {
  return sourceProductLabel(item.source.provider, item.source.product);
}

function openLabel(item: FilesConnectedItem) {
  return `Open in ${item.native && item.kind_label ? item.kind_label : product(item)}`;
}

/** Opens the platform's own page: its app when installed (universal links), else the browser. */
async function openOnPlatform(item: FilesConnectedItem) {
  if (!item.web_url) return;
  try {
    await Linking.openURL(item.web_url);
  } catch {
    await WebBrowser.openBrowserAsync(item.web_url);
  }
}

// ---- Files home: the "Connected" section ---------------------------------------------------------

/** Connected accounts on the Files home (with Reconnect), or a quiet prompt when none are. */
export function FilesConnectedHome({ orgId }: { orgId: string | null }) {
  const sources = useFilesSources(orgId);
  const [dismissed, setDismissed] = useState(false);
  const items = connectedSectionItems(sources.data?.sources);
  const offers = sources.data?.offers ?? [];
  if (!sources.isPending && items.length === 0) {
    if (dismissed || offers.length === 0) return null;
    return (
      <ConnectPrompt
        offers={offers}
        title="Connect your drives"
        info="Browse Google Drive, OneDrive and SharePoint here with your own account."
        onConnect={(o) => router.push(routeOf(o.connect_url))}
        onDismiss={() => setDismissed(true)}
      />
    );
  }
  return (
    <ConnectedSection
      items={items}
      loading={sources.isPending}
      onSelect={(id) => router.push(routeOf(filesConnectedPath(id)))}
      onFix={(it) => it.fixUrl && router.push(routeOf(it.fixUrl))}
      {...(offers[0] ? { onAdd: () => router.push(routeOf(offers[0]!.connect_url)) } : {})}
    />
  );
}

// ---- shared pieces ------------------------------------------------------------------------------

function ConnError({ error, onRetry, retrying }: { error: unknown; onRetry: () => void; retrying?: boolean }) {
  const code = (error as { code?: string } | null)?.code ?? "";
  const fix = (error as { details?: Record<string, unknown> } | null)?.details?.["fix_url"] as string | undefined;
  if (isConnectionErrorCode(code) && fix) {
    return <EmptyState icon={Unplug} title={errorMessage(error)} action={<Button variant="secondary" onPress={() => router.push(routeOf(fix))}>{code === "connection_scope_missing" ? "Allow it" : "Reconnect"}</Button>} />;
  }
  return <ErrorState title="Couldn't load this" info={errorMessage(error)} onRetry={onRetry} retrying={!!retrying} />;
}

function useConnectedMenu(item: FilesConnectedItem | null | undefined, orgId: string | null): ResourceMenuAction[] {
  const api = useApi();
  const copy = useFilesCopyToFiles();
  const shortcut = useFilesCreateShortcut(orgId ?? "");
  if (!item) return [];
  const rows: ResourceMenuAction[] = [];
  if (item.web_url) rows.push({ id: "open-platform", label: openLabel(item), icon: ExternalLink, onSelect: () => void openOnPlatform(item) });
  if (item.type === "file" && !(item.native && item.preview === "other")) {
    rows.push({ id: "browser", label: "Open in browser", icon: ExternalLink, onSelect: () => void filesOpenConnectedInBrowser(api.http, item, orgId) });
    rows.push({ id: "save", label: "Save or send a copy", icon: Download, onSelect: () => void filesSaveOrShareConnected(api.http, item, orgId) });
    if (orgId)
      rows.push({
        id: "copy-to-files",
        label: "Copy into Files",
        icon: Copy,
        onSelect: () =>
          copy.mutate(
            { connId: item.connection_id, ref: item.ref, orgId, parentId: null },
            {
              onSuccess: (it) => toast.success(`Copied to My drive as “${it.name}”`, { action: { label: "Open", onPress: () => router.push(routeOf(it.link)) } }),
              onError: (e) => toast.error(errorMessage(e, "Couldn't copy it.")),
            },
          ),
      });
  }
  if (orgId)
    rows.push({
      id: "shortcut",
      label: "Add shortcut to My drive",
      icon: Link2,
      onSelect: () =>
        shortcut.mutate(
          { connId: item.connection_id, ref: item.ref, parentId: null },
          { onSuccess: () => toast.success("Shortcut added to My drive"), onError: (e) => toast.error(errorMessage(e, "Couldn't add the shortcut.")) },
        ),
    });
  return rows;
}

function ConnIcon({ item }: { item: FilesConnectedItem }) {
  const theme = useTheme();
  const [failed, setFailed] = useState(false);
  if (item.thumb_url && !failed)
    return <Image source={{ uri: item.thumb_url }} style={{ width: 36, height: 36, borderRadius: theme.radius.sm }} contentFit="cover" onError={() => setFailed(true)} accessibilityIgnoresInvertColors />;
  const Icon = filesItemIcon(item);
  const tone = filesIconTone(item);
  const color = { primary: theme.colors.primaryStrong, accent: theme.colors.accentStrong, danger: theme.colors.dangerStrong, success: theme.colors.successStrong, muted: theme.colors.textMuted }[tone];
  return <Icon size={26} color={color} />;
}

function ConnRow({ item, orgId }: { item: FilesConnectedItem; orgId: string | null }) {
  const theme = useTheme();
  const actions = useConnectedMenu(item, orgId);
  return (
    <View style={{ flexDirection: "row", alignItems: "center", paddingRight: theme.space[1] }}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${item.name}, ${filesKindLabel(item)}, from ${product(item)}`}
        onPress={() => router.push(routeOf(item.link))}
        style={({ pressed }) => ({ flex: 1, flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.lg, paddingVertical: theme.space[2], paddingLeft: theme.space[4], backgroundColor: pressed ? theme.colors.sunken : undefined })}
      >
        <ConnIcon item={item} />
        <Text weight={500} numberOfLines={1} style={{ flex: 1 }}>
          {item.name}
        </Text>
        <SourceBadge source={item.source} iconOnly />
      </Pressable>
      <ResourceMenu feature="files" kind="files.external" actions={actions} label={`More actions for ${item.name}`} />
    </View>
  );
}

function ConnList({ query, empty, orgId }: { query: PagedQueryResult<FilesConnectedItem>; empty: string; orgId: string | null }) {
  const rows = query.data?.items ?? [];
  return (
    <ListGroup>
      {query.isPending ? (
        <View accessibilityLabel="Loading" accessibilityRole="progressbar">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : query.isError && rows.length === 0 ? (
        <ConnError error={query.error} onRetry={() => void query.refetch()} retrying={query.isRefetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={FolderOpen} title={empty} />
      ) : (
        rows.map((it) => <ConnRow key={it.id} item={it} orgId={orgId} />)
      )}
      {query.hasNextPage ? (
        <Button variant="ghost" loading={query.isFetchingNextPage} onPress={() => void query.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </ListGroup>
  );
}

// ---- one account ---------------------------------------------------------------------------------

/** files/c/[connectionID]: the account's drives. */
export function FilesConnectedAccountScreen() {
  const theme = useTheme();
  const { connectionID } = useLocalSearchParams<{ connectionID: string }>();
  const { org } = useActiveOrg();
  const sources = useFilesSources(org?.id);
  const drives = useFilesConnectedDrives(connectionID, org?.id);
  const source: FilesConnectedSource | undefined = sources.data?.sources.find((s) => s.id === connectionID);
  const list = drives.data?.items ?? [];
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={drives.isRefetching} onRefresh={() => void drives.refetch()} />}>
      <Stack.Screen options={{ title: source?.label ?? "Connected" }} />
      {source?.provider ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          <ProviderIcon provider={source.provider} product={source.product} size={20} />
          <Text role="titleSm" numberOfLines={1} style={{ flex: 1 }}>
            {source.label}
          </Text>
        </View>
      ) : null}
      <ListGroup>
        {drives.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : drives.isError ? (
          <ConnError error={drives.error} onRetry={() => void drives.refetch()} retrying={drives.isRefetching} />
        ) : list.length === 0 ? (
          <EmptyState compact icon={HardDrive} title="No drives in this account" />
        ) : (
          list.map((d) => {
            const Icon = d.kind === "shared_drive" || d.kind === "library" || d.kind === "shared_with_me" ? Users : HardDrive;
            return <ListRow key={d.ref} title={d.site ? `${d.site} · ${d.name}` : d.name} leading={<Icon size={22} color={theme.colors.primaryStrong} />} chevron onPress={() => router.push(routeOf(d.link))} />;
          })
        )}
      </ListGroup>
    </Screen>
  );
}

// ---- one item --------------------------------------------------------------------------------------

/** files/c/[connectionID]/[ref]: a connected folder or a file's preview. */
export function FilesConnectedItemScreen() {
  const { connectionID, ref } = useLocalSearchParams<{ connectionID: string; ref: string }>();
  const { org } = useActiveOrg();
  const orgId = org?.id ?? null;
  const detail = useFilesConnectedItem(connectionID, ref, orgId);
  const item = detail.data?.item;
  const actions = useConnectedMenu(item, orgId);
  if (detail.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton height={320} />
      </Screen>
    );
  }
  if (detail.isError || !item) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Files" }} />
        <ConnError error={detail.error} onRetry={() => void detail.refetch()} retrying={detail.isRefetching} />
      </Screen>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: item.name, headerRight: () => <ResourceMenu feature={`connectors:${item.connection_id}`} actions={actions} /> }} />
      {item.type === "folder" ? <ConnFolder item={item} orgId={orgId} /> : <ConnFile item={item} orgId={orgId} path={detail.data.path.map((c) => c.name)} />}
    </>
  );
}

function ConnFolder({ item, orgId }: { item: FilesConnectedItem; orgId: string | null }) {
  const theme = useTheme();
  const [q, setQ] = useState("");
  const debounced = useDebouncedValue(q, 300);
  const searching = debounced.trim().length > 0;
  const query = useFilesConnectedChildren(item.connection_id, item.ref, { thumbs: true }, orgId);
  const search = useFilesConnectedSearch(searching ? item.connection_id : null, debounced, orgId);
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={query.isRefetching} onRefresh={() => void query.refetch()} />}>
      <View style={{ gap: theme.space[3] }}>
        <SourceBadge source={item.source} showAccount />
        <Input value={q} onChangeText={setQ} placeholder={`Search ${product(item)}`} leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel={`Search ${product(item)}`} returnKeyType="search" />
      </View>
      {searching ? <ConnList query={search} empty={`Nothing in ${product(item)} matches “${debounced.trim()}”`} orgId={orgId} /> : <ConnList query={query} empty="This folder is empty" orgId={orgId} />}
    </Screen>
  );
}

function ConnFile({ item, orgId, path }: { item: FilesConnectedItem; orgId: string | null; path: string[] }) {
  const theme = useTheme();
  const api = useApi();
  const image = item.preview === "image";
  const text = filesIsTextPreview(item.preview) && item.preview !== "svg";
  const url = useFilesConnectedPreviewUrl(item.connection_id, item.ref, orgId, image);
  const content = useFilesConnectedText(item.connection_id, item.ref, orgId, text);
  const platformOnly = item.native && item.preview === "other";
  const rows = useMemo(() => (item.preview === "csv" && content.data ? filesParseCsv(content.data.text, 201).rows : []), [item.preview, content.data]);
  return (
    <Screen
      edges={["bottom"]}
      footer={
        <View style={{ flexDirection: "row", gap: theme.space[2] }}>
          {item.web_url ? (
            <Button style={{ flex: 1 }} variant={platformOnly || item.native ? "primary" : "secondary"} leftIcon={ExternalLink} onPress={() => void openOnPlatform(item)}>
              {openLabel(item)}
            </Button>
          ) : null}
          {!platformOnly ? (
            <Button style={{ flex: 1 }} variant={item.native ? "secondary" : "primary"} leftIcon={Download} onPress={() => void filesSaveOrShareConnected(api.http, item, orgId)}>
              Save or send
            </Button>
          ) : null}
        </View>
      }
    >
      {item.native ? <Banner tone="info" title={`${filesKindLabel(item)} files are read-only here.`} /> : null}
      {image ? (
        url.isPending ? (
          <Skeleton height={320} />
        ) : url.isError ? (
          <ErrorState compact title="Couldn't load the preview" onRetry={() => void url.refetch()} />
        ) : (
          <Image source={{ uri: url.data.url }} style={{ width: "100%", aspectRatio: 1, borderRadius: theme.radius.lg, backgroundColor: theme.colors.sunken }} contentFit="contain" accessibilityLabel={item.name} />
        )
      ) : text ? (
        content.isPending ? (
          <Skeleton height={320} />
        ) : content.isError ? (
          <ConnError error={content.error} onRetry={() => void content.refetch()} />
        ) : item.preview === "markdown" ? (
          <View style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border, padding: theme.space[4] }}>
            <FilesMarkdown text={content.data.text} />
          </View>
        ) : item.preview === "csv" ? (
          <ScrollView horizontal style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface }}>
            <View>
              {rows.map((r, i) => (
                <View key={i} style={{ flexDirection: "row", borderTopWidth: i ? 1 : 0, borderColor: theme.colors.border, backgroundColor: i ? undefined : theme.colors.sunken }}>
                  {r.map((c, j) => (
                    <Text key={j} role="label" weight={i ? undefined : 600} style={{ width: 128, paddingHorizontal: theme.space[3], paddingVertical: theme.space[2] }} numberOfLines={2}>
                      {c}
                    </Text>
                  ))}
                </View>
              ))}
            </View>
          </ScrollView>
        ) : (
          <ScrollView horizontal style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border }} contentContainerStyle={{ padding: theme.space[4] }}>
            <Text role="label" selectable style={{ fontFamily: FILES_MONO }}>
              {content.data.text.split("\n").slice(0, 2000).join("\n")}
            </Text>
          </ScrollView>
        )
      ) : (
        <View style={{ alignItems: "center", gap: theme.space[3], paddingVertical: theme.space[10], backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border }}>
          <ProviderIcon provider={item.source.provider} product={item.source.product} size={32} />
          <Text role="titleSm" align="center">
            {platformOnly ? `${filesKindLabel(item)} open only in ${product(item)}` : `Open to view this ${filesKindLabel(item).toLowerCase()}`}
          </Text>
          {!platformOnly ? (
            <Button variant="secondary" leftIcon={ExternalLink} onPress={() => void filesOpenConnectedInBrowser(api.http, item, orgId)}>
              Open in browser
            </Button>
          ) : null}
        </View>
      )}
      {content.data?.truncated ? <Banner tone="info" title="Showing the first 1 MB." /> : null}
      <Text role="titleSm" accessibilityRole="header">
        Details
      </Text>
      <ListGroup>
        <ListRow title="From" trailing={<SourceBadge source={item.source} showAccount />} />
        <ListRow title="Kind" trailing={<Text tone="muted">{filesKindLabel(item)}</Text>} />
        {item.size > 0 ? <ListRow title="Size" trailing={<Text tone="muted">{filesFormatSize(item.size)}</Text>} /> : null}
        {path.length ? <ListRow title="Folder" trailing={<Text tone="muted" numberOfLines={1}>{path[path.length - 1]}</Text>} /> : null}
        <ListRow title="Modified" trailing={<Text tone="muted">{new Date(item.updated_at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}</Text>} />
      </ListGroup>
    </Screen>
  );
}
