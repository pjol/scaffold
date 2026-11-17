import {
  docsEmbedName,
  docsFolds,
  docsIsTextBlock,
  docsListNumbers,
  docsPlain,
  docsTitle,
  useDocsFileUrl,
  useDocsLinkPreview,
  type DocsBlock,
  type DocsRefs,
  type DocsTextBlockType,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { Avatar, Button, FilePreview, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { AppWindow, ArrowUpRight, Check, ChevronRight, FileText, Globe, ListTree, Lock, RefreshCw, Sigma } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { Linking, Platform, Pressable, ScrollView, TextInput, View } from "react-native";
import { ConnectedPageCard } from "./ConnectedPageCard";
import { RichText, useSwatchStyle } from "./RichText";

const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

const ROLE: Record<DocsTextBlockType, "body" | "headline" | "title" | "titleSm" | "bodyLg"> = {
  paragraph: "body",
  heading_1: "headline",
  heading_2: "title",
  heading_3: "titleSm",
  heading_4: "bodyLg",
  bulleted_list: "body",
  numbered_list: "body",
  to_do: "body",
  toggle: "body",
  quote: "bodyLg",
  callout: "body",
};

const PLACEHOLDER: Record<DocsTextBlockType, string> = {
  paragraph: "Write something",
  heading_1: "Heading 1",
  heading_2: "Heading 2",
  heading_3: "Heading 3",
  heading_4: "Heading 4",
  bulleted_list: "List",
  numbered_list: "List",
  to_do: "To-do",
  toggle: "Toggle",
  quote: "Quote",
  callout: "Callout",
};

export interface BlockListProps {
  children: ReadonlyMap<string, readonly DocsBlock[]>;
  refs: DocsRefs;
  editable: boolean;
  token?: string | null;
  editingId?: string | null;
  viewersByBlock?: ReadonlyMap<string, DocsViewer[]>;
  pageHref?: (id: string) => string | null;
  onEdit?: (id: string | null) => void;
  onChangeText?: (b: DocsBlock, text: string) => void;
  onToggleTodo?: (b: DocsBlock) => void;
  onBlockMenu?: (b: DocsBlock) => void;
  onBackspaceEmpty?: (b: DocsBlock) => void;
  /** A database block: its database and the block (linked views keep their own views per block). */
  renderDatabase?: (id: string, blockId: string) => ReactNode;
  /** Connected pages: blocks shown but not changed here. */
  readOnly?: ReadonlySet<string>;
  /** The workspace connected-page blocks open in. */
  orgId?: string | null;
}

/** Opens a link in the in-app browser (embeds, PDFs and media without a native player). */
function openInBrowser(url: string | undefined) {
  if (url && /^https:/i.test(url)) void WebBrowser.openBrowserAsync(url);
  else if (url && /^(http:|mailto:)/i.test(url)) void Linking.openURL(url);
}

function useFileLink(b: DocsBlock, token: string | null) {
  // Connected pages carry the platform's link instead of an upload.
  const file = useDocsFileUrl(b.props.url ? null : b.props.file_id, token);
  return { url: b.props.url || file.data?.url || null, isError: !b.props.url && file.isError };
}

function Caption({ b }: { b: DocsBlock }) {
  const caption = docsPlain(b.props.caption);
  return caption ? (
    <Text role="label" tone="muted">
      {caption}
    </Text>
  ) : null;
}

function alignOf(a: string | undefined): "flex-start" | "center" | "flex-end" {
  return a === "left" ? "flex-start" : a === "right" ? "flex-end" : "center";
}

function ImageBlock({ b, token }: { b: DocsBlock; token: string | null }) {
  const theme = useTheme();
  const { url: src, isError } = useFileLink(b, token);
  const [ratio, setRatio] = useState(4 / 3);
  const caption = docsPlain(b.props.caption);
  return (
    <View style={{ gap: theme.space[1], alignItems: alignOf(b.props.align) }}>
      <View style={{ width: `${b.props.width ?? 100}%` }}>
        {src ? (
          <Image
            source={{ uri: src }}
            accessibilityLabel={b.props.alt || caption || b.props.name || "Image"}
            contentFit="cover"
            onLoad={(e) => e.source.width && e.source.height && setRatio(e.source.width / e.source.height)}
            style={{ width: "100%", aspectRatio: ratio, borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border }}
          />
        ) : isError ? (
          <Text role="label" tone="muted">
            This image isn&apos;t available.
          </Text>
        ) : (
          <Skeleton height={180} />
        )}
      </View>
      <Caption b={b} />
    </View>
  );
}

/** Video, audio, PDF and other files through the core preview kit (players and PDFs open in the in-app browser). */
function FileBlock({ b, token }: { b: DocsBlock; token: string | null }) {
  const theme = useTheme();
  const { url, isError } = useFileLink(b, token);
  return (
    <View style={{ gap: theme.space[1] }}>
      {url ? (
        <FilePreview compact url={url} name={b.props.name ?? "File"} mime={b.props.content_type ?? null} size={b.props.size ?? null} onDownload={() => openInBrowser(url)} />
      ) : isError ? (
        <Text role="label" tone="muted">
          This file isn&apos;t available.
        </Text>
      ) : (
        <Skeleton height={56} />
      )}
      <Caption b={b} />
    </View>
  );
}

function CardRow({ icon, title, subtitle, onPress, onLongPress }: { icon: ReactNode; title: string; subtitle?: string; onPress: () => void; onLongPress?: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  return (
    <Pressable
      accessibilityRole="link"
      onPress={onPress}
      onLongPress={onLongPress}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.base, paddingHorizontal: theme.space[3], paddingVertical: theme.space[2], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
    >
      {icon}
      <View style={{ flex: 1, gap: 2 }}>
        <Text role="body" numberOfLines={1}>
          {title}
        </Text>
        {subtitle ? (
          <Text role="caption" tone="muted" numberOfLines={2}>
            {subtitle}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function host(url: string | undefined): string {
  return (/^https?:\/\/([^/?#]+)/i.exec(url ?? "")?.[1] ?? url ?? "").replace(/^www\./, "");
}

function BookmarkBlock({ b, token, onLongPress }: { b: DocsBlock; token: string | null; onLongPress?: () => void }) {
  const theme = useTheme();
  const preview = useDocsLinkPreview(b.props.preview_id, token);
  const p = preview.data;
  const icon = p?.icon_url ? <Image source={{ uri: p.icon_url }} style={{ width: 20, height: 20, borderRadius: theme.radius.xs }} accessibilityIgnoresInvertColors /> : <Globe size={20} color={theme.colors.textMuted} />;
  return (
    <View style={{ gap: theme.space[1] }}>
      <CardRow icon={icon} title={b.props.title || p?.title || host(b.props.url)} subtitle={[b.props.description || p?.description, b.props.site_name || p?.site_name || host(b.props.url)].filter(Boolean).join(" · ")} onPress={() => openInBrowser(b.props.url)} {...(onLongPress ? { onLongPress } : {})} />
      <Caption b={b} />
    </View>
  );
}

function TextInputBlock({ b, role, placeholder, onChangeText, onDone, onBackspaceEmpty }: { b: DocsBlock; role: string; placeholder: string; onChangeText: (t: string) => void; onDone: () => void; onBackspaceEmpty: () => void }) {
  const theme = useTheme();
  const ref = useRef<TextInput>(null);
  const value = docsPlain(b.props.text);
  const t = theme.type[role as keyof typeof theme.type];
  return (
    <TextInput
      ref={ref}
      autoFocus
      multiline
      value={value}
      placeholder={placeholder}
      placeholderTextColor={theme.colors.textSubtle}
      onChangeText={onChangeText}
      onBlur={onDone}
      onKeyPress={(e) => {
        if (e.nativeEvent.key === "Backspace" && value === "") onBackspaceEmpty();
      }}
      accessibilityLabel={placeholder}
      style={[t as object, { flex: 1, color: theme.colors.text, paddingVertical: theme.space[1] }]}
    />
  );
}

function Row({ b, props, depth, number }: { b: DocsBlock; props: BlockListProps; depth: number; number: number | undefined }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [open, setOpen] = useState(false);
  const swatch = useSwatchStyle();
  const kids = props.children.get(b.id) ?? [];
  const folds = docsFolds(b.type, b.props);
  const viewers = props.viewersByBlock?.get(b.id);
  const textual = docsIsTextBlock(b.type);
  const editable = props.editable && !props.readOnly?.has(b.id);
  const editing = editable && props.editingId === b.id && textual;
  const role = textual ? ROLE[b.type as DocsTextBlockType] : "body";
  const checked = b.type === "to_do" && !!b.props.checked;
  const text = editing ? (
    <TextInputBlock
      b={b}
      role={role}
      placeholder={PLACEHOLDER[b.type as DocsTextBlockType]}
      onChangeText={(t) => props.onChangeText?.(b, t)}
      onDone={() => props.onEdit?.(null)}
      onBackspaceEmpty={() => props.onBackspaceEmpty?.(b)}
    />
  ) : (
    <Pressable
      style={{ flex: 1, paddingVertical: s[1] }}
      disabled={!editable || !textual}
      onPress={() => props.onEdit?.(b.id)}
      onLongPress={() => props.onBlockMenu?.(b)}
      accessibilityRole={editable ? "button" : "text"}
      accessibilityHint={editable ? "Edit. Long press for block actions." : undefined}
    >
      {docsPlain(b.props.text) ? (
        <RichText spans={b.props.text ?? []} refs={props.refs} role={role} {...(props.pageHref ? { pageHref: props.pageHref } : {})} style={checked ? { color: c.textMuted, textDecorationLine: "line-through" } : swatch(b.props.color)?.color ? { color: swatch(b.props.color)!.color! } : undefined} />
      ) : (
        <Text role={role} tone="subtle">
          {editable ? PLACEHOLDER[b.type as DocsTextBlockType] : " "}
        </Text>
      )}
    </Pressable>
  );
  let body: ReactNode;
  const chevron = (
    <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={open ? "Collapse" : "Expand"} onPress={() => setOpen((o) => !o)} hitSlop={12} style={{ width: s[6], height: 32, alignItems: "center", justifyContent: "center" }}>
      <ChevronRight size={16} color={c.textMuted} style={{ transform: [{ rotate: open ? "90deg" : "0deg" }] }} />
    </Pressable>
  );
  const longPress = () => props.onBlockMenu?.(b);
  switch (b.type) {
    case "heading_1":
    case "heading_2":
    case "heading_3":
    case "heading_4":
    case "paragraph":
      body = (
        <View style={{ flexDirection: "row", alignItems: "flex-start", paddingTop: b.type === "heading_1" ? s[5] : b.type === "heading_2" ? s[4] : b.type === "heading_3" ? s[3] : b.type === "heading_4" ? s[2] : 0 }}>
          {folds ? chevron : null}
          {text}
        </View>
      );
      break;
    case "bulleted_list":
      body = (
        <View style={{ flexDirection: "row", alignItems: "flex-start" }}>
          <View style={{ width: s[6], height: 32, alignItems: "center", justifyContent: "center" }}>
            <View style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: c.text }} />
          </View>
          {text}
        </View>
      );
      break;
    case "numbered_list":
      body = (
        <View style={{ flexDirection: "row", alignItems: "flex-start" }}>
          <Text role="body" tabular style={{ minWidth: s[6], paddingTop: s[1] }}>
            {number ?? 1}.
          </Text>
          {text}
        </View>
      );
      break;
    case "to_do":
      body = (
        <View style={{ flexDirection: "row", alignItems: "flex-start" }}>
          <Pressable
            accessibilityRole="checkbox"
            accessibilityState={{ checked, disabled: !editable }}
            accessibilityLabel={checked ? "Done" : "Not done"}
            disabled={!editable}
            onPress={() => props.onToggleTodo?.(b)}
            hitSlop={12}
            style={{ width: s[6], height: 32, alignItems: "center", justifyContent: "center" }}
          >
            <View style={{ width: 18, height: 18, borderRadius: theme.radius.xs, borderWidth: 1.5, borderColor: checked ? c.primary : c.borderStrong, backgroundColor: checked ? c.primary : c.surface, alignItems: "center", justifyContent: "center" }}>
              {checked ? <Check size={12} color={c.primaryFg} strokeWidth={3} /> : null}
            </View>
          </Pressable>
          {text}
        </View>
      );
      break;
    case "toggle":
      body = (
        <View style={{ flexDirection: "row", alignItems: "flex-start" }}>
          {chevron}
          {text}
        </View>
      );
      break;
    case "quote":
      body = <View style={{ flexDirection: "row", borderLeftWidth: 2, borderLeftColor: c.borderStrong, paddingLeft: s[3] }}>{text}</View>;
      break;
    case "callout":
      body = (
        <View style={[{ flexDirection: "row", gap: s[2], backgroundColor: c.sunken, borderRadius: theme.radius.md, paddingHorizontal: s[3], paddingVertical: s[2] }, swatch(b.props.color)]}>
          <Text role="bodyLg">{b.props.icon ?? "💡"}</Text>
          {text}
        </View>
      );
      break;
    case "code":
      body = (
        <Pressable onLongPress={longPress} disabled={!editable} style={{ gap: s[1] }}>
          {b.props.language && b.props.language !== "plain" ? (
            <Text role="caption" tone="muted">
              {b.props.language === "mermaid" ? "Mermaid diagram (drawn on the web)" : b.props.language}
            </Text>
          ) : null}
          {b.props.wrap ? (
            <View style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md, padding: s[3] }}>
              <Text role="label" style={{ fontFamily: MONO }}>
                {b.props.code}
              </Text>
            </View>
          ) : (
            <ScrollView horizontal style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ padding: s[3] }}>
              <Text role="label" style={{ fontFamily: MONO }}>
                {b.props.code}
              </Text>
            </ScrollView>
          )}
          <Caption b={b} />
        </Pressable>
      );
      break;
    case "equation":
      // TeX source styled as math; equations are drawn with KaTeX on the web.
      body = (
        <Pressable onLongPress={longPress} accessibilityLabel={`Equation: ${b.props.expression ?? ""}`} style={{ flexDirection: "row", gap: s[2], alignItems: "flex-start", backgroundColor: c.sunken, borderRadius: theme.radius.md, padding: s[3] }}>
          <Sigma size={16} color={c.textMuted} />
          <Text role="body" style={{ fontFamily: MONO, flex: 1 }}>
            {b.props.expression || " "}
          </Text>
        </Pressable>
      );
      break;
    case "table_of_contents": {
      const heads = [...props.children.values()].flat().filter((x) => /^heading_/.test(x.type) && docsPlain(x.props.text).trim());
      body = (
        <View style={{ gap: s[1] }}>
          <View style={{ flexDirection: "row", gap: s[2], alignItems: "center" }}>
            <ListTree size={16} color={c.textMuted} />
            <Text role="label" tone="muted">
              Contents
            </Text>
          </View>
          {heads.map((h) => (
            <Text key={h.id} role="label" style={{ paddingLeft: (Number(h.type.slice(-1)) - 1) * s[4], textDecorationLine: "underline", color: c.textMuted }}>
              {docsPlain(h.props.text)}
            </Text>
          ))}
        </View>
      );
      break;
    }
    case "breadcrumb":
      body = null;
      break;
    case "video":
    case "audio":
    case "pdf":
      body = <FileBlock b={b} token={props.token ?? null} />;
      break;
    case "embed":
      body = (
        <View style={{ gap: s[1] }}>
          <CardRow icon={<AppWindow size={20} color={c.textMuted} />} title={`${docsEmbedName(b.props.provider)}`} subtitle={host(b.props.url)} onPress={() => openInBrowser(b.props.url)} onLongPress={longPress} />
          <Caption b={b} />
        </View>
      );
      break;
    case "button": {
      const link = (b.props.actions ?? []).find((a) => a.type === "link");
      body = (
        <View style={{ flexDirection: "row" }}>
          <Button variant="secondary" size="md" disabled={!link} onPress={() => link && link.type === "link" && openInBrowser(link.url)} onLongPress={longPress}>
            {b.props.label || "Button"}
          </Button>
        </View>
      );
      break;
    }
    case "column_list":
      // Columns stack on phones, in order.
      body = (
        <View style={{ gap: s[3] }}>
          {kids.map((col) => (
            <View key={col.id} style={{ gap: s[1] }}>
              <BlockChildren parent={col.id} props={props} depth={depth + 2} />
            </View>
          ))}
        </View>
      );
      break;
    case "synced_block":
      body = b.props.source_id ? (
        <View style={{ flexDirection: "row", gap: s[2], alignItems: "center", borderWidth: 1, borderStyle: "dashed", borderColor: c.border, borderRadius: theme.radius.md, padding: s[3] }}>
          <RefreshCw size={16} color={c.accentStrong} />
          <Text role="label" tone="muted" style={{ flex: 1 }}>
            Synced from another page. Open it on the web to see and edit it.
          </Text>
        </View>
      ) : (
        <View style={{ borderWidth: 1, borderColor: c.accent, borderRadius: theme.radius.md, padding: s[2], gap: s[1] }}>
          <BlockChildren parent={b.id} props={props} depth={depth + 1} />
        </View>
      );
      break;
    case "divider":
      body = (
        <Pressable onLongPress={() => props.onBlockMenu?.(b)} disabled={!editable} style={{ paddingVertical: s[3] }} accessibilityRole="none" accessibilityLabel="Divider">
          <View style={{ height: 1, backgroundColor: c.border }} />
        </Pressable>
      );
      break;
    case "image":
      body = <ImageBlock b={b} token={props.token ?? null} />;
      break;
    case "file":
      body = <FileBlock b={b} token={props.token ?? null} />;
      break;
    case "bookmark":
      body = <BookmarkBlock b={b} token={props.token ?? null} onLongPress={longPress} />;
      break;
    case "link_to_page":
    case "page": {
      const ref = props.refs.pages[b.props.page_id ?? ""];
      const href = b.props.page_id ? (props.pageHref ? props.pageHref(b.props.page_id) : `/docs/${b.props.page_id}`) : null;
      const noAccess = !ref?.title;
      body = (
        <Pressable
          accessibilityRole="link"
          disabled={noAccess || !href}
          onPress={() => href && router.push(href as never)}
          onLongPress={() => props.onBlockMenu?.(b)}
          style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base, backgroundColor: pressed ? c.sunken : "transparent" })}
        >
          {noAccess ? <Lock size={16} color={c.textSubtle} /> : ref?.icon ? <Text role="body">{ref.icon}</Text> : <FileText size={18} color={c.textMuted} />}
          {b.type === "link_to_page" && !noAccess ? <ArrowUpRight size={14} color={c.textMuted} /> : null}
          <Text role="body" tone={noAccess ? "subtle" : "default"} style={noAccess ? undefined : { textDecorationLine: "underline" }}>
            {noAccess ? "No access" : docsTitle(ref?.title)}
          </Text>
        </Pressable>
      );
      break;
    }
    case "table":
      body = (
        <ScrollView horizontal>
          <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.sm }}>
            {(b.props.rows ?? []).map((row, r) => (
              <View key={r} style={{ flexDirection: "row" }}>
                {row.map((cell, ci) => {
                  const head = (b.props.header_row && r === 0) || (b.props.header_col && ci === 0);
                  return (
                    <View key={ci} style={{ width: 140, padding: s[2], borderRightWidth: ci < row.length - 1 ? 1 : 0, borderBottomWidth: r < (b.props.rows?.length ?? 0) - 1 ? 1 : 0, borderColor: c.border, backgroundColor: head ? c.sunken : "transparent" }}>
                      <RichText spans={cell} refs={props.refs} role="label" {...(head ? { weight: 600 as const } : {})} />
                    </View>
                  );
                })}
              </View>
            ))}
          </View>
        </ScrollView>
      );
      break;
    case "database":
      body = b.props.database_id && props.renderDatabase ? props.renderDatabase(b.props.database_id, b.id) : null;
      break;
    case "connected_page":
      body = <ConnectedPageCard b={b} orgId={props.orgId ?? null} token={props.token ?? null} />;
      break;
  }
  const color = b.type !== "callout" ? swatch(b.props.color) : null;
  const ownKids = b.type === "column_list" || b.type === "synced_block";
  return (
    <View style={{ gap: s[0.5] }}>
      <View style={{ flexDirection: "row", alignItems: "flex-start", gap: s[1] }}>
        <View style={[{ flex: 1 }, color?.backgroundColor ? { backgroundColor: color.backgroundColor, borderRadius: theme.radius.sm, paddingHorizontal: s[1] } : null]}>{body}</View>
        {viewers?.length ? (
          <View accessibilityLabel={`${viewers.map((v) => v.user.name).join(", ")} editing`} style={{ paddingTop: s[1] }}>
            <Avatar name={viewers[0]!.user.name} src={viewers[0]!.user.avatar_url} size="xs" />
          </View>
        ) : null}
      </View>
      {kids.length && !ownKids && (!folds || open) ? (
        <View style={{ paddingLeft: s[5] }}>
          <BlockChildren parent={b.id} props={props} depth={depth + 1} />
        </View>
      ) : null}
    </View>
  );
}

function BlockChildren({ parent, props, depth }: { parent: string; props: BlockListProps; depth: number }) {
  const kids = props.children.get(parent) ?? [];
  const numbers = docsListNumbers(kids);
  if (depth > 24) return null;
  return (
    <>
      {kids.map((b) => (
        <Row key={b.id} b={b} props={props} depth={depth} number={numbers.get(b.id)} />
      ))}
    </>
  );
}

/** Every block type rendered natively; text blocks become editable on tap. */
export function BlockList(props: BlockListProps) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[1] }}>
      <BlockChildren parent="" props={props} depth={0} />
    </View>
  );
}
