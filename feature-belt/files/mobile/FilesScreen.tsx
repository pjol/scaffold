import {
  errorMessage,
  FILES_SECTIONS,
  filesCanEdit,
  filesCanManage,
  filesFormatSize,
  filesIsTextPreview,
  filesKindLabel,
  filesParseCsv,
  hasErrorCode,
  orgsNounA,
  orgsNounTitle,
  topic,
  useActiveOrg,
  useApi,
  useDebouncedValue,
  useFilesBatch,
  useFilesChildren,
  useFilesDrive,
  useFilesItem,
  useFilesPreviewUrl,
  useFilesRestoreVersion,
  useFilesSearch,
  useFilesSection,
  useFilesStar,
  useFilesText,
  useFilesVersions,
  useTopic,
  type FilesItem,
  type FilesSection,
  type PagedQueryResult,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Input, ListGroup, ListRow, Screen, Sheet, Skeleton, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router, Stack, useLocalSearchParams } from "expo-router";
import {
  Building2,
  ChevronDown,
  Clock,
  Copy,
  Download,
  ExternalLink,
  FileUp,
  FolderInput,
  FolderOpen,
  FolderPlus,
  HardDrive,
  ImageUp,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  Star,
  StarOff,
  Trash,
  UserPlus,
  Users,
  X,
  type LucideIcon,
} from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { filesOpenInBrowser, filesPickDocuments, filesPickPhotos, filesSaveOrShare, filesUploadLocal, type FilesLocalFile } from "./actions";
import { filesIconTone, filesItemIcon } from "./icons";
import { FILES_MONO, FilesMarkdown } from "./Markdown";
import { MoveSheet, NewFolderSheet, RenameSheet } from "./sheets";

const SECTION_ICON: Record<FilesSection, LucideIcon> = { my: HardDrive, shared: Users, shared_with_me: UserPlus, recent: Clock, starred: Star, trash: Trash };

const EMPTY: Record<FilesSection, string> = {
  my: "No files yet",
  shared: "No shared folders yet",
  shared_with_me: "Nothing shared with you yet",
  recent: "Nothing opened yet",
  starred: "No starred items",
  trash: "The trash is empty",
};

function relative(iso: string): string {
  const d = new Date(iso);
  const days = Math.round((Date.now() - d.getTime()) / 86_400_000);
  if (days < 1) return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  if (days < 7) return d.toLocaleDateString(undefined, { weekday: "short" });
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// ---- shared pieces -------------------------------------------------------------------------

/** Dialogs and pickers the screens share. */
function useItemSheets(orgId: string | null) {
  const [renaming, setRenaming] = useState<FilesItem | null>(null);
  const [moving, setMoving] = useState<{ item: FilesItem; mode: "move" | "copy" } | null>(null);
  const [deleting, setDeleting] = useState<FilesItem | null>(null);
  const batch = useFilesBatch();
  const element = (
    <>
      <RenameSheet item={renaming} onClose={() => setRenaming(null)} />
      {orgId ? <MoveSheet orgId={orgId} item={moving?.item ?? null} mode={moving?.mode ?? "move"} onClose={() => setMoving(null)} /> : null}
      <ConfirmDialog
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}” forever?`}
        tone="danger"
        confirmLabel="Delete forever"
        onConfirm={async () => {
          if (!deleting) return;
          const { results } = await batch.mutateAsync({ action: "delete", ids: [deleting.id] });
          if (results[0]?.ok) toast.success("Deleted");
          else toast.error(results[0]?.error?.message ?? "Couldn't delete it.");
        }}
      >
        <Text tone="muted">Everything inside goes too, with every version. This can't be undone.</Text>
      </ConfirmDialog>
    </>
  );
  return { element, rename: setRenaming, move: (item: FilesItem) => setMoving({ item, mode: "move" }), copy: (item: FilesItem) => setMoving({ item, mode: "copy" }), remove: setDeleting };
}

/** The screen's own ⋮ rows for an item (Share… and AI agent access come from other features). */
function useItemMenu(item: FilesItem | null | undefined, sheets: ReturnType<typeof useItemSheets>): ResourceMenuAction[] {
  const api = useApi();
  const star = useFilesStar();
  const batch = useFilesBatch();
  if (!item) return [];
  const edit = filesCanEdit(item);
  const rows: ResourceMenuAction[] = [];
  if (item.trashed_at) {
    if (edit)
      rows.push({
        id: "restore",
        label: "Restore",
        icon: RotateCcw,
        onSelect: () => batch.mutate({ action: "restore", ids: [item.id] }, { onSuccess: ({ results }) => (results[0]?.ok ? toast.success("Restored") : toast.error(results[0]?.error?.message ?? "Couldn't restore.")) }),
      });
    if (filesCanManage(item)) rows.push({ id: "delete", label: "Delete forever", icon: X, destructive: true, onSelect: () => sheets.remove(item) });
    return rows;
  }
  if (item.type === "file") rows.push({ id: "share-file", label: "Save or send a copy", icon: Download, onSelect: () => void filesSaveOrShare(api.http, item) });
  if (edit) rows.push({ id: "rename", label: "Rename", icon: Pencil, onSelect: () => sheets.rename(item) });
  if (edit) rows.push({ id: "move", label: "Move", icon: FolderInput, onSelect: () => sheets.move(item) });
  if (item.type === "file" || item.type === "folder") rows.push({ id: "copy", label: "Make a copy", icon: Copy, onSelect: () => sheets.copy(item) });
  rows.push({ id: "star", label: item.starred ? "Remove star" : "Star", icon: item.starred ? StarOff : Star, onSelect: () => star.mutate({ id: item.id, on: !item.starred }) });
  if (edit)
    rows.push({
      id: "trash",
      label: "Move to trash",
      icon: Trash,
      destructive: true,
      onSelect: () =>
        batch.mutate(
          { action: "trash", ids: [item.id] },
          {
            onSuccess: ({ results }) =>
              results[0]?.ok
                ? toast.success(`“${item.name}” moved to the trash`, { action: { label: "Undo", onPress: () => batch.mutate({ action: "restore", ids: [item.id] }) } })
                : toast.error(results[0]?.error?.message ?? "Couldn't move it to the trash."),
          },
        ),
    });
  return rows;
}

function ItemIcon({ item }: { item: FilesItem }) {
  const theme = useTheme();
  if (item.thumb_url) return <Image source={{ uri: item.thumb_url }} style={{ width: 36, height: 36, borderRadius: theme.radius.sm }} contentFit="cover" accessibilityIgnoresInvertColors />;
  const Icon = filesItemIcon(item);
  const tone = filesIconTone(item);
  const color = { primary: theme.colors.primaryStrong, accent: theme.colors.accentStrong, danger: theme.colors.dangerStrong, success: theme.colors.successStrong, muted: theme.colors.textMuted }[tone];
  return <Icon size={26} color={color} />;
}

/** One item: a pressable name (opens it) beside its ⋮ menu (never nested). */
function ItemRow({ item, sheets }: { item: FilesItem; sheets: ReturnType<typeof useItemSheets> }) {
  const theme = useTheme();
  const actions = useItemMenu(item, sheets);
  return (
    <View style={{ flexDirection: "row", alignItems: "center", paddingRight: theme.space[1] }}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${item.name}, ${filesKindLabel(item)}`}
        onPress={() => router.push(item.link as never)}
        style={({ pressed }) => ({ flex: 1, flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.lg, paddingVertical: theme.space[2], paddingLeft: theme.space[4], backgroundColor: pressed ? theme.colors.sunken : undefined })}
      >
        <ItemIcon item={item} />
        <Text weight={500} numberOfLines={1} style={{ flex: 1 }}>
          {item.name}
        </Text>
        {item.starred ? <Star size={14} color={theme.colors.warningStrong} fill={theme.colors.warningStrong} accessibilityLabel="Starred" /> : null}
        <Text role="caption" tone="muted">
          {relative(item.trashed_at ?? item.updated_at)}
        </Text>
      </Pressable>
      <ResourceMenu feature="files" kind="files.item" id={item.id} title={item.name} orgId={item.org_id} actions={actions} label={`More actions for ${item.name}`} />
    </View>
  );
}

function ItemList({ query, empty, sheets, search }: { query: PagedQueryResult<FilesItem>; empty: string; sheets: ReturnType<typeof useItemSheets>; search?: boolean }) {
  const rows = query.data?.items ?? [];
  return (
    <View style={{ gap: 12 }}>
      {query.isError && rows.length > 0 ? <Banner tone="warning" title="Couldn't refresh. Showing what was loaded." action={{ label: "Retry", onPress: () => void query.refetch() }} /> : null}
      <ListGroup>
        {query.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : query.isError && rows.length === 0 ? (
          <ErrorState compact title="Couldn't load these files" info={errorMessage(query.error)} onRetry={() => void query.refetch()} retrying={query.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={search ? Search : FolderOpen} title={empty} />
        ) : (
          rows.map((it) => <ItemRow key={it.id} item={it} sheets={sheets} />)
        )}
        {query.hasNextPage ? (
          <Button variant="ghost" loading={query.isFetchingNextPage} onPress={() => void query.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </ListGroup>
    </View>
  );
}

/** "Add": photos, files, a new folder. Pickers start after the sheet closes. */
function AddSheet({ orgId, parentId, open, onClose }: { orgId: string; parentId: string | null; open: boolean; onClose: () => void }) {
  const api = useApi();
  const theme = useTheme();
  const [next, setNext] = useState<"photos" | "files" | "folder" | null>(null);
  const [folder, setFolder] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const upload = async (files: FilesLocalFile[]) => {
    if (files.length === 0) return;
    setBusy(files.length === 1 ? `Uploading “${files[0]?.name}”` : `Uploading ${files.length} files`);
    const { ok, error } = await filesUploadLocal(api.http, orgId, parentId, files, (done) => setBusy(`Uploaded ${done} of ${files.length}`));
    setBusy(null);
    if (error && ok === 0) toast.error(error);
    else if (error) toast.error(`${ok} uploaded. ${error}`);
    else toast.success(ok === 1 ? "Uploaded" : `${ok} files uploaded`);
  };
  return (
    <>
      <Sheet
        open={open}
        onClose={onClose}
        title="Add"
        onDismissed={() => {
          const n = next;
          setNext(null);
          if (n === "photos") void filesPickPhotos().then(upload);
          if (n === "files") void filesPickDocuments().then(upload);
          if (n === "folder") setFolder(true);
        }}
      >
        <ListGroup>
          <ListRow title="Upload photos" leading={<ImageUp size={22} color={theme.colors.textMuted} />} onPress={() => (setNext("photos"), onClose())} />
          <ListRow title="Upload files" leading={<FileUp size={22} color={theme.colors.textMuted} />} onPress={() => (setNext("files"), onClose())} />
          <ListRow title="New folder" leading={<FolderPlus size={22} color={theme.colors.textMuted} />} onPress={() => (setNext("folder"), onClose())} />
        </ListGroup>
      </Sheet>
      <NewFolderSheet orgId={orgId} parentId={parentId} open={folder} onClose={() => setFolder(false)} />
      {busy ? (
        <View style={{ position: "absolute", left: theme.space[4], right: theme.space[4], bottom: theme.space[4] }}>
          <Banner tone="info" title={busy} />
        </View>
      ) : null}
    </>
  );
}

// ---- Home → Files ------------------------------------------------------------------------------

/** files/index: a section of the active org's drive, with search and Add. */
export function FilesHomeScreen() {
  const theme = useTheme();
  const { org, isPending } = useActiveOrg();
  const params = useLocalSearchParams<{ section?: string }>();
  const section = (FILES_SECTIONS.find((s) => s.id === params.section)?.id ?? "my") as FilesSection;
  const [picking, setPicking] = useState(false);
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState("");
  const debounced = useDebouncedValue(q, 300);
  const query = useFilesSection(org?.id, section, { thumbs: true });
  const search = useFilesSearch(org?.id, debounced);
  const sheets = useItemSheets(org?.id ?? null);
  const drive = useFilesDrive(org?.id);
  const searching = debounced.trim().length > 0;
  const searchQuery = useMemo(
    () => ({ ...search, data: search.data ? { ...search.data, items: search.data.items.map((h) => h.item) } : undefined }) as unknown as PagedQueryResult<FilesItem>,
    [search],
  );
  useTopic(org && section === "shared" ? topic("files", org.id) : null);
  const label = FILES_SECTIONS.find((s) => s.id === section)?.label ?? "Files";

  if (!isPending && !org) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Files" }} />
        <EmptyState icon={Building2} title={`Join or create ${orgsNounA()} first`} action={<Button onPress={() => router.push("/settings/orgs" as never)}>{orgsNounTitle("many")}</Button>} />
      </Screen>
    );
  }
  return (
    <Screen
      edges={["bottom"]}
      refreshControl={<RefreshControl refreshing={query.isRefetching} onRefresh={() => void query.refetch()} />}
      footer={
        section === "my" && org ? (
          <Button fullWidth leftIcon={Plus} onPress={() => setAdding(true)}>
            Add
          </Button>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: "Files" }} />
      <View style={{ gap: theme.space[3] }}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`${label}. Change section`}
          onPress={() => setPicking(true)}
          hitSlop={8}
          style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: theme.tap.base, alignSelf: "flex-start" }}
        >
          <Text role="title">{label}</Text>
          <ChevronDown size={20} color={theme.colors.textMuted} />
        </Pressable>
        <Input value={q} onChangeText={setQ} placeholder="Search files" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} accessibilityLabel="Search files" returnKeyType="search" />
        {drive.data && drive.data.quota_bytes > 0 && section === "my" && !searching ? (
          <Text role="caption" tone="muted">
            {filesFormatSize(drive.data.used_bytes)} of {filesFormatSize(drive.data.quota_bytes)} used
          </Text>
        ) : null}
      </View>
      {searching ? <ItemList query={searchQuery} empty={`Nothing matches “${debounced.trim()}”`} sheets={sheets} search /> : <ItemList query={query} empty={EMPTY[section]} sheets={sheets} />}
      <Sheet open={picking} onClose={() => setPicking(false)} title="Drive">
        <ListGroup>
          {FILES_SECTIONS.map((s) => {
            const Icon = SECTION_ICON[s.id];
            return (
              <ListRow
                key={s.id}
                title={s.label}
                leading={<Icon size={22} color={s.id === section ? theme.colors.primaryStrong : theme.colors.textMuted} />}
                trailing={s.id === section ? <Badge tone="primary">Open</Badge> : undefined}
                onPress={() => {
                  setPicking(false);
                  router.setParams({ section: s.id === "my" ? undefined : s.id });
                }}
              />
            );
          })}
        </ListGroup>
      </Sheet>
      {org ? <AddSheet orgId={org.id} parentId={null} open={adding} onClose={() => setAdding(false)} /> : null}
      {sheets.element}
    </Screen>
  );
}

// ---- one item -----------------------------------------------------------------------------------

/** files/[itemID]: a folder's contents or a file's preview, with the ⋮ menu in the header. */
export function FilesItemScreen() {
  const { itemID } = useLocalSearchParams<{ itemID: string }>();
  const detail = useFilesItem(itemID);
  const item = detail.data?.item;
  const sheets = useItemSheets(item?.org_id ?? null);
  const actions = useItemMenu(item, sheets);
  const menu = item ? () => <ResourceMenu feature="files" kind="files.item" id={item.id} title={item.name} orgId={item.org_id} actions={actions} /> : undefined;
  // Registered item types (a repository) open their own screen.
  useEffect(() => {
    if (item && item.type !== "folder" && item.type !== "file" && item.link !== `/files/${item.id}`) router.replace(item.link as never);
  }, [item]);

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
        {hasErrorCode(detail.error, "not_found") ? (
          <EmptyState icon={FolderOpen} title="This item isn't available" action={<Button variant="secondary" onPress={() => router.replace("/files" as never)}>Go to My drive</Button>} />
        ) : (
          <ErrorState title="Couldn't load this item" info={errorMessage(detail.error)} onRetry={() => void detail.refetch()} retrying={detail.isRefetching} />
        )}
      </Screen>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: item.name, ...(menu ? { headerRight: menu } : {}) }} />
      {item.type === "folder" ? <FolderBody item={item} sheets={sheets} /> : <FileBody item={item} />}
      {sheets.element}
    </>
  );
}

function FolderBody({ item, sheets }: { item: FilesItem; sheets: ReturnType<typeof useItemSheets> }) {
  const [adding, setAdding] = useState(false);
  const query = useFilesChildren(item.trashed_at ? null : item.id, { thumbs: true });
  useTopic(topic("files", item.id));
  const edit = filesCanEdit(item) && !item.trashed_at;
  return (
    <Screen
      edges={["bottom"]}
      refreshControl={<RefreshControl refreshing={query.isRefetching} onRefresh={() => void query.refetch()} />}
      footer={
        edit ? (
          <Button fullWidth leftIcon={Plus} onPress={() => setAdding(true)}>
            Add
          </Button>
        ) : undefined
      }
    >
      {item.trashed_at ? <Banner tone="warning" title="This folder is in the trash." /> : <ItemList query={query} empty="This folder is empty" sheets={sheets} />}
      <AddSheet orgId={item.org_id} parentId={item.id} open={adding} onClose={() => setAdding(false)} />
    </Screen>
  );
}

function FileBody({ item }: { item: FilesItem }) {
  const theme = useTheme();
  const api = useApi();
  const media = item.preview === "image";
  const text = filesIsTextPreview(item.preview) && item.preview !== "svg";
  const url = useFilesPreviewUrl(item.id, null, media);
  const content = useFilesText(text ? item.id : null);
  const browser = item.preview === "pdf" || item.preview === "video" || item.preview === "audio" || item.preview === "office" || item.preview === "svg";
  return (
    <Screen
      edges={["bottom"]}
      footer={
        <View style={{ flexDirection: "row", gap: theme.space[2] }}>
          {browser ? (
            <Button style={{ flex: 1 }} leftIcon={ExternalLink} onPress={() => void filesOpenInBrowser(api.http, item)}>
              Open
            </Button>
          ) : null}
          <Button style={{ flex: 1 }} variant={browser ? "secondary" : "primary"} leftIcon={Download} onPress={() => void filesSaveOrShare(api.http, item)}>
            Save or send
          </Button>
        </View>
      }
    >
      {item.trashed_at ? <Banner tone="warning" title="This file is in the trash." /> : null}
      {media ? (
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
          <ErrorState compact title="Couldn't load the file" info={errorMessage(content.error)} onRetry={() => void content.refetch()} />
        ) : item.preview === "markdown" ? (
          <View style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border, padding: theme.space[4] }}>
            <FilesMarkdown text={content.data.text} />
          </View>
        ) : item.preview === "csv" ? (
          <CsvView text={content.data.text} />
        ) : (
          <ScrollView horizontal style={{ backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border }} contentContainerStyle={{ padding: theme.space[4] }}>
            <Text role="label" selectable style={{ fontFamily: FILES_MONO }}>
              {content.data.text.split("\n").slice(0, 2000).join("\n")}
            </Text>
          </ScrollView>
        )
      ) : (
        <View style={{ alignItems: "center", gap: theme.space[3], paddingVertical: theme.space[10], backgroundColor: theme.colors.surface, borderRadius: theme.radius.lg, borderWidth: 1, borderColor: theme.colors.border }}>
          <ItemIcon item={item} />
          <Text role="titleSm" align="center">
            {browser ? `Open to view this ${filesKindLabel(item).toLowerCase()}` : "No preview for this file"}
          </Text>
        </View>
      )}
      {content.data?.truncated ? <Banner tone="info" title="Showing the first 1 MB." /> : null}
      <Details item={item} />
    </Screen>
  );
}

function CsvView({ text }: { text: string }) {
  const theme = useTheme();
  const { rows } = useMemo(() => filesParseCsv(text, 201), [text]);
  return (
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
  );
}

function Details({ item }: { item: FilesItem }) {
  const theme = useTheme();
  const api = useApi();
  const versions = useFilesVersions(item.type === "file" ? item.id : null);
  const restore = useFilesRestoreVersion(item.id);
  const [confirm, setConfirm] = useState<{ id: string; number: number } | null>(null);
  const edit = filesCanEdit(item) && !item.trashed_at;
  return (
    <View style={{ gap: theme.space[3] }}>
      <Text role="titleSm" accessibilityRole="header">
        Details
      </Text>
      <ListGroup>
        <ListRow title="Size" trailing={<Text tone="muted">{filesFormatSize(item.size)}</Text>} />
        <ListRow title="Kind" trailing={<Text tone="muted">{filesKindLabel(item)}</Text>} />
        <ListRow title="Modified" trailing={<Text tone="muted">{new Date(item.updated_at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}</Text>} />
        <ListRow title="By" trailing={<Text tone="muted">{item.updated_by ? item.updated_by.name || "Deleted user" : "Deleted user"}</Text>} />
      </ListGroup>
      {item.version_count > 1 ? (
        <>
          <Text role="titleSm" accessibilityRole="header">
            Versions
          </Text>
          <ListGroup>
            {(versions.data?.items ?? []).map((v) => (
              <View key={v.id} style={{ flexDirection: "row", alignItems: "center", minHeight: theme.tap.base, paddingLeft: theme.space[4], paddingRight: theme.space[1], gap: theme.space[2] }}>
                <Text weight={500} style={{ flex: 1 }}>
                  Version {v.number}
                </Text>
                {v.current ? <Badge tone="primary">Current</Badge> : <Text role="caption" tone="muted">{relative(v.created_at)}</Text>}
                <IconButton label={`Download version ${v.number}`} icon={Download} onPress={() => void filesSaveOrShare(api.http, item, v.id)} />
                {edit && !v.current ? <IconButton label={`Restore version ${v.number}`} icon={RotateCcw} onPress={() => setConfirm({ id: v.id, number: v.number })} /> : null}
              </View>
            ))}
          </ListGroup>
        </>
      ) : null}
      <ConfirmDialog
        open={confirm !== null}
        onClose={() => setConfirm(null)}
        title={`Restore version ${confirm?.number ?? ""}?`}
        confirmLabel="Restore"
        onConfirm={async () => {
          if (!confirm) return;
          await restore.mutateAsync(confirm.id);
          toast.success(`Version ${confirm.number} is current again`);
        }}
      >
        <Text tone="muted">A copy of it becomes the newest version. Every other version stays.</Text>
      </ConfirmDialog>
    </View>
  );
}
