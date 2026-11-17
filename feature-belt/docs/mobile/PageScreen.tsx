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
  useDocsExport,
  useDocsFollow,
  useDocsPage,
  useDocsPageSession,
  useDocsPresence,
  useDocsRestorePage,
  useDocsSaveAsTemplate,
  useDocsSetFavorite,
  useDocsTrashPage,
  useDocsUpdatePage,
  useMe,
  DOCS_COVER_GALLERY_NAMES,
  DOCS_ICON_COLORS,
  DOCS_ICON_SET,
  type DocsBlock,
  type DocsBlockType,
  type DocsCoverColor,
  type DocsExportFormat,
  type DocsOp,
  type DocsPageDetail,
  type DocsSessionSnapshot,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { AvatarGroup, Banner, Button, EmptyState, ErrorState, IconButton, ListRow, Screen, Sheet, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { ArrowDown, ArrowUp, BadgeCheck, Bell, BellOff, CloudOff, Download, FileText, History, ImagePlus, LayoutTemplate, Lock, MessageSquare, Plus, Smile, Star, Trash2, LockOpen, Type } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { AppState, Platform, Pressable, ScrollView, Share, TextInput, View } from "react-native";
import { BlockInsertSheet, type InsertPlace } from "./BlockInsertSheet";
import { CoverArt, DOCS_LUCIDE_NATIVE, PageIcon, paletteColor } from "./PageIcon";
import { docsShareExport } from "./pageActions";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { BlockList } from "./Blocks";
import { DatabaseView } from "./DatabaseView";
import { InlineDatabase } from "./database/InlineDatabase";
import { RowProperties } from "./database/RowProperties";

const LISTY = new Set<DocsBlockType>(["bulleted_list", "numbered_list", "to_do", "toggle"]);
const TURN_INTO: DocsBlockType[] = ["paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"];

/** Emoji offered for page icons on mobile (the web has the full searchable set). */
const ICON_EMOJI = ["📄", "📝", "📘", "📚", "🗂️", "📌", "✅", "🎯", "🚀", "💡", "⚠️", "📅", "🗓️", "📈", "📊", "💰", "📦", "🛠️", "🧪", "🐛", "💻", "📱", "🌐", "💬", "📣", "👋", "🎉", "❤️", "⭐", "🔥", "✨", "🌱", "🏠", "🏢", "🧭", "🗺️", "🧩", "🎨", "✏️", "🎓", "🧠", "🤝", "👥", "🏆", "🔍", "⚖️"];

const EXPORTS: { id: DocsExportFormat; label: string }[] = [
  { id: "markdown", label: "Markdown" },
  { id: "docx", label: "Word (.docx)" },
  { id: "html", label: "HTML (print to PDF from the share sheet)" },
  { id: "zip", label: "Markdown & CSV with subpages (.zip)" },
];

const FONT_FAMILY = { default: undefined, serif: Platform.select({ ios: "Georgia", default: "serif" }), mono: Platform.select({ ios: "Menlo", default: "monospace" }) } as const;

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
  const follow = useDocsFollow(page.id);
  const exporter = useDocsExport();
  const saveTemplate = useDocsSaveAsTemplate(page.org_id);
  const [insertAt, setInsertAt] = useState<InsertPlace | null>(null);
  const [sheet, setSheet] = useState<null | "icon" | "cover" | "export">(null);
  const [iconColor, setIconColor] = useState<DocsCoverColor>("neutral");
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
    setInsertAt({ parentId: null, after: last?.position ?? null, before: null });
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
                  {
                    id: "follow",
                    label: detail.following ? "Stop following" : "Follow",
                    icon: detail.following ? BellOff : Bell,
                    onSelect: () => follow.mutate(!detail.following, { onSuccess: (r) => toast.success(r.following ? "You'll hear about edits and comments." : "You won't hear about edits any more."), onError: (e) => toast.error(errorMessage(e, "Couldn't change following.")) }),
                  },
                  { id: "link", label: "Share link", icon: FileText, onSelect: () => void Share.share({ message: `/docs/${page.id}` }) },
                  ...(page.kind === "page" ? [{ id: "history", label: "Page history", icon: History, onSelect: () => router.push(`/docs/${page.id}/history` as never) }] : []),
                  { id: "export", label: "Export…", icon: Download, onSelect: () => setSheet("export") },
                  ...(!page.is_row && !page.is_template
                    ? [{ id: "template", label: "Save as template", icon: LayoutTemplate, onSelect: () => saveTemplate.mutate({ pageId: page.id }, { onSuccess: () => toast.success("Saved as a template."), onError: (e) => toast.error(errorMessage(e, "Couldn't save the template.")) }) }]
                    : []),
                  ...(editable && page.kind === "page"
                    ? [
                        {
                          id: "font",
                          label: "Text style",
                          icon: Type,
                          choices: (["default", "serif", "mono"] as const).map((f) => ({ id: f, label: f === "default" ? "Default" : f === "serif" ? "Serif" : "Mono", checked: (page.font ?? "default") === f, onSelect: () => update.mutate({ font: f }) })),
                        },
                      ]
                    : []),
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
      {page.cover ? (
        <Pressable accessibilityRole="button" accessibilityLabel="Change cover" disabled={!editable} onPress={() => setSheet("cover")} style={{ marginHorizontal: -s[4], marginTop: -s[4] }}>
          <CoverArt cover={page.cover} height={120} />
        </Pressable>
      ) : null}
      <View style={{ gap: s[2] }}>
        {page.icon || page.custom_icon ? (
          <Pressable accessibilityRole="button" accessibilityLabel="Change icon" disabled={!editable} onPress={() => setSheet("icon")} style={{ alignSelf: "flex-start" }}>
            <PageIcon icon={page.icon} custom={page.custom_icon} kind={page.kind} size="lg" />
          </Pressable>
        ) : null}
        {editable && (!(page.icon || page.custom_icon) || !page.cover) ? (
          <View style={{ flexDirection: "row", gap: s[1] }}>
            {!(page.icon || page.custom_icon) ? (
              <Button variant="ghost" size="md" leftIcon={Smile} onPress={() => setSheet("icon")}>
                Add icon
              </Button>
            ) : null}
            {!page.cover ? (
              <Button variant="ghost" size="md" leftIcon={ImagePlus} onPress={() => setSheet("cover")}>
                Add cover
              </Button>
            ) : null}
          </View>
        ) : null}
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
            style={[theme.type.display as object, { color: c.text }, FONT_FAMILY[page.font ?? "default"] ? { fontFamily: FONT_FAMILY[page.font ?? "default"] } : null]}
          />
        ) : (
          <Text role="display" style={FONT_FAMILY[page.font ?? "default"] ? { fontFamily: FONT_FAMILY[page.font ?? "default"] } : undefined}>
            {name}
          </Text>
        )}
        {page.verified ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[1], alignSelf: "flex-start", backgroundColor: c.successSoft, borderRadius: theme.radius.pill, paddingHorizontal: s[2], paddingVertical: s[0.5] }}>
            <BadgeCheck size={14} color={c.successStrong} />
            <Text role="caption" style={{ color: c.successStrong }}>
              Verified{page.verified_by && detail.refs.users[page.verified_by] ? ` by ${detail.refs.users[page.verified_by]!.name}` : ""}
            </Text>
          </View>
        ) : null}
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
            renderDatabase={(id, blockId) => <InlineDatabase databaseId={id} blockId={blockId} pageId={page.id} />}
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
                const sibs = siblings(session!.getSnapshot(), b.parent_id);
                const i = sibs.findIndex((x) => x.id === b.id);
                setInsertAt({ parentId: b.parent_id, after: b.position, before: sibs[i + 1]?.position ?? null });
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
      <BlockInsertSheet
        open={!!insertAt}
        onClose={() => setInsertAt(null)}
        pageId={page.id}
        place={insertAt ?? { parentId: null, after: null, before: null }}
        onInsert={(ops, focusId) => {
          setInsertAt(null);
          apply(ops);
          if (focusId) setEditingId(focusId);
        }}
      />
      <Sheet open={sheet === "icon"} onClose={() => setSheet(null)} title="Page icon" snapPoints={["70%"]}>
        <ScrollView contentContainerStyle={{ gap: s[3] }}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1] }}>
            {ICON_EMOJI.map((e) => (
              <Pressable key={e} accessibilityRole="button" accessibilityLabel={`Icon ${e}`} onPress={() => (setSheet(null), update.mutate({ icon: e }))} style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center" }}>
                <Text style={{ fontSize: 24 }}>{e}</Text>
              </Pressable>
            ))}
          </View>
          <View style={{ flexDirection: "row", gap: s[2] }}>
            {DOCS_ICON_COLORS.map((col) => (
              <Pressable
                key={col}
                accessibilityRole="radio"
                accessibilityState={{ checked: iconColor === col }}
                accessibilityLabel={`${col} colour`}
                onPress={() => setIconColor(col)}
                style={{ width: 28, height: 28, borderRadius: theme.radius.pill, backgroundColor: paletteColor(theme, col === "neutral" ? "ink-400" : col), borderWidth: 2, borderColor: iconColor === col ? c.text : "transparent" }}
              />
            ))}
          </View>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1] }}>
            {DOCS_ICON_SET.map((n) => {
              const Icon = DOCS_LUCIDE_NATIVE[n];
              return (
                <Pressable key={n} accessibilityRole="button" accessibilityLabel={n.replace(/-/g, " ")} onPress={() => (setSheet(null), update.mutate({ custom_icon: { type: "lucide", name: n, color: iconColor } }))} style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center" }}>
                  <Icon size={22} color={paletteColor(theme, iconColor)} />
                </Pressable>
              );
            })}
          </View>
          {page.icon || page.custom_icon ? (
            <Button variant="ghost" size="md" onPress={() => (setSheet(null), update.mutate({ icon: null, custom_icon: null }))}>
              Remove icon
            </Button>
          ) : null}
        </ScrollView>
      </Sheet>
      <Sheet open={sheet === "cover"} onClose={() => setSheet(null)} title="Cover" snapPoints={["55%"]}>
        <View style={{ gap: s[3] }}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            {DOCS_COVER_GALLERY_NAMES.map((g) => (
              <Pressable key={g} accessibilityRole="button" accessibilityLabel={`${g} cover`} onPress={() => (setSheet(null), update.mutate({ cover: { gallery: g, position: 50 } }))} style={{ width: 72, borderRadius: theme.radius.sm, overflow: "hidden" }}>
                <CoverArt cover={{ gallery: g, position: 50 }} height={40} />
              </Pressable>
            ))}
          </View>
          {page.cover ? (
            <Button variant="ghost" size="md" onPress={() => (setSheet(null), update.mutate({ cover: null }))}>
              Remove cover
            </Button>
          ) : null}
        </View>
      </Sheet>
      <Sheet open={sheet === "export"} onClose={() => setSheet(null)} title="Export" snapPoints={["45%"]}>
        <View>
          {EXPORTS.map((x) => (
            <ListRow
              key={x.id}
              title={x.label}
              leading={<Download size={18} color={c.text} />}
              chevron={false}
              disabled={exporter.isPending}
              onPress={() =>
                exporter.mutate(
                  { pageId: page.id, format: x.id, subpages: x.id === "zip" },
                  { onSuccess: (r) => (setSheet(null), void docsShareExport(r)), onError: (e) => toast.error(errorMessage(e, "Couldn't export the page.")) },
                )
              }
            />
          ))}
        </View>
      </Sheet>
    </Screen>
  );
}
