import {
  docsIsTextBlock,
  docsListNumbers,
  docsPlain,
  docsTitle,
  useDocsFileUrl,
  type DocsBlock,
  type DocsRefs,
  type DocsTextBlockType,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { Avatar, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router } from "expo-router";
import { Check, ChevronRight, Download, FileText, Globe, Lock } from "lucide-react-native";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Linking, Platform, Pressable, ScrollView, TextInput, View } from "react-native";
import { RichText } from "./RichText";

const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

const ROLE: Record<DocsTextBlockType, "body" | "headline" | "title" | "titleSm" | "bodyLg"> = {
  paragraph: "body",
  heading_1: "headline",
  heading_2: "title",
  heading_3: "titleSm",
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
  renderDatabase?: (id: string) => ReactNode;
}

function ImageBlock({ b, token }: { b: DocsBlock; token: string | null }) {
  const theme = useTheme();
  const file = useDocsFileUrl(b.props.file_id, token);
  const [ratio, setRatio] = useState(4 / 3);
  const caption = docsPlain(b.props.caption);
  return (
    <View style={{ gap: theme.space[1], width: `${b.props.width ?? 100}%` }}>
      {file.data?.url ? (
        <Image
          source={{ uri: file.data.url }}
          accessibilityLabel={caption || b.props.name || "Image"}
          contentFit="cover"
          onLoad={(e) => e.source.width && e.source.height && setRatio(e.source.width / e.source.height)}
          style={{ width: "100%", aspectRatio: ratio, borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border }}
        />
      ) : file.isError ? (
        <Text role="label" tone="muted">
          This image isn't available.
        </Text>
      ) : (
        <Skeleton height={180} />
      )}
      {caption ? (
        <Text role="label" tone="muted">
          {caption}
        </Text>
      ) : null}
    </View>
  );
}

function FileBlock({ b, token }: { b: DocsBlock; token: string | null }) {
  const theme = useTheme();
  const [want, setWant] = useState(false);
  const file = useDocsFileUrl(want ? b.props.file_id : null, token);
  useEffect(() => {
    if (want && file.data?.url) {
      void Linking.openURL(file.data.url);
      setWant(false);
    }
  }, [want, file.data?.url]);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Download ${b.props.name ?? "file"}`}
      onPress={() => setWant(true)}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.base, paddingHorizontal: theme.space[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border, backgroundColor: pressed ? theme.colors.sunken : theme.colors.surface })}
    >
      <FileText size={20} color={theme.colors.textMuted} />
      <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
        {b.props.name ?? "File"}
      </Text>
      <Download size={18} color={theme.colors.textMuted} />
    </Pressable>
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
  const kids = props.children.get(b.id) ?? [];
  const viewers = props.viewersByBlock?.get(b.id);
  const textual = docsIsTextBlock(b.type);
  const editing = props.editable && props.editingId === b.id && textual;
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
      disabled={!props.editable || !textual}
      onPress={() => props.onEdit?.(b.id)}
      onLongPress={() => props.onBlockMenu?.(b)}
      accessibilityRole={props.editable ? "button" : "text"}
      accessibilityHint={props.editable ? "Edit. Long press for block actions." : undefined}
    >
      {docsPlain(b.props.text) ? (
        <RichText spans={b.props.text ?? []} refs={props.refs} role={role} {...(props.pageHref ? { pageHref: props.pageHref } : {})} style={checked ? { color: c.textMuted, textDecorationLine: "line-through" } : undefined} />
      ) : (
        <Text role={role} tone="subtle">
          {props.editable ? PLACEHOLDER[b.type as DocsTextBlockType] : " "}
        </Text>
      )}
    </Pressable>
  );
  let body: ReactNode;
  switch (b.type) {
    case "heading_1":
    case "heading_2":
    case "heading_3":
    case "paragraph":
      body = <View style={{ flexDirection: "row", paddingTop: b.type === "heading_1" ? s[5] : b.type === "heading_2" ? s[4] : b.type === "heading_3" ? s[3] : 0 }}>{text}</View>;
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
            accessibilityState={{ checked, disabled: !props.editable }}
            accessibilityLabel={checked ? "Done" : "Not done"}
            disabled={!props.editable}
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
          <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} accessibilityLabel={open ? "Collapse" : "Expand"} onPress={() => setOpen((o) => !o)} hitSlop={12} style={{ width: s[6], height: 32, alignItems: "center", justifyContent: "center" }}>
            <ChevronRight size={16} color={c.textMuted} style={{ transform: [{ rotate: open ? "90deg" : "0deg" }] }} />
          </Pressable>
          {text}
        </View>
      );
      break;
    case "quote":
      body = <View style={{ flexDirection: "row", borderLeftWidth: 2, borderLeftColor: c.borderStrong, paddingLeft: s[3] }}>{text}</View>;
      break;
    case "callout":
      body = (
        <View style={{ flexDirection: "row", gap: s[2], backgroundColor: c.sunken, borderRadius: theme.radius.md, paddingHorizontal: s[3], paddingVertical: s[2] }}>
          <Text role="bodyLg">{b.props.icon ?? "💡"}</Text>
          {text}
        </View>
      );
      break;
    case "code":
      body = (
        <Pressable onLongPress={() => props.onBlockMenu?.(b)} disabled={!props.editable}>
          <ScrollView horizontal style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ padding: s[3] }}>
            <Text role="label" style={{ fontFamily: MONO }}>
              {b.props.code}
            </Text>
          </ScrollView>
        </Pressable>
      );
      break;
    case "divider":
      body = (
        <Pressable onLongPress={() => props.onBlockMenu?.(b)} disabled={!props.editable} style={{ paddingVertical: s[3] }} accessibilityRole="none" accessibilityLabel="Divider">
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
      body = (
        <Pressable
          accessibilityRole="link"
          onPress={() => b.props.url && /^https?:/i.test(b.props.url) && void Linking.openURL(b.props.url)}
          onLongPress={() => props.onBlockMenu?.(b)}
          style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, paddingHorizontal: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
        >
          <Globe size={20} color={c.textMuted} />
          <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
            {b.props.title || b.props.url}
          </Text>
        </Pressable>
      );
      break;
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
      body = b.props.database_id && props.renderDatabase ? props.renderDatabase(b.props.database_id) : null;
      break;
  }
  return (
    <View style={{ gap: s[0.5] }}>
      <View style={{ flexDirection: "row", alignItems: "flex-start", gap: s[1] }}>
        <View style={{ flex: 1 }}>{body}</View>
        {viewers?.length ? (
          <View accessibilityLabel={`${viewers.map((v) => v.user.name).join(", ")} editing`} style={{ paddingTop: s[1] }}>
            <Avatar name={viewers[0]!.user.name} src={viewers[0]!.user.avatar_url} size="xs" />
          </View>
        ) : null}
      </View>
      {kids.length && (b.type !== "toggle" || open) ? (
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
