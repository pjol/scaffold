import {
  DOCS_BLOCK_LABEL,
  docsIsTextBlock,
  docsKeyBetween,
  docsPlain,
  docsReplaceText,
  docsShortcut,
  docsSlice,
  docsTitle,
  errorMessage,
  generateId,
  useDocsChildren,
  useDocsPage,
  useDocsPageSession,
  useDocsPresence,
  useDocsRestorePage,
  useDocsSetFavorite,
  useDocsSetRowValues,
  useDocsTrashPage,
  useDocsUpdatePage,
  useMe,
  type DocsBlock,
  type DocsBlockType,
  type DocsOp,
  type DocsPageDetail,
  type DocsProperty,
  type DocsSessionSnapshot,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { AvatarGroup, Banner, Button, EmptyState, ErrorState, IconButton, ListRow, Screen, Sheet, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { ArrowDown, ArrowUp, CloudOff, FileText, Lock, MessageSquare, Plus, Star, Trash2, LockOpen } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { AppState, Pressable, Share, TextInput, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { BlockList } from "./Blocks";
import { DatabaseView, ValueView } from "./DatabaseView";
import { PropertySheet } from "./PropertySheet";

const LISTY = new Set<DocsBlockType>(["bulleted_list", "numbered_list", "to_do", "toggle"]);
const TURN_INTO: DocsBlockType[] = ["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"];

function siblings(snap: DocsSessionSnapshot, parent: string | null) {
  return snap.children.get(parent ?? "") ?? [];
}

function positionAfter(snap: DocsSessionSnapshot, b: DocsBlock): string {
  const sibs = siblings(snap, b.parent_id);
  const i = sibs.findIndex((x) => x.id === b.id);
  return docsKeyBetween(b.position, sibs[i + 1]?.position ?? null);
}

/** docs/[id]: read and edit a page, database or row. */
export function PageScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const detail = useDocsPage(id);
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
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "" }} />
        {detail.error.status === 404 ? <EmptyState icon={Lock} title="This page doesn't exist, or you can't open it" /> : <ErrorState title="Couldn't load this page." info={errorMessage(detail.error)} onRetry={() => void detail.refetch()} />}
      </Screen>
    );
  }
  return <Loaded key={id} detail={detail.data} />;
}

function RowProperties({ detail, editable }: { detail: DocsPageDetail; editable: boolean }) {
  const theme = useTheme();
  const db = detail.database!;
  const setValues = useDocsSetRowValues(db.id);
  const [editing, setEditing] = useState<DocsProperty | null>(null);
  const page = detail.page;
  return (
    <View style={{ gap: theme.space[1], borderBottomWidth: 1, borderBottomColor: theme.colors.border, paddingBottom: theme.space[3] }}>
      {db.schema
        .filter((p) => p.type !== "title")
        .map((p) => (
          <Pressable
            key={p.id}
            accessibilityRole="button"
            disabled={!editable}
            onPress={() => (p.type === "checkbox" ? setValues.mutate({ rowId: page.id, values: { [p.id]: page.props[p.id] !== true } }) : setEditing(p))}
            style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.base, backgroundColor: pressed ? theme.colors.sunken : "transparent" })}
          >
            <Text role="label" tone="muted" numberOfLines={1} style={{ width: 110 }}>
              {p.name}
            </Text>
            <View style={{ flex: 1 }}>
              <ValueView prop={p} value={page.props[p.id]} refs={detail.refs} />
            </View>
          </Pressable>
        ))}
      <PropertySheet
        open={!!editing}
        prop={editing}
        value={editing ? page.props[editing.id] : undefined}
        orgId={page.org_id}
        onClose={() => setEditing(null)}
        onSave={(v) => {
          const p = editing;
          setEditing(null);
          if (p) setValues.mutate({ rowId: page.id, values: { [p.id]: v } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that value.")) });
        }}
      />
    </View>
  );
}

function Loaded({ detail }: { detail: DocsPageDetail }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const page = detail.page;
  const level = page.my_level;
  const trashed = !!page.deleted_at;
  const canEdit = level === "edit" || level === "manage";
  const editable = canEdit && !page.locked && !trashed;
  const { session, snapshot } = useDocsPageSession(page.id, detail, (m) => toast.error(m));
  const [editingId, setEditingId] = useState<string | null>(null);
  const [active, setActive] = useState(true);
  const me = useMe();
  const viewers = useDocsPresence(page.id, editingId, active && !trashed).filter((v) => v.user.id !== me.data?.id);
  const [menuBlock, setMenuBlock] = useState<DocsBlock | null>(null);
  const [title, setTitle] = useState(page.title);
  const update = useDocsUpdatePage(page.id);
  const favorite = useDocsSetFavorite(page.id);
  const trash = useDocsTrashPage(page.org_id);
  const restore = useDocsRestorePage(page.org_id);
  const kids = useDocsChildren(page.id, page.kind === "page");
  useEffect(() => setTitle(page.title), [page.title]);
  useEffect(() => {
    const sub = AppState.addEventListener("change", (st) => {
      setActive(st === "active");
      if (st !== "active") session?.flushNow();
    });
    return () => sub.remove();
  }, [session]);
  const viewersByBlock = useMemo(() => {
    const m = new Map<string, DocsViewer[]>();
    for (const v of viewers) if (v.block_id) m.set(v.block_id, [...(m.get(v.block_id) ?? []), v]);
    return m;
  }, [viewers]);
  const apply = (ops: DocsOp[], key?: string) => session?.apply(ops, key ? { history: "merge", key } : {});

  const onChangeText = (b: DocsBlock, text: string) => {
    if (!session) return;
    const cur = session.block(b.id) ?? b;
    const spans = cur.props.text ?? [];
    const prev = docsPlain(spans);
    const snap = session.getSnapshot();
    if (text === `${prev}\n`) {
      const type: DocsBlockType = LISTY.has(cur.type) ? cur.type : "paragraph";
      const nid = generateId();
      apply([{ op: "create", id: nid, type, parent_id: cur.parent_id, position: positionAfter(snap, cur), props: { text: [], ...(type === "to_do" ? { checked: false } : {}) } }]);
      setEditingId(nid);
      return;
    }
    if (cur.type === "paragraph" && text.length === prev.length + 1 && text.endsWith(" ") && !prev.includes(" ")) {
      const sc = docsShortcut(text.trim());
      if (sc && sc.type !== "divider" && sc.type !== "code") {
        apply([{ op: "update", id: cur.id, type: sc.type, props: { ...(sc.props ?? {}), text: docsSlice(spans, text.length) } }]);
        return;
      }
    }
    apply([{ op: "update", id: cur.id, props: { text: docsReplaceText(spans, text) } }], `text:${cur.id}`);
  };

  const onBackspaceEmpty = (b: DocsBlock) => {
    if (!session) return;
    const order = session.getSnapshot().blocks.filter((x) => docsIsTextBlock(x.type));
    const i = order.findIndex((x) => x.id === b.id);
    if (b.type !== "paragraph") {
      apply([{ op: "update", id: b.id, type: "paragraph", props: {} }]);
      return;
    }
    if (i <= 0) return;
    apply([{ op: "delete", id: b.id }]);
    setEditingId(order[i - 1]!.id);
  };

  const addBlock = () => {
    if (!session) return;
    const snap = session.getSnapshot();
    const last = siblings(snap, null).at(-1);
    if (last && docsIsTextBlock(last.type) && docsPlain(last.props.text) === "") {
      setEditingId(last.id);
      return;
    }
    const nid = generateId();
    apply([{ op: "create", id: nid, type: "paragraph", parent_id: null, position: docsKeyBetween(last?.position ?? null, null), props: { text: [] } }]);
    setEditingId(nid);
  };

  const shift = (b: DocsBlock, dir: -1 | 1) => {
    if (!session) return;
    const sibs = siblings(session.getSnapshot(), b.parent_id);
    const i = sibs.findIndex((x) => x.id === b.id);
    const j = i + dir;
    if (j < 0 || j >= sibs.length) return;
    const pos = dir < 0 ? docsKeyBetween(sibs[j - 1]?.position ?? null, sibs[j]!.position) : docsKeyBetween(sibs[j]!.position, sibs[j + 1]?.position ?? null);
    apply([{ op: "move", id: b.id, parent_id: b.parent_id, position: pos }]);
  };

  const saveTitle = () => {
    if (title !== page.title) update.mutate({ title }, { onError: (e) => toast.error(errorMessage(e, "Couldn't rename the page.")) });
  };
  const name = docsTitle(page.title);
  const commentCount = Object.values(detail.comment_counts).reduce((a, b) => a + b, 0);
  const linked = new Set(snapshot.blocks.map((b) => b.props.page_id ?? b.props.database_id).filter(Boolean));
  const loose = (kids.data ?? []).filter((k) => !linked.has(k.id));

  return (
    <Screen edges={["bottom"]} scroll keyboard>
      <Stack.Screen
        options={{
          title: name,
          headerRight: () => (
            <View style={{ flexDirection: "row", alignItems: "center" }}>
              <IconButton label={commentCount ? `Comments, ${commentCount} open` : "Comments"} icon={MessageSquare} onPress={() => router.push(`/docs/${page.id}/comments` as never)} />
              <ResourceMenu
                feature="docs"
                kind="docs.page"
                id={page.id}
                title={name}
                orgId={page.org_id}
                actions={[
                  { id: "fav", label: detail.favorite ? "Remove from favorites" : "Add to favorites", icon: Star, onSelect: () => favorite.mutate(!detail.favorite) },
                  { id: "link", label: "Share link", icon: FileText, onSelect: () => void Share.share({ message: `/docs/${page.id}` }) },
                  ...(canEdit && !trashed ? [{ id: "lock", label: page.locked ? "Allow editing" : "Lock page", icon: page.locked ? LockOpen : Lock, onSelect: () => update.mutate({ locked: !page.locked }) }] : []),
                  ...(canEdit && !trashed
                    ? [
                        {
                          id: "trash",
                          label: "Move to trash",
                          icon: Trash2,
                          destructive: true,
                          onSelect: () =>
                            trash.mutate(
                              { pageId: page.id, databaseId: page.is_row ? page.parent_id : null },
                              {
                                onSuccess: () => {
                                  toast.success("Moved to the trash.");
                                  router.back();
                                },
                                onError: (e) => toast.error(errorMessage(e, "Couldn't move it to the trash.")),
                              },
                            ),
                        },
                      ]
                    : []),
                ]}
              />
            </View>
          ),
        }}
      />
      {trashed ? (
        <Banner tone="warning" title="This page is in the trash." {...(canEdit ? { action: { label: "Restore", onPress: () => restore.mutate(page.id) } } : {})} />
      ) : null}
      {snapshot.saveState === "offline" ? <Banner tone="warning" icon={CloudOff} title="Offline. Your changes save when you're back online." /> : null}
      <View style={{ gap: s[2] }}>
        {page.icon ? <Text role="hero">{page.icon}</Text> : null}
        {editable ? (
          <TextInput
            value={title}
            onChangeText={(t) => setTitle(t.replace(/\n/g, " "))}
            onBlur={saveTitle}
            onSubmitEditing={saveTitle}
            placeholder="Untitled"
            placeholderTextColor={c.textSubtle}
            multiline
            blurOnSubmit
            maxLength={300}
            accessibilityLabel="Page title"
            style={[theme.type.display as object, { color: c.text }]}
          />
        ) : (
          <Text role="display">{name}</Text>
        )}
        {viewers.length ? <AvatarGroup people={Array.from(new Map(viewers.map((v) => [v.user.id, { name: v.user.name, src: v.user.avatar_url }])).values())} size="xs" /> : null}
        {page.locked ? (
          <Text role="caption" tone="muted">
            Locked
          </Text>
        ) : null}
      </View>
      {page.is_row && detail.database ? <RowProperties detail={detail} editable={canEdit && !trashed} /> : null}
      {page.kind === "database" ? (
        <DatabaseView db={page} views={detail.views} editable={editable} />
      ) : (
        <>
          <BlockList
            children={snapshot.children}
            refs={detail.refs}
            editable={editable}
            editingId={editingId}
            viewersByBlock={viewersByBlock}
            onEdit={setEditingId}
            onChangeText={onChangeText}
            onToggleTodo={(b) => apply([{ op: "update", id: b.id, props: { checked: !b.props.checked } }])}
            onBlockMenu={(b) => editable && setMenuBlock(b)}
            onBackspaceEmpty={onBackspaceEmpty}
            renderDatabase={(id) => (
              <ListRow title={docsTitle(detail.refs.pages[id]?.title)} leading={<FileText size={18} color={c.textMuted} />} onPress={() => router.push(`/docs/${id}` as never)} />
            )}
          />
          {editable ? (
            <Button variant="ghost" size="md" leftIcon={Plus} onPress={addBlock}>
              Add a block
            </Button>
          ) : null}
        </>
      )}
      {loose.length ? (
        <View style={{ gap: s[1] }}>
          <Text role="label" tone="muted" weight={600}>
            Pages inside
          </Text>
          {loose.map((k) => (
            <ListRow key={k.id} title={`${k.icon ? `${k.icon} ` : ""}${docsTitle(k.title)}`} onPress={() => router.push(`/docs/${k.id}` as never)} />
          ))}
        </View>
      ) : null}
      <Sheet open={!!menuBlock} onClose={() => setMenuBlock(null)} title={menuBlock ? DOCS_BLOCK_LABEL[menuBlock.type] : ""} snapPoints={["60%"]}>
        {menuBlock ? (
          <View>
            <ListRow
              title="Add a block below"
              leading={<Plus size={18} color={c.text} />}
              chevron={false}
              onPress={() => {
                const b = menuBlock;
                setMenuBlock(null);
                const nid = generateId();
                apply([{ op: "create", id: nid, type: "paragraph", parent_id: b.parent_id, position: positionAfter(session!.getSnapshot(), b), props: { text: [] } }]);
                setEditingId(nid);
              }}
            />
            <ListRow title="Move up" leading={<ArrowUp size={18} color={c.text} />} chevron={false} onPress={() => (shift(menuBlock, -1), setMenuBlock(null))} />
            <ListRow title="Move down" leading={<ArrowDown size={18} color={c.text} />} chevron={false} onPress={() => (shift(menuBlock, 1), setMenuBlock(null))} />
            {docsIsTextBlock(menuBlock.type)
              ? TURN_INTO.filter((t) => t !== menuBlock.type).map((t) => (
                  <ListRow key={t} title={`Turn into ${DOCS_BLOCK_LABEL[t].toLowerCase()}`} chevron={false} onPress={() => (apply([{ op: "update", id: menuBlock.id, type: t, props: t === "to_do" ? { checked: false } : {} }]), setMenuBlock(null))} />
                ))
              : null}
            <ListRow title="Delete" destructive leading={<Trash2 size={18} color={c.dangerStrong} />} chevron={false} onPress={() => (apply([{ op: "delete", id: menuBlock.id }]), setMenuBlock(null))} />
          </View>
        ) : null}
      </Sheet>
    </Screen>
  );
}
