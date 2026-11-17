import {
  docsEmbedFor,
  docsFileBlockType,
  docsKeysBetween,
  docsSafeLink,
  generateId,
  useDocsFetchLinkPreview,
  useDocsUploadFile,
  type DocsBlockProps,
  type DocsBlockType,
  type DocsOp,
} from "@scaffoldapp/shared";
import { Button, Input, ListRow, Sheet, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import * as ImagePicker from "expo-image-picker";
import {
  AppWindow,
  CheckSquare,
  ChevronRight,
  Code,
  Columns2,
  Globe,
  Heading1,
  Heading2,
  Heading3,
  Heading4,
  Image as ImageIcon,
  Lightbulb,
  List,
  ListOrdered,
  ListTree,
  Minus,
  Quote,
  Sigma,
  Table,
  Text as TextIcon,
} from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { ScrollView, View } from "react-native";

type Choice = { id: string; label: string; icon: ReactNode; type?: DocsBlockType; props?: DocsBlockProps; action?: "columns" | "bookmark" | "embed" | "media" };

/** Where new blocks go: under `parentId`, between two sibling positions. */
export interface InsertPlace {
  parentId: string | null;
  after: string | null;
  before: string | null;
}

/**
 * Every block type mobile can add, from the "+" sheet: text and headings, lists, toggles, quotes, callouts, dividers,
 * code, equations, tables, table of contents, toggle headings, two columns, bookmarks and embeds (by link) and photos
 * or videos from the library. Columns, synced blocks, buttons, databases and files from other apps are added on the web.
 */
export function BlockInsertSheet({ open, onClose, pageId, place, onInsert }: { open: boolean; onClose: () => void; pageId: string; place: InsertPlace; onInsert: (ops: DocsOp[], focusId: string | null) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const [url, setUrl] = useState<{ mode: "bookmark" | "embed"; value: string; error?: string } | null>(null);
  const preview = useDocsFetchLinkPreview();
  const upload = useDocsUploadFile(pageId);
  const icon = (n: ReactNode) => <View style={{ width: 24, alignItems: "center" }}>{n}</View>;
  const ic = { size: 18, color: c.text };
  const choices: Choice[] = [
    { id: "paragraph", label: "Text", icon: <TextIcon {...ic} />, type: "paragraph" },
    { id: "heading_1", label: "Heading 1", icon: <Heading1 {...ic} />, type: "heading_1" },
    { id: "heading_2", label: "Heading 2", icon: <Heading2 {...ic} />, type: "heading_2" },
    { id: "heading_3", label: "Heading 3", icon: <Heading3 {...ic} />, type: "heading_3" },
    { id: "heading_4", label: "Heading 4", icon: <Heading4 {...ic} />, type: "heading_4" },
    { id: "bulleted_list", label: "Bulleted list", icon: <List {...ic} />, type: "bulleted_list" },
    { id: "numbered_list", label: "Numbered list", icon: <ListOrdered {...ic} />, type: "numbered_list" },
    { id: "to_do", label: "To-do list", icon: <CheckSquare {...ic} />, type: "to_do", props: { checked: false } },
    { id: "toggle", label: "Toggle list", icon: <ChevronRight {...ic} />, type: "toggle" },
    { id: "toggle_heading_2", label: "Toggle heading", icon: <Heading2 {...ic} />, type: "heading_2", props: { toggleable: true } },
    { id: "quote", label: "Quote", icon: <Quote {...ic} />, type: "quote" },
    { id: "callout", label: "Callout", icon: <Lightbulb {...ic} />, type: "callout", props: { icon: "💡", color: "gray_background" } },
    { id: "divider", label: "Divider", icon: <Minus {...ic} />, type: "divider" },
    { id: "code", label: "Code", icon: <Code {...ic} />, type: "code", props: { code: "", language: "plain" } },
    { id: "equation", label: "Block equation", icon: <Sigma {...ic} />, type: "equation", props: { expression: "" } },
    { id: "table", label: "Table", icon: <Table {...ic} />, type: "table", props: { rows: [[[], [], []], [[], [], []]], header_row: true } },
    { id: "table_of_contents", label: "Table of contents", icon: <ListTree {...ic} />, type: "table_of_contents" },
    { id: "columns", label: "2 columns", icon: <Columns2 {...ic} />, action: "columns" },
    { id: "media", label: "Photo or video", icon: <ImageIcon {...ic} />, action: "media" },
    { id: "bookmark", label: "Web bookmark", icon: <Globe {...ic} />, action: "bookmark" },
    { id: "embed", label: "Embed", icon: <AppWindow {...ic} />, action: "embed" },
  ];

  const one = (type: DocsBlockType, props: DocsBlockProps = {}): { ops: DocsOp[]; id: string } => {
    const [pos] = docsKeysBetween(place.after, place.before, 1);
    const id = generateId();
    const base: DocsBlockProps = ["paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"].includes(type) ? { text: [], ...props } : props;
    return { ops: [{ op: "create", id, type, parent_id: place.parentId, position: pos!, props: base }], id };
  };

  const pick = async (ch: Choice) => {
    if (ch.type) {
      const { ops, id } = one(ch.type, ch.props);
      onInsert(ops, ["divider", "table", "table_of_contents"].includes(ch.type) ? null : id);
      onClose();
      return;
    }
    if (ch.action === "columns") {
      const list = one("column_list");
      const listId = list.id;
      const ops = [...list.ops];
      let focus: string | null = null;
      for (const pos of docsKeysBetween(null, null, 2)) {
        const col = generateId();
        const p = generateId();
        focus ??= p;
        ops.push({ op: "create", id: col, type: "column", parent_id: listId, position: pos, props: {} }, { op: "create", id: p, type: "paragraph", parent_id: col, position: "a0", props: { text: [] } });
      }
      onInsert(ops, focus);
      onClose();
      return;
    }
    if (ch.action === "media") {
      const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ["images", "videos"], quality: 0.9 });
      const asset = res.canceled ? null : res.assets[0];
      if (!asset) return;
      const name = asset.fileName ?? (asset.type === "video" ? "video.mp4" : "photo.jpg");
      try {
        const f = await upload.mutateAsync({ file: { uri: asset.uri, name, mimeType: asset.mimeType ?? null, size: asset.fileSize ?? null }, name, ...(asset.mimeType ? { contentType: asset.mimeType } : {}) });
        const kind = docsFileBlockType(f.content_type, f.name);
        onInsert(one(kind, { file_id: f.id, caption: [] }).ops, null);
        onClose();
      } catch (e) {
        toast.error((e as { message?: string }).message ?? "Couldn't upload that. Try again.");
      }
      return;
    }
    if (ch.action === "bookmark" || ch.action === "embed") setUrl({ mode: ch.action, value: "" });
  };

  const saveUrl = async () => {
    if (!url) return;
    const href = docsSafeLink(url.value);
    if (!href || href.startsWith("mailto:")) return setUrl({ ...url, error: "Enter a link starting with http:// or https://." });
    if (url.mode === "embed") {
      if (!docsEmbedFor(href)) return setUrl({ ...url, error: "That site can't be embedded. Add it as a bookmark." });
      onInsert(one("embed", { url: href, caption: [] }).ops, null);
    } else {
      let p: DocsBlockProps = {};
      try {
        const pr = await preview.mutateAsync(href);
        p = { title: pr.title, ...(pr.description ? { description: pr.description } : {}), ...(pr.site_name ? { site_name: pr.site_name } : {}), preview_id: pr.id };
      } catch {
        // Saved without a preview.
      }
      onInsert(one("bookmark", { url: href, caption: [], ...p }).ops, null);
    }
    setUrl(null);
    onClose();
  };

  return (
    <Sheet open={open} onClose={() => (setUrl(null), onClose())} title={url ? (url.mode === "embed" ? "Embed a link" : "Web bookmark") : "Add a block"} snapPoints={["70%"]}>
      {url ? (
        <View style={{ gap: theme.space[3], padding: theme.space[4] }}>
          <Input value={url.value} onChangeText={(v) => setUrl({ ...url, value: v, error: undefined })} placeholder="https://" autoCapitalize="none" keyboardType="url" accessibilityLabel="Link" autoFocus />
          {url.error ? (
            <Text role="caption" tone="danger">
              {url.error}
            </Text>
          ) : null}
          <Button onPress={() => void saveUrl()} loading={preview.isPending}>
            {url.mode === "embed" ? "Embed" : "Add bookmark"}
          </Button>
        </View>
      ) : (
        <ScrollView>
          {choices.map((ch) => (
            <ListRow key={ch.id} title={ch.label} leading={icon(ch.icon)} chevron={false} onPress={() => void pick(ch)} />
          ))}
        </ScrollView>
      )}
    </Sheet>
  );
}
