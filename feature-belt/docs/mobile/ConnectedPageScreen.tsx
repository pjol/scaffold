import {
  docsConnectedHref,
  docsIsTextBlock,
  docsKeyBetween,
  docsPlain,
  docsPlatformName,
  docsReplaceText,
  docsTitle,
  errorMessage,
  generateId,
  isConnectionErrorCode,
  useActiveOrg,
  useDocsConnectedCopy,
  useDocsConnectedPage,
  useDocsConnectedPageSession,
  useDocsConnectedRows,
  type DocsBlock,
  type DocsBlockType,
  type DocsConnectedPageDetail,
  type DocsOp,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, IconButton, ListRow, Screen, Sheet, Skeleton, SkeletonRow, SourceBadge, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Copy, ExternalLink, Lock, MoreVertical, Plus, RefreshCw, Trash2 } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { AppState, Linking, View } from "react-native";
import { BlockList } from "./Blocks";
import { ValueView } from "./DatabaseView";

const LISTY = new Set<DocsBlockType>(["bulleted_list", "numbered_list", "to_do", "toggle"]);

/** docs/c/[conn]/[ref]: a Notion page or Google Doc rendered natively; Notion text and to-dos edit in place. */
export function ConnectedPageScreen() {
  const { conn, ref } = useLocalSearchParams<{ conn: string; ref: string }>();
  const { org } = useActiveOrg();
  const detail = useDocsConnectedPage(conn, ref, org?.id);
  if (detail.isPending) {
    return (
      <Screen edges={["bottom"]} scroll>
        <Stack.Screen options={{ title: "" }} />
        <Skeleton height={36} width="70%" />
        <Skeleton height={16} />
        <Skeleton height={16} width="80%" />
      </Screen>
    );
  }
  if (detail.isError) {
    const e = detail.error;
    const fix = e.details?.["fix_url"] as string | undefined;
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "" }} />
        {e.status === 404 || fix ? (
          <EmptyState
            icon={Lock}
            title={e.status === 404 ? "This page isn't available to this account" : errorMessage(e)}
            {...(e.status === 404 ? { info: errorMessage(e) } : {})}
            {...(fix && isConnectionErrorCode(e.code) && e.code !== "connection_blocked"
              ? {
                  action: (
                    <Button size="md" onPress={() => router.push(fix as never)}>
                      Reconnect
                    </Button>
                  ),
                }
              : {})}
          />
        ) : (
          <ErrorState title="Couldn't load this page." info={errorMessage(e)} onRetry={() => void detail.refetch()} />
        )}
      </Screen>
    );
  }
  return <Loaded key={detail.data.session_id} detail={detail.data} orgId={org?.id ?? null} onReload={() => void detail.refetch()} />;
}

function Rows({ detail }: { detail: DocsConnectedPageDetail }) {
  const theme = useTheme();
  const rows = useDocsConnectedRows(detail.connection_id, detail.ref, { limit: 50 });
  const schema = (detail.page.schema ?? []).filter((p) => p.type !== "title").slice(0, 2);
  if (rows.isPending) return <SkeletonRow />;
  if (rows.isError) return <ErrorState title="Couldn't load the rows." info={errorMessage(rows.error)} onRetry={() => void rows.refetch()} />;
  if (!rows.data.rows.length) return <EmptyState icon={Plus} title="No rows yet" compact />;
  return (
    <View style={{ gap: theme.space[1] }}>
      {rows.data.rows.map((r) => (
        <ListRow
          key={r.id}
          title={`${r.icon ? `${r.icon} ` : ""}${docsTitle(r.title)}`}
          trailing={
            <View style={{ flexDirection: "row", gap: theme.space[2], maxWidth: 180 }}>
              {schema.map((p) => (
                <ValueView key={p.id} prop={p} value={r.props[p.id]} refs={rows.data.refs} />
              ))}
            </View>
          }
          onPress={() => router.push(docsConnectedHref(detail.connection_id, r.ref ?? r.id) as never)}
        />
      ))}
    </View>
  );
}

function Loaded({ detail, orgId, onReload }: { detail: DocsConnectedPageDetail; orgId: string | null; onReload: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const page = detail.page;
  const provider = detail.source.provider;
  const platform = docsPlatformName(provider);
  // Mobile edits plain text and to-dos on Notion; Google Docs shows read-only here.
  const editable = detail.can_edit && provider === "notion";
  const [banner, setBanner] = useState<string | null>(null);
  const { session, snapshot } = useDocsConnectedPageSession(page.kind === "page" ? detail : undefined, orgId, (message, code) => {
    if (code === "changed_on_platform") setBanner(`Changed in ${platform} while you were editing. Your last change wasn't saved; this is the latest version.`);
    else if (code === "connected_session_expired" || isConnectionErrorCode(code)) setBanner(message);
    else toast.error(message);
  });
  const [editingId, setEditingId] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const [blockMenu, setBlockMenu] = useState<DocsBlock | null>(null);
  const copy = useDocsConnectedCopy(detail.connection_id, detail.ref);
  const readOnly = useMemo(() => new Set(detail.read_only_blocks), [detail.read_only_blocks]);
  useEffect(() => {
    const sub = AppState.addEventListener("change", (st) => {
      if (st !== "active") session?.flushNow();
    });
    return () => sub.remove();
  }, [session]);
  const apply = (ops: DocsOp[], key?: string) => session?.apply(ops, key ? { history: "merge", key } : {});
  const positionAfter = (b: DocsBlock) => {
    const sibs = snapshot.children.get(b.parent_id ?? "") ?? [];
    const i = sibs.findIndex((x) => x.id === b.id);
    return docsKeyBetween(b.position, sibs[i + 1]?.position ?? null);
  };
  const onChangeText = (b: DocsBlock, text: string) => {
    if (!session) return;
    const cur = session.block(b.id) ?? b;
    const spans = cur.props.text ?? [];
    const prev = docsPlain(spans);
    if (text === `${prev}\n`) {
      const type: DocsBlockType = LISTY.has(cur.type) ? cur.type : "paragraph";
      const nid = generateId();
      apply([{ op: "create", id: nid, type, parent_id: cur.parent_id, position: positionAfter(cur), props: { text: [], ...(type === "to_do" ? { checked: false } : {}) } }]);
      setEditingId(nid);
      return;
    }
    apply([{ op: "update", id: cur.id, props: { text: docsReplaceText(spans, text) } }], `text:${cur.id}`);
  };
  const onBackspaceEmpty = (b: DocsBlock) => {
    if (!session) return;
    const order = session.getSnapshot().blocks.filter((x) => docsIsTextBlock(x.type) && !readOnly.has(x.id));
    const i = order.findIndex((x) => x.id === b.id);
    if (i <= 0) return;
    apply([{ op: "delete", id: b.id }]);
    setEditingId(order[i - 1]!.id);
  };
  const openPlatform = () => void Linking.openURL(detail.url);
  const name = docsTitle(page.title);
  return (
    <Screen edges={["bottom"]} scroll keyboard refreshControl={undefined}>
      <Stack.Screen options={{ title: name, headerRight: () => <IconButton label="More actions" icon={MoreVertical} onPress={() => setMenu(true)} /> }} />
      {banner ? <Banner tone="warning" title={banner} action={{ label: "Reload", onPress: onReload }} /> : null}
      {snapshot.saveState === "offline" ? <Banner tone="warning" title="Offline. Your changes save when you're back online." /> : null}
      {!detail.connection.can_write ? <Banner tone="info" title={`You can read this here. Allow changes to ${platform} in Settings → Connections to edit it.`} /> : null}
      <View style={{ gap: s[2] }}>
        <SourceBadge source={detail.source} showAccount />
        {page.icon ? <Text role="hero">{page.icon}</Text> : null}
        <Text role="display">{name}</Text>
      </View>
      {page.is_row && detail.database ? (
        <View style={{ gap: s[1], borderBottomWidth: 1, borderBottomColor: c.border, paddingBottom: s[3] }}>
          {detail.database.schema
            .filter((p) => p.type !== "title")
            .map((p) => (
              <View key={p.id} style={{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base }}>
                <Text role="label" tone="muted" numberOfLines={1} style={{ width: 110 }}>
                  {p.name}
                </Text>
                <View style={{ flex: 1 }}>
                  <ValueView prop={p} value={page.props[p.id]} refs={detail.refs} />
                </View>
              </View>
            ))}
        </View>
      ) : null}
      {page.kind === "database" ? (
        <Rows detail={detail} />
      ) : (
        <>
          <BlockList
            children={snapshot.children}
            refs={detail.refs}
            editable={editable}
            readOnly={readOnly}
            orgId={orgId}
            editingId={editingId}
            pageHref={(id) => docsConnectedHref(detail.connection_id, id)}
            onEdit={setEditingId}
            onChangeText={onChangeText}
            onToggleTodo={(b) => apply([{ op: "update", id: b.id, props: { checked: !b.props.checked } }])}
            onBlockMenu={(b) => editable && !readOnly.has(b.id) && setBlockMenu(b)}
            onBackspaceEmpty={onBackspaceEmpty}
          />
          {detail.partial ? (
            <Button variant="secondary" size="md" leftIcon={ExternalLink} onPress={openPlatform}>
              {`Continue in ${platform}`}
            </Button>
          ) : null}
        </>
      )}
      <Sheet open={menu} onClose={() => setMenu(false)} title={name} snapPoints={["45%"]}>
        <View>
          <ListRow title={`Open in ${platform}`} leading={<ExternalLink size={18} color={c.text} />} chevron={false} onPress={() => (setMenu(false), openPlatform())} />
          <ListRow title={`Manage sharing in ${platform}`} leading={<ExternalLink size={18} color={c.text} />} chevron={false} onPress={() => (setMenu(false), openPlatform())} />
          {page.kind === "page" && orgId ? (
            <ListRow
              title="Copy into Docs"
              leading={<Copy size={18} color={c.text} />}
              chevron={false}
              onPress={() => {
                setMenu(false);
                copy.mutate({ org_id: orgId }, { onSuccess: (p) => router.push(`/docs/${p.id}` as never), onError: (e) => toast.error(errorMessage(e, "Couldn't copy it into Docs.")) });
              }}
            />
          ) : null}
          <ListRow title="Reload" leading={<RefreshCw size={18} color={c.text} />} chevron={false} onPress={() => (setMenu(false), onReload())} />
        </View>
      </Sheet>
      <Sheet open={!!blockMenu} onClose={() => setBlockMenu(null)} title="Block" snapPoints={["35%"]}>
        {blockMenu ? (
          <View>
            <ListRow
              title="Add a block below"
              leading={<Plus size={18} color={c.text} />}
              chevron={false}
              onPress={() => {
                const b = blockMenu;
                setBlockMenu(null);
                const nid = generateId();
                apply([{ op: "create", id: nid, type: "paragraph", parent_id: b.parent_id, position: positionAfter(b), props: { text: [] } }]);
                setEditingId(nid);
              }}
            />
            <ListRow title="Delete" destructive leading={<Trash2 size={18} color={c.dangerStrong} />} chevron={false} onPress={() => (apply([{ op: "delete", id: blockMenu.id }]), setBlockMenu(null))} />
          </View>
        ) : null}
      </Sheet>
    </Screen>
  );
}
