import {
  FILE_PREVIEW_TEXT_MAX,
  fileFormatSize,
  fileOfficeKind,
  filePreviewCsvDelimiter,
  filePreviewFormatJson,
  filePreviewIsText,
  filePreviewKind,
  filePreviewKindLabel,
  filePreviewLanguage,
  filePreviewParseCsv,
  filePreviewParseMarkdown,
  type FilePreviewKind,
  type FilePreviewMdBlock,
  type FilePreviewMdInline,
} from "@scaffoldapp/shared/preview";
import type { FilePreviewToken, FilePreviewTokenRole } from "@scaffoldapp/shared/preview/highlight";
import { Image } from "expo-image";
import * as WebBrowser from "expo-web-browser";
import {
  Download,
  ExternalLink,
  File,
  FileArchive,
  FileCode,
  FileHeadphone,
  FileImage,
  FileJson,
  FileSpreadsheet,
  FileText,
  FileType,
  FileVideoCamera,
  Presentation,
  type LucideIcon,
} from "lucide-react-native";
import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { FlatList, Linking, Modal, Platform, Pressable, ScrollView, View, type StyleProp, type ViewStyle } from "react-native";
import type { NativeTheme } from "../theme/ThemeProvider";
import { useTheme } from "../theme/ThemeProvider";
import { Button } from "./Button";
import { EmptyState, ErrorState } from "./EmptyState";
import { IconButton } from "./IconButton";
import { PhotoLightbox } from "./PhotoLightbox";
import { SegmentedControl } from "./SegmentedControl";
import { Skeleton } from "./Skeleton";
import { Text } from "./Text";
import { Banner } from "./Toast";

export { filePreviewKind, type FilePreviewKind };

const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

export interface FilePreviewProps {
  /** A link the app may fetch (presigned, public or a stream from the API). */
  url: string;
  name: string;
  /** The stored content type; missing or generic types fall back to the extension. */
  mime?: string | null;
  size?: number | null;
  /** An attachment link Download opens. */
  downloadUrl?: string | null;
  /** Download handled by the caller (share sheet, save), instead of `downloadUrl`. */
  onDownload?: () => void;
  /** Attachment size: a bounded image, or a file card that opens the full preview. */
  compact?: boolean;
  /** A small image for compact images (a server thumbnail). */
  thumbnailUrl?: string | null;
  style?: StyleProp<ViewStyle>;
}

/** The lucide icon for a preview kind. */
export function filePreviewIcon(kind: FilePreviewKind, name = "", mime?: string | null): LucideIcon {
  switch (kind) {
    case "image":
    case "svg":
      return FileImage;
    case "video":
      return FileVideoCamera;
    case "audio":
      return FileHeadphone;
    case "code":
      return FileCode;
    case "json":
      return FileJson;
    case "csv":
      return FileSpreadsheet;
    case "pdf":
    case "text":
    case "markdown":
      return FileText;
    case "archive":
      return FileArchive;
    case "font":
      return FileType;
    case "office": {
      const k = fileOfficeKind(name, mime);
      return k === "sheet" ? FileSpreadsheet : k === "slides" ? Presentation : FileText;
    }
  }
  return File;
}

/** Tint per kind (never colour alone: always with the icon and name). */
export function filePreviewToneColor(theme: NativeTheme, kind: FilePreviewKind, name = "", mime?: string | null): string {
  const c = theme.colors;
  switch (kind) {
    case "image":
    case "svg":
    case "video":
      return c.accentStrong;
    case "pdf":
      return c.dangerStrong;
    case "csv":
      return c.successStrong;
    case "audio":
    case "font":
      return c.infoStrong;
    case "code":
    case "json":
      return c.primaryStrong;
    case "office": {
      const k = fileOfficeKind(name, mime);
      return k === "sheet" ? c.successStrong : k === "slides" ? c.warningStrong : c.infoStrong;
    }
  }
  return c.textMuted;
}

export interface FileIconProps {
  name: string;
  mime?: string | null;
  kind?: FilePreviewKind;
  size?: number;
  /** Tint by kind. @default true */
  tone?: boolean;
}

/** A file's icon by kind, tinted (decorative: pair it with the name). */
export function FileIcon({ name, mime, kind, size = 20, tone = true }: FileIconProps) {
  const theme = useTheme();
  const k = kind ?? filePreviewKind(name, mime);
  const Icon = filePreviewIcon(k, name, mime);
  return <Icon size={size} color={tone ? filePreviewToneColor(theme, k, name, mime) : theme.colors.textMuted} strokeWidth={1.75} />;
}

type Loaded = { status: "loading" } | { status: "error" } | { status: "ready"; text: string; truncated: boolean };

/** The first 2 MB of a URL as text (a ranged request; servers that ignore Range are cut client-side). */
function useText(url: string | null): Loaded & { retry: () => void } {
  const [state, setState] = useState<Loaded>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!url) return;
    let live = true;
    setState({ status: "loading" });
    fetch(url, { headers: { Range: `bytes=0-${FILE_PREVIEW_TEXT_MAX - 1}` } })
      .then(async (res) => {
        if (!res.ok) throw new Error(String(res.status));
        const total = Number(/\/(\d+)$/.exec(res.headers.get("content-range") ?? "")?.[1] ?? "0");
        const text = await res.text();
        const truncated = res.status === 206 ? total > FILE_PREVIEW_TEXT_MAX : text.length > FILE_PREVIEW_TEXT_MAX;
        if (live) setState({ status: "ready", text: text.length > FILE_PREVIEW_TEXT_MAX ? text.slice(0, FILE_PREVIEW_TEXT_MAX) : text, truncated });
      })
      .catch(() => live && setState({ status: "error" }));
    return () => {
      live = false;
    };
  }, [url, attempt]);
  return { ...state, retry: () => setAttempt((a) => a + 1) };
}

function tokenColor(theme: NativeTheme, role: FilePreviewTokenRole): string | undefined {
  const c = theme.colors;
  const map: Record<FilePreviewTokenRole, string | undefined> = {
    keyword: c.primaryStrong,
    string: c.successStrong,
    number: c.warningStrong,
    comment: c.textMuted,
    function: c.infoStrong,
    type: c.accentStrong,
    attr: c.warningStrong,
    meta: c.dangerStrong,
    deletion: c.dangerStrong,
    addition: c.successStrong,
    plain: undefined,
  };
  return map[role];
}

const MAX_LINES = 3000;

function plain(text: string): FilePreviewToken[][] {
  return text
    .replace(/\r\n?/g, "\n")
    .replace(/\n$/, "")
    .split("\n")
    .map((l) => (l ? [{ text: l, role: "plain" as const }] : []));
}

/** Code with line numbers; highlighted once the highlighter (loaded on first use) is ready. */
function CodeView({ text, language, wrap }: { text: string; language: string; wrap?: boolean }) {
  const theme = useTheme();
  const base = useMemo(() => plain(text), [text]);
  const [lines, setLines] = useState(base);
  useEffect(() => {
    setLines(base);
    if (language === "plaintext") return;
    let live = true;
    void import("@scaffoldapp/shared/preview/highlight").then(
      (m) => live && setLines(m.filePreviewHighlight(text, language)),
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [text, language, base]);
  if (text.trim() === "") return <EmptyState compact title="This file is empty" />;
  const shown = lines.slice(0, MAX_LINES);
  const gutter = String(shown.length).length;
  const body = (
    <View style={{ paddingVertical: theme.space[3] }}>
      {shown.map((l, i) => (
        <View key={i} style={{ flexDirection: "row" }}>
          <Text role="caption" tone="subtle" style={{ fontFamily: MONO, width: (gutter + 2) * 8, textAlign: "right", paddingRight: theme.space[3] }}>
            {i + 1}
          </Text>
          <Text role="caption" style={{ fontFamily: MONO, paddingRight: theme.space[3], flexShrink: wrap ? 1 : 0 }}>
            {l.length === 0
              ? " "
              : l.map((t, j) => (
                  <Text key={j} role="caption" style={{ fontFamily: MONO, color: tokenColor(theme, t.role) ?? theme.colors.text, fontStyle: t.role === "comment" ? "italic" : "normal" }}>
                    {t.text}
                  </Text>
                ))}
          </Text>
        </View>
      ))}
      {lines.length > MAX_LINES ? (
        <Text role="caption" tone="muted" style={{ padding: theme.space[3] }}>
          Showing {MAX_LINES.toLocaleString()} of {lines.length.toLocaleString()} lines.
        </Text>
      ) : null}
    </View>
  );
  return (
    <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface, overflow: "hidden" }}>
      {wrap ? body : <ScrollView horizontal>{body}</ScrollView>}
    </View>
  );
}

function Inline({ nodes }: { nodes: FilePreviewMdInline[] }): ReactNode {
  const theme = useTheme();
  return nodes.map((n, i) => {
    switch (n.type) {
      case "text":
        return <Fragment key={i}>{n.text}</Fragment>;
      case "code":
        return (
          <Text key={i} style={{ fontFamily: MONO, backgroundColor: theme.colors.sunken }}>
            {n.text}
          </Text>
        );
      case "strong":
        return (
          <Text key={i} weight={600}>
            <Inline nodes={n.children} />
          </Text>
        );
      case "em":
        return (
          <Text key={i} style={{ fontStyle: "italic" }}>
            <Inline nodes={n.children} />
          </Text>
        );
      case "link":
        return (
          <Text key={i} tone="primary" style={{ textDecorationLine: "underline" }} onPress={() => void Linking.openURL(n.href).catch(() => undefined)} accessibilityRole="link">
            <Inline nodes={n.children} />
          </Text>
        );
    }
  });
}

const HEADING_ROLE = ["headline", "title", "titleSm", "bodyLg", "body", "body"] as const;

function MdBlock({ block }: { block: FilePreviewMdBlock }) {
  const theme = useTheme();
  switch (block.type) {
    case "heading":
      return (
        <Text role={HEADING_ROLE[block.level - 1]} weight={block.level > 3 ? 600 : undefined} accessibilityRole="header">
          <Inline nodes={block.children} />
        </Text>
      );
    case "paragraph":
      return (
        <Text>
          <Inline nodes={block.children} />
        </Text>
      );
    case "list":
      return (
        <View style={{ gap: theme.space[1] }}>
          {block.items.map((item, i) => (
            <View key={i} style={{ flexDirection: "row", gap: theme.space[2] }}>
              <Text tone="muted">{block.ordered ? `${block.start + i}.` : "•"}</Text>
              <Text style={{ flex: 1 }}>
                <Inline nodes={item} />
              </Text>
            </View>
          ))}
        </View>
      );
    case "quote":
      return (
        <View style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md, padding: theme.space[3] }}>
          <Text tone="muted">
            <Inline nodes={block.children} />
          </Text>
        </View>
      );
    case "code":
      return (
        <ScrollView horizontal style={{ backgroundColor: theme.colors.sunken, borderRadius: theme.radius.md }}>
          <Text role="caption" style={{ fontFamily: MONO, padding: theme.space[3] }}>
            {block.text}
          </Text>
        </ScrollView>
      );
    case "table":
      return (
        <ScrollView horizontal>
          <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md }}>
            {[block.header, ...block.rows].map((row, r) => (
              <View key={r} style={{ flexDirection: "row", borderTopWidth: r ? 1 : 0, borderColor: theme.colors.border, backgroundColor: r ? undefined : theme.colors.sunken }}>
                {row.map((c, i) => (
                  <Text key={i} role="caption" weight={r ? undefined : 600} style={{ width: 140, padding: theme.space[2] }}>
                    <Inline nodes={c} />
                  </Text>
                ))}
              </View>
            ))}
          </View>
        </ScrollView>
      );
    case "rule":
      return <View style={{ height: 1, backgroundColor: theme.colors.border }} />;
  }
}

function MarkdownView({ text }: { text: string }) {
  const theme = useTheme();
  const blocks = useMemo(() => filePreviewParseMarkdown(text), [text]);
  if (blocks.length === 0) return <EmptyState compact title="This file is empty" />;
  return (
    <View style={{ gap: theme.space[3], padding: theme.space[4], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface }}>
      {blocks.map((b, i) => (
        <MdBlock key={i} block={b} />
      ))}
    </View>
  );
}

const ROW_H = 40;
const COL_W = 140;

/** CSV or TSV as a virtualised table (rows recycle; columns scroll sideways). */
function CsvView({ text, name }: { text: string; name: string }) {
  const theme = useTheme();
  const { header, rows, truncated } = useMemo(() => {
    const parsed = filePreviewParseCsv(text, 50_001, filePreviewCsvDelimiter(name, text));
    const [h, ...rest] = parsed.rows;
    return { header: h ?? [], rows: rest, truncated: parsed.truncated };
  }, [text, name]);
  if (header.length === 0) return <EmptyState compact title="This file is empty" />;
  const cols = header.length;
  const cell = { width: COL_W, height: ROW_H, paddingHorizontal: theme.space[2], justifyContent: "center" as const, borderRightWidth: 1, borderColor: theme.colors.border };
  return (
    <View style={{ gap: theme.space[2] }}>
      <ScrollView horizontal style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface }}>
        <View style={{ width: cols * COL_W, height: Math.min(560, (rows.length + 1) * ROW_H) }}>
          <View style={{ flexDirection: "row", backgroundColor: theme.colors.sunken, borderBottomWidth: 1, borderColor: theme.colors.border }}>
            {header.map((h, i) => (
              <View key={i} style={cell}>
                <Text role="caption" weight={600} numberOfLines={1}>
                  {h}
                </Text>
              </View>
            ))}
          </View>
          <FlatList
            data={rows}
            keyExtractor={(_, i) => String(i)}
            getItemLayout={(_, i) => ({ length: ROW_H, offset: ROW_H * i, index: i })}
            initialNumToRender={20}
            windowSize={7}
            nestedScrollEnabled
            renderItem={({ item }) => (
              <View style={{ flexDirection: "row", borderBottomWidth: 1, borderColor: theme.colors.border }}>
                {header.map((_, i) => (
                  <View key={i} style={cell}>
                    <Text role="caption" numberOfLines={1}>
                      {item[i] ?? ""}
                    </Text>
                  </View>
                ))}
              </View>
            )}
          />
        </View>
      </ScrollView>
      <Text role="caption" tone="muted">
        {truncated ? `Showing the first ${rows.length.toLocaleString()} rows.` : `${rows.length.toLocaleString()} ${rows.length === 1 ? "row" : "rows"}`}
      </Text>
    </View>
  );
}

function DownloadButton({ downloadUrl, onDownload, variant = "secondary" }: Pick<FilePreviewProps, "downloadUrl" | "onDownload"> & { variant?: "primary" | "secondary" }) {
  if (onDownload)
    return (
      <Button variant={variant} size="md" leftIcon={Download} onPress={onDownload}>
        Download
      </Button>
    );
  if (downloadUrl)
    return (
      <Button variant={variant} size="md" leftIcon={Download} onPress={() => void Linking.openURL(downloadUrl).catch(() => undefined)}>
        Download
      </Button>
    );
  return null;
}

/** Kinds the in-app browser draws or plays for us (video, audio, PDF, office files). */
const BROWSER_KINDS = new Set<FilePreviewKind>(["video", "audio", "pdf", "office"]);

function openInBrowser(url: string) {
  void WebBrowser.openBrowserAsync(url).catch(() => Linking.openURL(url).catch(() => undefined));
}

function Card({ kind, children, title, ...p }: FilePreviewProps & { kind: FilePreviewKind; title: string; children?: ReactNode }) {
  const theme = useTheme();
  const label = filePreviewKindLabel(kind, p.name, p.mime);
  return (
    <View style={[{ alignItems: "center", gap: theme.space[4], paddingVertical: theme.space[10], paddingHorizontal: theme.space[4], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface }, p.style]}>
      <FileIcon name={p.name} mime={p.mime} kind={kind} size={48} />
      <View style={{ alignItems: "center", gap: theme.space[1] }}>
        <Text role="titleSm" align="center">
          {title}
        </Text>
        <Text role="caption" tone="muted">
          {label}
          {p.size != null ? ` · ${fileFormatSize(p.size)}` : ""}
        </Text>
      </View>
      <View style={{ flexDirection: "row", flexWrap: "wrap", justifyContent: "center", gap: theme.space[2] }}>{children}</View>
    </View>
  );
}

function FullPreview(p: FilePreviewProps) {
  const theme = useTheme();
  const kind = filePreviewKind(p.name, p.mime);
  const [zoom, setZoom] = useState(false);
  const [broken, setBroken] = useState(false);
  const text = filePreviewIsText(kind);
  const loaded = useText(text ? p.url : null);
  const [mode, setMode] = useState<"rendered" | "source">("rendered");

  if (kind === "image" && !broken) {
    return (
      <View style={p.style}>
        <Pressable onPress={() => setZoom(true)} accessibilityRole="imagebutton" accessibilityLabel={`${p.name}. Open to zoom.`}>
          <Image source={{ uri: p.url }} style={{ width: "100%", aspectRatio: 1, borderRadius: theme.radius.lg, backgroundColor: theme.colors.sunken }} contentFit="contain" onError={() => setBroken(true)} />
        </Pressable>
        <PhotoLightbox photos={[{ id: p.url, url: p.url, caption: p.name }]} open={zoom} onClose={() => setZoom(false)} />
      </View>
    );
  }
  if (BROWSER_KINDS.has(kind) || kind === "image") {
    return (
      <Card {...p} kind={kind} title={broken ? "Couldn't show this image" : kind === "video" || kind === "audio" ? `Play this ${kind === "video" ? "video" : "recording"}` : `Open to view this ${filePreviewKindLabel(kind, p.name, p.mime).toLowerCase()}`}>
        <Button size="md" leftIcon={ExternalLink} onPress={() => openInBrowser(p.url)}>
          {kind === "video" || kind === "audio" ? "Play" : "Open"}
        </Button>
        <DownloadButton {...p} />
      </Card>
    );
  }
  if (!text) {
    return (
      <Card {...p} kind={kind} title={`No preview for ${kind === "other" ? "this file" : `${filePreviewKindLabel(kind, p.name, p.mime).toLowerCase()} files`}`}>
        <DownloadButton {...p} variant="primary" />
      </Card>
    );
  }
  if (loaded.status === "loading") return <Skeleton height={320} />;
  if (loaded.status === "error") return <ErrorState compact title="Couldn't load the file" onRetry={loaded.retry} />;
  const json = kind === "json" && !loaded.truncated ? filePreviewFormatJson(loaded.text) : null;
  const rich = kind === "markdown" || kind === "csv" || kind === "svg" || (kind === "json" && !!json?.ok);
  const source = !rich || mode === "source";
  let body: ReactNode;
  if (source) body = <CodeView text={loaded.text} language={kind === "text" ? "plaintext" : kind === "json" ? "json" : kind === "svg" ? "xml" : filePreviewLanguage(p.name, p.mime)} wrap={kind === "text"} />;
  else if (kind === "markdown") body = <MarkdownView text={loaded.text} />;
  else if (kind === "csv") body = <CsvView text={loaded.text} name={p.name} />;
  else if (kind === "json" && json?.ok) body = <CodeView text={json.text} language="json" />;
  else
    body = (
      <Image
        source={{ uri: `data:image/svg+xml;utf8,${encodeURIComponent(loaded.text)}` }}
        style={{ width: "100%", aspectRatio: 1, borderRadius: theme.radius.lg, backgroundColor: theme.colors.sunken }}
        contentFit="contain"
        accessibilityLabel={p.name}
      />
    );
  return (
    <View style={[{ gap: theme.space[3] }, p.style]}>
      {loaded.truncated ? <Banner tone="info" title={`Showing the first ${fileFormatSize(FILE_PREVIEW_TEXT_MAX)}.`} action={p.onDownload ? { label: "Download", onPress: p.onDownload } : undefined} /> : null}
      {kind === "json" && json && !json.ok ? <Banner tone="warning" title={`This isn't valid JSON: ${json.error}`} /> : null}
      {rich ? (
        <SegmentedControl
          accessibilityLabel="Show as"
          value={mode}
          onValueChange={setMode}
          options={[
            { value: "rendered", label: kind === "csv" ? "Table" : kind === "svg" ? "Image" : kind === "json" ? "Formatted" : "Preview" },
            { value: "source", label: "Source" },
          ]}
        />
      ) : null}
      {body}
    </View>
  );
}

/**
 * Shows a file from a URL on iOS and Android: images (pinch, pan, double tap in a full-screen
 * viewer), text, code (highlighted), Markdown (drawn from a parsed tree), CSV/TSV (virtualised),
 * JSON (formatted) and SVG (drawn as an image). Video, audio, PDF and office files open in the
 * in-app browser, which plays or draws them; anything else is an icon, size and Download.
 * `compact` gives an attachment-sized thumbnail or card that opens the full preview.
 */
export function FilePreview(p: FilePreviewProps) {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  if (!p.compact) return <FullPreview {...p} />;
  const kind = filePreviewKind(p.name, p.mime);
  if (kind === "image") {
    return (
      <>
        <Pressable onPress={() => setOpen(true)} accessibilityRole="imagebutton" accessibilityLabel={`Open ${p.name}`} style={p.style}>
          <Image source={{ uri: p.thumbnailUrl ?? p.url }} style={{ width: 240, height: 180, maxWidth: "100%", borderRadius: theme.radius.lg, backgroundColor: theme.colors.sunken }} contentFit="cover" />
        </Pressable>
        <PhotoLightbox photos={[{ id: p.url, url: p.url, caption: p.name }]} open={open} onClose={() => setOpen(false)} />
      </>
    );
  }
  const caption = `${filePreviewKindLabel(kind, p.name, p.mime)}${p.size != null ? ` · ${fileFormatSize(p.size)}` : ""}`;
  const text = filePreviewIsText(kind);
  return (
    <View style={[{ flexDirection: "row", alignItems: "center", gap: theme.space[1], borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.lg, backgroundColor: theme.colors.surface, padding: theme.space[1] }, p.style]}>
      <Pressable
        onPress={() => (text ? setOpen(true) : BROWSER_KINDS.has(kind) ? openInBrowser(p.url) : p.onDownload ? p.onDownload() : p.downloadUrl ? openInBrowser(p.downloadUrl) : undefined)}
        accessibilityRole="button"
        accessibilityLabel={`Open ${p.name}`}
        style={({ pressed }) => ({ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: theme.space[3], padding: theme.space[2], borderRadius: theme.radius.md, backgroundColor: pressed ? theme.colors.sunken : undefined })}
      >
        <FileIcon name={p.name} mime={p.mime} kind={kind} size={32} />
        <View style={{ flex: 1, minWidth: 0 }}>
          <Text role="label" weight={600} numberOfLines={1}>
            {p.name}
          </Text>
          <Text role="caption" tone="muted" numberOfLines={1}>
            {caption}
          </Text>
        </View>
      </Pressable>
      {p.onDownload || p.downloadUrl ? <IconButton label={`Download ${p.name}`} icon={Download} variant="ghost" onPress={() => (p.onDownload ? p.onDownload() : openInBrowser(p.downloadUrl!))} /> : null}
      {text ? <TextModal {...p} open={open} onClose={() => setOpen(false)} /> : null}
    </View>
  );
}

/** The full text preview in a page sheet with a close button. */
function TextModal({ open, onClose, ...p }: FilePreviewProps & { open: boolean; onClose: () => void }) {
  const theme = useTheme();
  return (
    <Modal visible={open} animationType="slide" presentationStyle="pageSheet" onRequestClose={onClose}>
      <View style={{ flex: 1, backgroundColor: theme.colors.canvas }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[3], padding: theme.space[4], borderBottomWidth: 1, borderColor: theme.colors.border }}>
          <FileIcon name={p.name} mime={p.mime} />
          <Text role="titleSm" numberOfLines={1} style={{ flex: 1 }}>
            {p.name}
          </Text>
          <Button variant="ghost" size="md" onPress={onClose}>
            Done
          </Button>
        </View>
        <ScrollView contentContainerStyle={{ padding: theme.space[4] }}>
          <FullPreview {...p} compact={false} />
        </ScrollView>
      </View>
    </Modal>
  );
}
