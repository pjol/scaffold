import {
  reposHighlightLines,
  reposInlineText,
  reposLanguageOf,
  reposParseMarkdown,
  reposResolvePath,
  reposTokenRole,
  useReposRefs,
  type ReposDiffFile,
  type ReposDiffLine,
  type ReposMdBlock,
  type ReposMdInline,
  type ReposThread,
  type ReposToken,
  type ReposTokenRole,
  type ReposUser,
} from "@scaffoldapp/shared";
import { Avatar, Badge, ListGroup, ListRow, Sheet, Text, useTheme, type NativeTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Check, ChevronDown, ChevronRight, GitBranch, Square, SquareCheck, Tag } from "lucide-react-native";
import { Fragment, memo, useMemo, useState, type ReactNode } from "react";
import { Linking, Platform, Pressable, ScrollView, View } from "react-native";

export const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

/** App path for a repository screen with query parameters. */
export function repoPath(repoId: string, sub: string, params: Record<string, string | number | undefined> = {}): string {
  const q = Object.entries(params)
    .filter(([, v]) => v !== undefined && v !== "")
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
    .join("&");
  return `/repos/${repoId}${sub ? `/${sub}` : ""}${q ? `?${q}` : ""}`;
}

function roleColor(theme: NativeTheme, role: ReposTokenRole): string | undefined {
  const c = theme.colors;
  switch (role) {
    case "keyword":
      return c.primaryStrong;
    case "string":
    case "addition":
      return c.successStrong;
    case "number":
    case "attr":
      return c.warningStrong;
    case "comment":
      return c.textMuted;
    case "function":
      return c.infoStrong;
    case "type":
      return c.accentStrong;
    case "meta":
    case "deletion":
      return c.dangerStrong;
    default:
      return undefined;
  }
}

/** One line of tokens as nested Text (colours from the theme). */
export const TokenText = memo(function TokenText({ tokens }: { tokens: ReposToken[] }) {
  const theme = useTheme();
  if (tokens.length === 0) return <>{" "}</>;
  return (
    <>
      {tokens.map((t, i) => {
        const role = reposTokenRole(t.scope);
        const color = roleColor(theme, role);
        return (
          <Text key={i} role="caption" style={{ fontFamily: MONO, ...(color ? { color } : {}), ...(role === "comment" ? { fontStyle: "italic" } : {}) }}>
            {t.text}
          </Text>
        );
      })}
    </>
  );
});

/** A file's code with line numbers. Lines wrap (phones are narrow); numbers stay aligned. */
export function CodeLines({ code, language, onLine }: { code: string; language: string; onLine?: (n: number) => void }) {
  const theme = useTheme();
  const lines = useMemo(() => reposHighlightLines(code, language), [code, language]);
  const width = String(lines.length).length * 8 + theme.space[4];
  return (
    <View style={{ paddingVertical: theme.space[2] }}>
      {lines.map((tokens, i) => (
        <Pressable key={i} disabled={!onLine} onPress={() => onLine?.(i + 1)} accessibilityLabel={`Line ${i + 1}`} style={{ flexDirection: "row", gap: theme.space[2], paddingRight: theme.space[3] }}>
          <Text role="caption" tone="subtle" tabular style={{ fontFamily: MONO, width, textAlign: "right" }}>
            {i + 1}
          </Text>
          <Text role="caption" selectable style={{ flex: 1, fontFamily: MONO }}>
            <TokenText tokens={tokens} />
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

/** A person's avatar and name ("Deleted user" for removed accounts). */
export function PersonLine({ user, trailing }: { user: ReposUser | null; trailing?: ReactNode }) {
  const theme = useTheme();
  const name = user?.display_name || "Deleted user";
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
      <Avatar name={name} src={user?.avatar_url ?? null} size="xs" />
      <Text role="label" weight={600}>
        {name}
      </Text>
      {trailing}
    </View>
  );
}

// ---- markdown ------------------------------------------------------------------

/** A README rendered with Text and View only (raw HTML shows as text). */
export function RepoMarkdown({ source, repoId, gitRef, dir }: { source: string; repoId: string; gitRef?: string | undefined; dir: string }) {
  const theme = useTheme();
  const blocks = useMemo(() => reposParseMarkdown(source), [source]);
  return (
    <View style={{ gap: theme.space[3] }}>
      {blocks.map((b, i) => (
        <MdBlock key={i} block={b} ctx={{ repoId, gitRef, dir }} />
      ))}
    </View>
  );
}

type MdCtx = { repoId: string; gitRef?: string | undefined; dir: string };

function follow(ctx: MdCtx, href: string) {
  if (/^(https?:|mailto:)/i.test(href)) {
    void Linking.openURL(href);
    return;
  }
  if (href.startsWith("#")) return;
  router.push(repoPath(ctx.repoId, "blob", { ref: ctx.gitRef, path: reposResolvePath(ctx.dir, href) }) as never);
}

function MdBlock({ block: b, ctx }: { block: ReposMdBlock; ctx: MdCtx }): ReactNode {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  switch (b.t) {
    case "heading":
      return (
        <Text selectable role={b.depth === 1 ? "title" : b.depth === 2 ? "titleSm" : "body"} weight={b.depth > 2 ? 600 : undefined}>
          <MdInline nodes={b.c} ctx={ctx} />
        </Text>
      );
    case "paragraph":
      return (
        <Text selectable>
          <MdInline nodes={b.c} ctx={ctx} />
        </Text>
      );
    case "text":
      return <Text selectable>{b.text}</Text>;
    case "code":
      return (
        <ScrollView horizontal style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ padding: s[3] }}>
          <Text role="caption" selectable style={{ fontFamily: MONO }}>
            {reposHighlightLines(b.text, b.lang || "plaintext").map((line, i) => (
              <Fragment key={i}>
                {i > 0 ? "\n" : null}
                <TokenText tokens={line} />
              </Fragment>
            ))}
          </Text>
        </ScrollView>
      );
    case "hr":
      return <View style={{ height: 1, backgroundColor: c.border }} />;
    case "quote":
      return (
        <View style={{ backgroundColor: c.sunken, borderRadius: theme.radius.md, paddingHorizontal: s[4], paddingVertical: s[3], gap: s[2] }}>
          {b.c.map((x, i) => (
            <MdBlock key={i} block={x} ctx={ctx} />
          ))}
        </View>
      );
    case "list":
      return (
        <View style={{ gap: s[1] }}>
          {b.items.map((it, i) => (
            <View key={i} style={{ flexDirection: "row", gap: s[2] }}>
              {it.task ? (
                <View style={{ minWidth: s[5], paddingTop: s[1] }} accessibilityLabel={it.checked ? "Done" : "Not done"}>
                  {it.checked ? <SquareCheck size={16} color={c.textMuted} /> : <Square size={16} color={c.textMuted} />}
                </View>
              ) : (
                <Text tone="muted" tabular style={{ minWidth: s[5] }}>
                  {b.ordered ? `${b.start + i}.` : "•"}
                </Text>
              )}
              <View style={{ flex: 1, gap: s[1] }}>
                {it.c.map((x, j) => (
                  <MdBlock key={j} block={x} ctx={ctx} />
                ))}
              </View>
            </View>
          ))}
        </View>
      );
    case "table":
      return (
        <ScrollView horizontal>
          <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md }}>
            {[b.header, ...b.rows].map((row, r) => (
              <View key={r} style={{ flexDirection: "row", backgroundColor: r === 0 ? c.sunken : undefined, borderTopWidth: r ? 1 : 0, borderColor: c.border }}>
                {row.map((cell, k) => (
                  <View key={k} style={{ minWidth: 96, paddingHorizontal: s[3], paddingVertical: s[2] }}>
                    <Text role="label" weight={r === 0 ? 600 : 400}>
                      {reposInlineText(cell)}
                    </Text>
                  </View>
                ))}
              </View>
            ))}
          </View>
        </ScrollView>
      );
  }
}

function MdInline({ nodes, ctx }: { nodes: ReposMdInline[]; ctx: MdCtx }): ReactNode {
  const theme = useTheme();
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.t) {
          case "text":
            return <Fragment key={i}>{n.text}</Fragment>;
          case "br":
            return <Fragment key={i}>{"\n"}</Fragment>;
          case "strong":
            return (
              <Text key={i} weight={600}>
                <MdInline nodes={n.c} ctx={ctx} />
              </Text>
            );
          case "em":
            return (
              <Text key={i} style={{ fontStyle: "italic" }}>
                <MdInline nodes={n.c} ctx={ctx} />
              </Text>
            );
          case "del":
            return (
              <Text key={i} style={{ textDecorationLine: "line-through" }}>
                <MdInline nodes={n.c} ctx={ctx} />
              </Text>
            );
          case "code":
            return (
              <Text key={i} style={{ fontFamily: MONO, backgroundColor: theme.colors.sunken }}>
                {n.text}
              </Text>
            );
          case "link":
            return (
              <Text key={i} accessibilityRole="link" style={{ color: theme.colors.primaryStrong, textDecorationLine: "underline" }} onPress={() => follow(ctx, n.href)}>
                <MdInline nodes={n.c} ctx={ctx} />
              </Text>
            );
          case "image":
            return (
              <Text key={i} accessibilityRole="link" style={{ color: theme.colors.primaryStrong, textDecorationLine: "underline" }} onPress={() => follow(ctx, n.src)}>
                {n.alt || "Image"}
              </Text>
            );
        }
      })}
    </>
  );
}

// ---- diffs ---------------------------------------------------------------------

const MAX_HIGHLIGHT = 3000;

/** One changed file as a unified diff, collapsible, with line threads under their lines. */
export function DiffFileCard({ file, threads = [], renderThread, onLine, highlight = true }: { file: ReposDiffFile; threads?: ReposThread[]; renderThread?: (t: ReposThread) => ReactNode; onLine?: (line: number, side: "old" | "new") => void; highlight?: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const big = file.additions + file.deletions > 300;
  const [open, setOpen] = useState(!big && file.status !== "deleted");
  const lang = reposLanguageOf(file.path);
  const lines = useMemo(() => file.hunks.flatMap((h) => [{ hunk: `@@ −${h.old_start},${h.old_lines} +${h.new_start},${h.new_lines} @@ ${h.header}` } as const, ...h.lines.map((l) => ({ line: l }))]), [file]);
  const doHighlight = highlight && lines.length < MAX_HIGHLIGHT;
  const tokens = (l: ReposDiffLine): ReposToken[] => (doHighlight ? (reposHighlightLines(l.text, lang)[0] ?? []) : l.text ? [{ text: l.text }] : []);
  const at = (side: "old" | "new", n: number) => threads.filter((t) => t.root.path === file.path && t.root.side === side && t.root.line === n && !t.outdated);
  return (
    <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.lg, overflow: "hidden", backgroundColor: c.surface }}>
      <Pressable
        onPress={() => setOpen(!open)}
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: 48, paddingHorizontal: theme.space[3], backgroundColor: c.sunken }}
      >
        {open ? <ChevronDown size={18} color={c.textMuted} /> : <ChevronRight size={18} color={c.textMuted} />}
        <Text role="label" numberOfLines={1} style={{ flex: 1, fontFamily: MONO }}>
          {file.path}
        </Text>
        <Text role="caption" tone="success" tabular>
          +{file.additions}
        </Text>
        <Text role="caption" tone="danger" tabular>
          −{file.deletions}
        </Text>
      </Pressable>
      {!open ? null : file.binary ? (
        <Text role="label" tone="muted" style={{ padding: theme.space[3] }}>
          Binary file not shown.
        </Text>
      ) : file.truncated ? (
        <Text role="label" tone="muted" style={{ padding: theme.space[3] }}>
          This file's diff is too large for a phone. Open it on the web.
        </Text>
      ) : (
        <View>
          {lines.map((row, i) => {
            if ("hunk" in row) {
              return (
                <Text key={i} role="caption" tone="info" style={{ fontFamily: MONO, backgroundColor: c.infoSoft, paddingHorizontal: theme.space[3], paddingVertical: theme.space[1] }}>
                  {row.hunk}
                </Text>
              );
            }
            const l = row.line;
            const side: "old" | "new" = l.type === "del" ? "old" : "new";
            const n = side === "old" ? l.old : l.new;
            const bg = l.type === "add" ? c.successSoft : l.type === "del" ? c.dangerSoft : undefined;
            const ts = n ? at(side, n) : [];
            return (
              <Fragment key={i}>
                <Pressable
                  disabled={!onLine || l.type === "note" || !n}
                  onPress={() => onLine?.(n, side)}
                  accessibilityLabel={`${l.type === "add" ? "Added" : l.type === "del" ? "Removed" : ""} line ${n}`}
                  style={{ flexDirection: "row", backgroundColor: bg, paddingRight: theme.space[3] }}
                >
                  <Text role="caption" tone="subtle" tabular style={{ fontFamily: MONO, width: 40, textAlign: "right", paddingRight: theme.space[2] }}>
                    {n || ""}
                  </Text>
                  <Text role="caption" tone="muted" style={{ fontFamily: MONO, width: 14 }}>
                    {l.type === "add" ? "+" : l.type === "del" ? "−" : " "}
                  </Text>
                  <Text role="caption" selectable style={{ flex: 1, fontFamily: MONO, ...(l.type === "note" ? { fontStyle: "italic" } : {}) }}>
                    {l.type === "note" ? l.text : <TokenText tokens={tokens(l)} />}
                  </Text>
                </Pressable>
                {ts.length && renderThread ? <View style={{ padding: theme.space[3], gap: theme.space[3], backgroundColor: c.canvas }}>{ts.map((t) => <Fragment key={t.root.id}>{renderThread(t)}</Fragment>)}</View> : null}
              </Fragment>
            );
          })}
        </View>
      )}
    </View>
  );
}

// ---- ref picker ----------------------------------------------------------------

/** Branch and tag picker in a sheet. */
export function RefSheet({ repoId, value, open, onClose, onPick }: { repoId: string; value: string; open: boolean; onClose: () => void; onPick: (ref: string) => void }) {
  const refs = useReposRefs(repoId);
  const items = refs.data?.items ?? [];
  return (
    <Sheet open={open} onClose={onClose} title="Switch branch or tag">
      <ScrollView style={{ maxHeight: 420 }}>
        <ListGroup>
          {items.map((r) => (
            <ListRow
              key={r.kind + r.name}
              title={r.name}
              leading={r.kind === "tag" ? <Tag size={20} /> : <GitBranch size={20} />}
              trailing={r.name === value ? <Check size={20} /> : r.default ? <Badge tone="neutral">default</Badge> : undefined}
              onPress={() => {
                onClose();
                onPick(r.name);
              }}
              chevron={false}
            />
          ))}
        </ListGroup>
      </ScrollView>
    </Sheet>
  );
}
