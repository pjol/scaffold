"use client";

import {
  DOCS_BLOCK_LABEL,
  DOCS_FILE_ACCEPT,
  DOCS_TURN_INTO,
  DOCS_TURN_INTO_KEYS,
  docsBlocksToMarkdown,
  docsBlocksToText,
  docsCodePoints,
  docsColorAt,
  docsConcat,
  docsDeleteRange,
  docsEmbedFor,
  docsEndpoints,
  docsFileBlockType,
  docsFolds,
  docsHasMark,
  docsHoldsChildren,
  docsInsertSpans,
  docsInsertText,
  docsIsStructural,
  docsIsTextBlock,
  docsKeyBetween,
  docsLinkAt,
  docsPlain,
  docsSafeLink,
  docsSetColor,
  docsSetLink,
  docsShortcut,
  docsSlice,
  docsSplit,
  docsTextToBlocks,
  docsToggleEquation,
  docsToggleMark,
  docsTurnIntoId,
  useDocsFetchLinkPreview,
  useEndpoints,
  type DocsBlock,
  type DocsBlockType,
  type DocsColor,
  type DocsComment,
  type DocsDraftBlock,
  type DocsFile,
  type DocsMark,
  type DocsMention,
  type DocsOp,
  type DocsPageSession,
  type DocsRefs,
  type DocsSessionSnapshot,
  type DocsSlashItem,
  type DocsSpan,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { cn, toast } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowUp, Copy, CornerDownRight, Keyboard, Link2, MessageSquarePlus, Palette, RefreshCw, Repeat2, Trash2, Type, Unlink } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent, type MouseEvent, type PointerEvent } from "react";
import { Floating, FloatingMenu, type FloatingMenuItem } from "../Floating";
import { BlockChildren } from "./BlockTree";
import { EditorContext, type DropPlace, type EditorContextValue, type EditorSelection, type NestedEditorProps } from "./context";
import { caretOnFirstLine, caretOnLastLine, caretRect, placeCaretAtX, type Decoration } from "./dom";
import {
  ColorPalette,
  DateForm,
  EmojiMenu,
  EquationForm,
  emojiAt,
  emojiCount,
  filteredSlash,
  InlineToolbar,
  LinkForm,
  MentionMenu,
  PASTE_ICONS,
  PasteMenu,
  recentSlash,
  rememberSlash,
  SlashMenu,
  slashIcon,
  UrlForm,
  useMentionChoices,
  type CaretMenuState,
  type MentionChoice,
  type PasteChoice,
  type ToolbarState,
} from "./menus";
import {
  columnCleanupOps,
  columnDropOps,
  columnsOps,
  draftOps,
  indentOps,
  moveBlocksOps,
  newBlock,
  outdentOps,
  positionAfter,
  positionAtEnd,
  positionBefore,
  shiftOps,
  siblingsOf,
  toDrafts,
  turnInto,
  visibleOrder,
} from "./ops";
import { htmlToBlocks } from "./paste";
import type { RichTextHandle } from "./RichText";
import { ShortcutsSheet } from "./ShortcutsSheet";
import { copyAndSync, SYNCED_PASTE_RE } from "./synced";

const MIME = "application/x-docs-blocks";
const LISTY = new Set<DocsBlockType>(["bulleted_list", "numbered_list", "to_do", "toggle"]);
type MediaKind = "image" | "video" | "audio" | "pdf" | "file";

export interface EditorProps {
  pageId: string;
  orgId: string;
  session: DocsPageSession<EditorSelection>;
  snap: DocsSessionSnapshot;
  refs: DocsRefs;
  editable: boolean;
  canComment: boolean;
  viewers: readonly DocsViewer[];
  commentCounts: Readonly<Record<string, number>>;
  threads: readonly DocsComment[];
  activeThread: string | null;
  onFocusBlock: (id: string | null) => void;
  onOpenComments: (blockId: string | null, threadId?: string) => void;
  onComment: (target: { blockId: string; start?: number; end?: number; quote?: string }) => void;
  onOpenPage: (id: string) => void;
  onCreateSubpage: () => Promise<string | null>;
  /** A new inline database; `viewType` is the first view (table, board, gallery, list, calendar, timeline, chart, form). */
  onCreateDatabase: (viewType?: string) => Promise<string | null>;
  onUpload: (file: File) => Promise<DocsFile>;
  onMoveToPage: (blockIds: string[]) => void;
  /** A connected page (Notion, Google Docs): what it can take and what stays read-only. */
  connected?: EditorConnected;
  /** Show only this block's children (a synced copy shows its original); null for the whole page. */
  root?: string | null;
}

/** How a connected page limits the editor: the platform takes these block types, can't move blocks, and keeps some blocks read-only ("Edit in Notion"). */
export interface EditorConnected {
  types: readonly DocsBlockType[];
  readOnly: ReadonlySet<string>;
  /** "Notion", "Google Docs". */
  platform: string;
  /** The page on the platform. */
  url: string;
  /** Date mentions are written as the platform's own (Notion). */
  dates?: boolean;
}

function isMod(e: { metaKey: boolean; ctrlKey: boolean }) {
  return e.metaKey || e.ctrlKey;
}

/** Offsets of an anchored thread's quote in a block (server offsets count code points). */
function decorate(plain: string, t: DocsComment, active: boolean): Decoration | null {
  if (!t.quote) return null;
  const chars = Array.from(plain);
  if (t.anchor_start !== null && t.anchor_end !== null && chars.slice(t.anchor_start, t.anchor_end).join("") === t.quote) {
    const start = chars.slice(0, t.anchor_start).join("").length;
    return { start, end: start + t.quote.length, id: t.id, active };
  }
  const at = plain.indexOf(t.quote);
  return at >= 0 ? { start: at, end: at + t.quote.length, id: t.id, active } : null;
}

/** Applies **x**, *x*, _x_, ~x~, ~~x~~, `x` and $x$ when the closing delimiter was just typed. */
function inlineShortcut(spans: DocsSpan[], caret: number): { spans: DocsSpan[]; caret: number; mark: DocsMark | "equation" } | null {
  const before = docsPlain(spans).slice(0, caret);
  const rules: [RegExp, number, DocsMark | "equation"][] = [
    [/(?:^|[^*])\*\*([^*\n]+)\*\*$/, 2, "bold"],
    [/(?:^|[^_])__([^_\n]+)__$/, 2, "bold"],
    [/(?:^|[^~])~~([^~\n]+)~~$/, 2, "strike"],
    [/(?:^|[^~\w])~([^~\n\s][^~\n]*)~$/, 1, "strike"],
    [/(?:^|[^`])`([^`\n]+)`$/, 1, "code"],
    [/(?:^|[^$\w])\$([^$\n\s\d][^$\n]*[^$\n\s]|[^$\n\s\d])\$$/, 1, "equation"],
    [/(?:^|[^*\w])\*([^*\n\s][^*\n]*)\*$/, 1, "italic"],
    [/(?:^|[^_\w])_([^_\n\s][^_\n]*)_$/, 1, "italic"],
  ];
  for (const [re, d, mark] of rules) {
    const m = re.exec(before);
    if (!m) continue;
    const inner = m[1]!;
    const closeStart = caret - d;
    const innerStart = closeStart - inner.length;
    const openStart = innerStart - d;
    if (docsSlice(spans, openStart, caret).some((s) => s.mention || s.code || s.equation)) continue;
    let next = docsDeleteRange(spans, closeStart, caret);
    next = docsDeleteRange(next, openStart, innerStart);
    next = mark === "equation" ? docsToggleEquation(next, openStart, openStart + inner.length) : docsToggleMark(next, openStart, openStart + inner.length, mark);
    return { spans: next, caret: caret - 2 * d, mark };
  }
  return null;
}

function loadOpen(pageId: string): Set<string> {
  try {
    return new Set(JSON.parse(window.localStorage.getItem(`scaffoldapp.docs.open.${pageId}`) ?? "[]") as string[]);
  } catch {
    return new Set();
  }
}

type MentionState = CaretMenuState & { mode: "all" | "people" | "pages" | "dates" | "link"; trigger: number; purpose: "mention" | "link_block" | "linked_db" };

/**
 * The block editor: one contenteditable per text block over the span model, keyboard editing (Enter splits,
 * Backspace merges, arrows cross blocks, Tab nests), "/" (every block and action), "@", "[[", "+" and ":" menus,
 * Markdown shortcuts, a formatting toolbar (marks, colours, equations, links), block selection (Escape, shift-click,
 * select all) with delete, move, turn into, colour, copy and paste, drag handles (and dragging beside a block makes
 * columns), file drops and pastes, undo and redo.
 */
export function Editor(props: EditorProps) {
  const { pageId, orgId, session, snap, refs, editable, canComment, viewers, commentCounts, threads, activeThread, connected } = props;
  const root = props.root ?? null;
  const api = useEndpoints(docsEndpoints);
  const fetchPreview = useDocsFetchLinkPreview();
  const handles = useRef(new Map<string, RichTextHandle>());
  const rows = useRef(new Map<string, HTMLElement>());
  const container = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const pendingFocus = useRef<{ id: string; at: number | "start" | "end"; end?: number } | null>(null);
  const focused = useRef<string | null>(null);
  const anchor = useRef<string | null>(null);
  const uploadTarget = useRef<{ blockId: string; kind: MediaKind } | null>(null);
  const lastColor = useRef<DocsColor | null>(null);
  // After **x** turns bold, the next typed text isn't: the mark stops at the caret.
  const sticky = useRef<{ blockId: string; at: number; mark: DocsMark } | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [slash, setSlash] = useState<CaretMenuState | null>(null);
  const [mention, setMention] = useState<MentionState | null>(null);
  const [emoji, setEmoji] = useState<CaretMenuState | null>(null);
  const [toolbar, setToolbar] = useState<ToolbarState | null>(null);
  const [linkForm, setLinkForm] = useState<{ blockId: string; start: number; end: number; rect: DOMRect; initial: string; existing: boolean } | null>(null);
  const [urlForm, setUrlForm] = useState<{ blockId: string; rect: DOMRect; mode: "bookmark" | "embed" } | null>(null);
  const [dateForm, setDateForm] = useState<{ blockId: string; start: number; rect: DOMRect; remind: boolean } | null>(null);
  const [eqForm, setEqForm] = useState<{ blockId: string; start: number; end: number; rect: DOMRect; initial: string; existing: boolean } | null>(null);
  const [pasteMenu, setPasteMenu] = useState<{ blockId: string; start: number; end: number; url: string; rect: DOMRect; index: number; choices: PasteChoice[] } | null>(null);
  const [blockMenu, setBlockMenu] = useState<{ ids: string[]; anchor: HTMLElement | DOMRect; view: "main" | "turn" | "color" } | null>(null);
  const [help, setHelp] = useState(false);
  const [drag, setDrag] = useState<{ ids: Set<string>; target: EditorContextValue["dropTarget"]; x: number; y: number; label: string } | null>(null);
  const [fileDrop, setFileDrop] = useState<string | null>(null);
  const [open, setOpen] = useState<Set<string>>(() => (typeof window === "undefined" ? new Set() : loadOpen(pageId)));

  const now = () => session.getSnapshot();
  const blockOf = (id: string) => now().byId.get(id);
  const textOf = (b: DocsBlock) => b.props.text ?? [];
  const isOpen = useCallback((id: string) => open.has(id), [open]);
  const toggleOpen = useCallback(
    (id: string, value?: boolean) => {
      setOpen((prev) => {
        const next = new Set(prev);
        if (value ?? !next.has(id)) next.add(id);
        else next.delete(id);
        try {
          window.localStorage.setItem(`scaffoldapp.docs.open.${pageId}`, JSON.stringify([...next].slice(-500)));
        } catch {
          // Private mode: toggles just don't persist.
        }
        return next;
      });
    },
    [pageId],
  );

  // ---- focus and selection

  const flushFocus = () => {
    const p = pendingFocus.current;
    if (!p) return;
    const h = handles.current.get(p.id);
    if (!h) return;
    pendingFocus.current = null;
    h.focus(p.at, p.end);
  };
  useLayoutEffect(() => flushFocus());

  const focusBlock = useCallback((id: string, at: number | "start" | "end" = "end", end?: number) => {
    setSelected((s) => (s.size ? new Set() : s));
    // Reveal it inside closed toggles and toggle headings.
    let p = blockOf(id)?.parent_id;
    while (p) {
      const pb = blockOf(p);
      if (pb && docsFolds(pb.type, pb.props)) toggleOpen(pb.id, true);
      p = pb?.parent_id;
    }
    pendingFocus.current = { id, at, ...(end !== undefined ? { end } : {}) };
    requestAnimationFrame(flushFocus);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const currentSel = (): EditorSelection => {
    if (selected.size) return { blocks: [...selected] };
    const id = focused.current;
    const c = id ? handles.current.get(id)?.caret() : null;
    return id && c ? { block: id, ...c } : null;
  };

  const apply = useCallback(
    (ops: DocsOp[], after?: EditorSelection, history: "push" | "merge" | "skip" = "push", key?: string) => {
      if (!editable || ops.length === 0) return;
      session.apply(ops, { history, ...(key ? { key } : {}), selectionBefore: currentSel(), ...(after !== undefined ? { selectionAfter: after } : {}) });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [session, editable, selected],
  );

  /** Tidies columns left empty by a move or delete (one more undo step's worth, merged). */
  const tidyColumns = () => {
    const ops = columnCleanupOps(now());
    if (ops.length) session.apply(ops, { history: "merge", key: "tidy-columns", selectionBefore: null });
  };

  const restore = (sel: EditorSelection | undefined) => {
    if (!sel) return;
    if ("blocks" in sel) selectBlocks(sel.blocks.filter((id) => blockOf(id)));
    else if (blockOf(sel.block)) focusBlock(sel.block, sel.start, sel.end);
  };
  const undo = () => restore(session.undo());
  const redo = () => restore(session.redo());

  const selectBlocks = (ids: string[], keepAnchor = false) => {
    setSelected(new Set(ids));
    if (!keepAnchor) anchor.current = ids[0] ?? null;
    setToolbar(null);
    setSlash(null);
    setMention(null);
    setEmoji(null);
    if (ids.length) {
      (document.activeElement as HTMLElement | null)?.blur?.();
      container.current?.focus({ preventScroll: true });
    }
  };

  const order = () => visibleOrder(now(), isOpen, root);

  // ---- text editing

  const closeMenusFor = (id: string) => {
    if (slash?.blockId === id) setSlash(null);
    if (mention?.blockId === id) setMention(null);
    if (emoji?.blockId === id) setEmoji(null);
  };

  const rectOfCaret = (id: string) => {
    const el = handles.current.get(id)?.el;
    return el ? caretRect(el) : new DOMRect();
  };

  const onTextChange = (b: DocsBlock, input: DocsSpan[], caret: { start: number; end: number } | null) => {
    if (!editable) return;
    const cur = blockOf(b.id) ?? b;
    const prevPlain = docsPlain(textOf(cur));
    let spans = input;
    const plain = docsPlain(spans);
    const c = caret?.start ?? plain.length;
    const collapsed = !caret || caret.start === caret.end;
    const inserted = collapsed && plain.length === prevPlain.length + 1 && c > 0 && plain.slice(0, c - 1) === prevPlain.slice(0, c - 1) ? plain[c - 1] : null;

    if (cur.type === "paragraph" && inserted === " " && !connected?.types.length) {
      const prefix = plain.slice(0, c - 1);
      const sc = /^\S{1,4}$/.test(prefix) ? docsShortcut(prefix) : null;
      if (sc && sc.type === "equation") {
        apply([{ op: "update", id: cur.id, type: "equation", props: { expression: docsPlain(docsSlice(spans, c)) } }], { block: cur.id, start: 0, end: 0 });
        closeMenusFor(cur.id);
        focusBlock(cur.id, 0);
        return;
      }
      if (sc && sc.type !== "divider" && sc.type !== "code") {
        apply([...turnInto(now(), cur, sc.type, sc.props), { op: "update", id: cur.id, props: { text: docsSlice(spans, c) } }], { block: cur.id, start: 0, end: 0 });
        closeMenusFor(cur.id);
        focusBlock(cur.id, 0);
        return;
      }
    } else if (cur.type === "paragraph" && inserted === " ") {
      const prefix = plain.slice(0, c - 1);
      const sc = /^\S{1,4}$/.test(prefix) ? docsShortcut(prefix) : null;
      if (sc && connected?.types.includes(sc.type) && sc.type !== "divider" && sc.type !== "code") {
        apply([...turnInto(now(), cur, sc.type, sc.props), { op: "update", id: cur.id, props: { text: docsSlice(spans, c) } }], { block: cur.id, start: 0, end: 0 });
        closeMenusFor(cur.id);
        focusBlock(cur.id, 0);
        return;
      }
    }
    if (cur.type === "paragraph" && plain === "```" && inserted === "`") {
      apply(turnInto(now(), { ...cur, props: { ...cur.props, text: [] } }, "code"), { block: cur.id, start: 0, end: 0 });
      focusBlock(cur.id, "start");
      return;
    }
    if (cur.type === "paragraph" && plain === "---" && inserted === "-") {
      const para = newBlock("paragraph", cur.parent_id, positionAfter(now(), cur));
      apply([{ op: "update", id: cur.id, type: "divider", props: {} }, para], { block: para.id, start: 0, end: 0 });
      focusBlock(para.id, 0);
      return;
    }
    let caretAfter = c;
    const st = sticky.current;
    if (st && st.blockId === cur.id) {
      sticky.current = null;
      const k = plain.length - prevPlain.length;
      if (collapsed && k > 0 && c === st.at + k && plain.slice(0, st.at) === prevPlain.slice(0, st.at) && docsHasMark(spans, st.at, c, st.mark)) {
        spans = docsToggleMark(spans, st.at, c, st.mark);
      }
    }
    if (inserted && "*_~`$".includes(inserted)) {
      const r = inlineShortcut(spans, c);
      if (r) {
        spans = r.spans;
        caretAfter = r.caret;
        if (r.mark !== "equation") sticky.current = { blockId: cur.id, at: r.caret, mark: r.mark };
      }
    }
    const before = (n: number) => plain[c - n - 1] ?? " ";
    // "/" and "@" menus; "[[" and "+" link or create a page; ":" emoji.
    if (inserted === "/" && (c === 1 || /\s/.test(before(1)))) {
      setSlash({ blockId: cur.id, start: c - 1, query: "", rect: rectOfCaret(cur.id), index: 0 });
    } else if (slash && slash.blockId === cur.id) {
      const q = plain.slice(slash.start + 1, c);
      if (c <= slash.start || plain[slash.start] !== "/" || q.length > 24 || /\n|\s\s/.test(q)) setSlash(null);
      else setSlash({ ...slash, query: q, index: 0 });
    }
    if (!connected && inserted === "@" && (c === 1 || /\s/.test(before(1)))) {
      setMention({ blockId: cur.id, start: c - 1, query: "", rect: rectOfCaret(cur.id), index: 0, mode: "all", trigger: 1, purpose: "mention" });
    } else if (!connected && inserted === "[" && plain.slice(c - 2, c) === "[[") {
      setMention({ blockId: cur.id, start: c - 2, query: "", rect: rectOfCaret(cur.id), index: 0, mode: "link", trigger: 2, purpose: "mention" });
    } else if (!connected && inserted === "+" && plain === "+" && cur.type === "paragraph") {
      setMention({ blockId: cur.id, start: 0, query: "", rect: rectOfCaret(cur.id), index: 0, mode: "link", trigger: 1, purpose: "link_block" });
    } else if (mention && mention.blockId === cur.id) {
      const q = plain.slice(mention.start + mention.trigger, c);
      const trig = plain.slice(mention.start, mention.start + mention.trigger);
      const want = mention.trigger === 2 ? "[[" : mention.purpose === "link_block" ? "+" : "@";
      if (c < mention.start + mention.trigger || (mention.trigger && trig !== want) || q.length > 40 || /\n/.test(q)) setMention(null);
      else setMention({ ...mention, query: q.replace(/\]\]$/, ""), index: 0 });
    }
    if (inserted && /[\w+-]/.test(inserted) && !emoji) {
      const m = /(?:^|\s):([\w+-]{2,24})$/.exec(plain.slice(0, c));
      if (m && emojiCount(m[1]!) > 0) setEmoji({ blockId: cur.id, start: c - m[1]!.length - 1, query: m[1]!, rect: rectOfCaret(cur.id), index: 0 });
    } else if (emoji && emoji.blockId === cur.id) {
      const q = plain.slice(emoji.start + 1, c);
      if (c <= emoji.start || plain[emoji.start] !== ":" || !/^[\w+-]{1,24}$/.test(q) || emojiCount(q) === 0) setEmoji(null);
      else setEmoji({ ...emoji, query: q, index: 0 });
    }
    apply([{ op: "update", id: cur.id, props: { text: spans } }], { block: cur.id, start: caretAfter, end: caretAfter }, "merge", `text:${cur.id}`);
    if (caretAfter !== c) focusBlock(cur.id, caretAfter);
  };

  const handleEnter = (b: DocsBlock, shift: boolean) => {
    if (!editable) return;
    const snapNow = now();
    const cur = snapNow.byId.get(b.id) ?? b;
    if (!docsIsTextBlock(cur.type)) {
      const para = newBlock("paragraph", cur.parent_id, positionAfter(snapNow, cur));
      apply([para], { block: para.id, start: 0, end: 0 });
      focusBlock(para.id, 0);
      return;
    }
    const h = handles.current.get(cur.id);
    const text = textOf(cur);
    const plain = docsPlain(text);
    const c = h?.caret() ?? { start: plain.length, end: plain.length };
    if (shift) {
      const next = docsInsertText(docsDeleteRange(text, c.start, c.end), c.start, "\n");
      apply([{ op: "update", id: cur.id, props: { text: next } }], { block: cur.id, start: c.start + 1, end: c.start + 1 }, "merge", `text:${cur.id}`);
      focusBlock(cur.id, c.start + 1);
      return;
    }
    if (plain === "" && cur.type !== "paragraph") {
      const ops = cur.parent_id && LISTY.has(cur.type) ? outdentOps(snapNow, cur) : turnInto(snapNow, cur, "paragraph");
      apply(ops ?? turnInto(snapNow, cur, "paragraph"), { block: cur.id, start: 0, end: 0 });
      focusBlock(cur.id, 0);
      return;
    }
    const type: DocsBlockType = LISTY.has(cur.type) ? cur.type : "paragraph";
    if (c.start === 0 && c.end === 0 && plain.length > 0) {
      const before = newBlock(type, cur.parent_id, positionBefore(snapNow, cur));
      apply([before], { block: cur.id, start: 0, end: 0 });
      focusBlock(cur.id, 0);
      return;
    }
    const [left, right] = docsSplit(docsDeleteRange(text, c.start, c.end), c.start);
    if (docsFolds(cur.type, cur.props) && isOpen(cur.id) && c.start >= plain.length) {
      const first = siblingsOf(snapNow, cur.id)[0];
      const child = newBlock("paragraph", cur.id, docsKeyBetween(null, first?.position ?? null));
      apply([child], { block: child.id, start: 0, end: 0 });
      focusBlock(child.id, 0);
      return;
    }
    const nb = newBlock(type, cur.parent_id, positionAfter(snapNow, cur), { text: right, ...(type === "to_do" ? { checked: false } : {}) });
    apply([{ op: "update", id: cur.id, props: { text: left } }, nb], { block: nb.id, start: 0, end: 0 });
    focusBlock(nb.id, 0);
  };

  const addBlockNear = (b: DocsBlock, where: "before" | "after") => {
    if (!editable) return;
    const snapNow = now();
    const cur = snapNow.byId.get(b.id) ?? b;
    // Like Notion's "+": a new block holding "/" with the command menu open.
    const p = newBlock("paragraph", cur.parent_id, where === "before" ? positionBefore(snapNow, cur) : positionAfter(snapNow, cur), { text: [{ text: "/" }] });
    apply([p], { block: p.id, start: 1, end: 1 });
    focusBlock(p.id, 1);
    requestAnimationFrame(() => {
      const el = handles.current.get(p.id)?.el;
      if (el) setSlash({ blockId: p.id, start: 0, query: "", rect: el.getBoundingClientRect(), index: 0 });
    });
  };

  const mergeInto = (target: DocsBlock, source: DocsBlock) => {
    const snapNow = now();
    const at = docsPlain(textOf(target)).length;
    const ops: DocsOp[] = [{ op: "update", id: target.id, props: { text: docsConcat(textOf(target), textOf(source)) } }];
    const kids = siblingsOf(snapNow, source.id);
    if (kids.length) {
      const nestable = docsHoldsChildren(target.type, target.props);
      ops.push(...moveBlocksOps(snapNow, kids.map((k) => k.id), nestable ? target : source, nestable ? "inside" : "after"));
    }
    ops.push({ op: "delete", id: source.id });
    apply(ops, { block: target.id, start: at, end: at });
    focusBlock(target.id, at);
    tidyColumns();
  };

  const backspaceAtStart = (b: DocsBlock): boolean => {
    const snapNow = now();
    if (b.type !== "paragraph" && docsIsTextBlock(b.type)) {
      apply(turnInto(snapNow, b, "paragraph"), { block: b.id, start: 0, end: 0 });
      focusBlock(b.id, 0);
      return true;
    }
    if (b.parent_id) {
      const ops = outdentOps(snapNow, b);
      if (ops) {
        apply(ops, { block: b.id, start: 0, end: 0 });
        focusBlock(b.id, 0);
        return true;
      }
    }
    const list = order();
    const i = list.findIndex((x) => x.id === b.id);
    const prev = i > 0 ? list[i - 1] : undefined;
    if (!prev) return false;
    if (!docsIsTextBlock(prev.type)) {
      if (docsPlain(textOf(b)) === "" && !prev.type.startsWith("column")) {
        apply([{ op: "delete", id: b.id }], { blocks: [prev.id] });
        tidyColumns();
      }
      if (!prev.type.startsWith("column")) selectBlocks([prev.id]);
      return true;
    }
    mergeInto(prev, b);
    return true;
  };

  const neighbour = (b: DocsBlock, dir: -1 | 1, focusable = true): DocsBlock | undefined => {
    const list = order();
    let i = list.findIndex((x) => x.id === b.id) + dir;
    while (i >= 0 && i < list.length) {
      const x = list[i]!;
      if (!focusable || handles.current.has(x.id)) return x;
      i += dir;
    }
    return undefined;
  };

  const moveCaretTo = (target: DocsBlock, x: number | null, edge: "first" | "last") => {
    const h = handles.current.get(target.id);
    if (!h) return;
    if (h.el && x !== null) {
      h.el.focus({ preventScroll: false });
      placeCaretAtX(h.el, x, edge);
    } else h.focus(edge === "first" ? "start" : "end");
  };

  const applyMark = (b: DocsBlock, mark: DocsMark) => {
    const c = handles.current.get(b.id)?.caret();
    if (!c || c.start === c.end) return;
    apply([{ op: "update", id: b.id, props: { text: docsToggleMark(textOf(blockOf(b.id) ?? b), c.start, c.end, mark) } }], { block: b.id, ...c });
    focusBlock(b.id, c.start, c.end);
  };

  const applyColor = (b: DocsBlock, color: DocsColor | null, range?: { start: number; end: number }) => {
    const c = range ?? handles.current.get(b.id)?.caret();
    if (!c || c.start === c.end) return;
    if (color) lastColor.current = color;
    apply([{ op: "update", id: b.id, props: { text: docsSetColor(textOf(blockOf(b.id) ?? b), c.start, c.end, color) } }], { block: b.id, ...c });
    focusBlock(b.id, c.start, c.end);
  };

  const toggleEquation = (b: DocsBlock, range?: { start: number; end: number }) => {
    const c = range ?? handles.current.get(b.id)?.caret();
    if (!c) return;
    if (c.start === c.end) {
      // Nothing selected: an empty equation at the caret, edited at once.
      setEqForm({ blockId: b.id, start: c.start, end: c.end, rect: rectOfCaret(b.id), initial: "", existing: false });
      return;
    }
    apply([{ op: "update", id: b.id, props: { text: docsToggleEquation(textOf(blockOf(b.id) ?? b), c.start, c.end) } }], { block: b.id, start: c.start, end: c.start });
    focusBlock(b.id, c.start);
  };

  const openLink = (b: DocsBlock) => {
    const h = handles.current.get(b.id);
    const c = h?.caret();
    if (!h?.el || !c) return;
    const text = textOf(blockOf(b.id) ?? b);
    const existing = docsLinkAt(text, c.start);
    const range = c.start === c.end && existing ? { start: existing.start, end: existing.end } : c;
    if (range.start === range.end) return;
    setToolbar(null);
    setLinkForm({ blockId: b.id, ...range, rect: caretRect(h.el), initial: existing?.href ?? "", existing: !!existing });
  };

  const duplicate = (ids: string[]) => {
    const snapNow = now();
    const drafts = toDrafts(snapNow, ids);
    const last = snapNow.byId.get(ids[ids.length - 1]!);
    if (!last || !drafts.length) return;
    const { ops, ids: newIds } = draftOps(drafts, last.parent_id, last.position, siblingsOf(snapNow, last.parent_id).find((x) => x.position > last.position)?.position ?? null);
    apply(ops, { blocks: newIds });
    selectBlocks(newIds);
  };

  const removeBlocks = (ids: string[]) => {
    const list = order();
    const first = list.findIndex((x) => ids.includes(x.id));
    const prev = first > 0 ? list.slice(0, first).reverse().find((x) => !ids.includes(x.id) && handles.current.has(x.id)) : undefined;
    apply(
      ids.map((id) => ({ op: "delete" as const, id })),
      prev ? { block: prev.id, start: docsPlain(textOf(prev)).length, end: docsPlain(textOf(prev)).length } : null,
    );
    setSelected(new Set());
    tidyColumns();
    if (prev) focusBlock(prev.id, "end");
  };

  /** Turn blocks into a turn-into target ("toggle_heading_2"…) or a page (⌘⌥9). */
  const turnBlocks = async (blocks: DocsBlock[], targetId: string) => {
    if (targetId === "page") {
      const b = blocks[0];
      if (!b || !docsIsTextBlock(b.type) || connected) return;
      const id = await props.onCreateSubpage();
      if (!id) return;
      const title = docsPlain(textOf(blockOf(b.id) ?? b)).trim();
      if (title) await api.updatePage(id, { title }).catch(() => undefined);
      const fresh = blockOf(b.id);
      if (fresh) apply([{ op: "delete", id: fresh.id }, { ...newBlock("page", fresh.parent_id, fresh.position, { page_id: id }) }]);
      return;
    }
    const t = DOCS_TURN_INTO.find((x) => x.id === targetId);
    if (!t) return;
    const ok = blocks.filter((b) => (docsIsTextBlock(b.type) || b.type === "code" || b.type === "equation") && (!connected || connected.types.includes(t.type)));
    if (!ok.length) return;
    apply(ok.flatMap((b) => turnInto(now(), b, t.type, t.props ?? {})), { blocks: ok.map((b) => b.id) });
  };

  const setBlockColor = (ids: string[], color: DocsColor | null) => {
    if (color) lastColor.current = color;
    apply(ids.map((id) => ({ op: "update" as const, id, props: { color: (color ?? "") as DocsColor } })), { blocks: ids });
  };

  const onTextKeyDown = (b: DocsBlock, e: KeyboardEvent<HTMLDivElement>) => {
    const cur = blockOf(b.id) ?? b;
    const menu = slash?.blockId === cur.id ? "slash" : mention?.blockId === cur.id ? "mention" : emoji?.blockId === cur.id ? "emoji" : pasteMenu?.blockId === cur.id ? "paste" : null;
    if (menu) {
      const count = menu === "slash" ? slashItems.length : menu === "mention" ? mentionChoices.length : menu === "emoji" ? emojiCount(emoji!.query) : pasteMenu!.choices.length;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const d = e.key === "ArrowDown" ? 1 : -1;
        const step = <T extends { index: number }>(s: T | null): T | null => (s ? { ...s, index: (s.index + d + count) % Math.max(1, count) } : s);
        if (menu === "slash") setSlash(step);
        else if (menu === "mention") setMention(step);
        else if (menu === "emoji") setEmoji(step);
        else setPasteMenu(step);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        if (menu === "slash" && slashItems[slash!.index]) void pickSlash(slashItems[slash!.index]!);
        if (menu === "mention" && mentionChoices[mention!.index]) void pickMention(mentionChoices[mention!.index]!);
        if (menu === "emoji") {
          const em = emojiAt(emoji!.query, emoji!.index);
          if (em) pickEmoji(em);
        }
        if (menu === "paste" && pasteMenu!.choices[pasteMenu!.index]) void pickPaste(pasteMenu!.choices[pasteMenu!.index]!);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSlash(null);
        setMention(null);
        setEmoji(null);
        setPasteMenu(null);
        return;
      }
      if (menu === "paste") setPasteMenu(null);
    }
    const mod = isMod(e);
    const el = handles.current.get(cur.id)?.el ?? null;
    if (mod && !e.shiftKey && e.key.toLowerCase() === "z") {
      e.preventDefault();
      undo();
      return;
    }
    if (mod && ((e.shiftKey && e.key.toLowerCase() === "z") || e.key.toLowerCase() === "y")) {
      e.preventDefault();
      redo();
      return;
    }
    if (mod && e.shiftKey && (e.key === "?" || e.key === "/")) {
      e.preventDefault();
      setHelp(true);
      return;
    }
    if (!editable) {
      if (e.key === "Escape") selectBlocks([cur.id]);
      return;
    }
    // ⌘⌥0–9: turn into (Notion's keys). Option changes e.key on macOS, so read the physical digit.
    if (mod && e.altKey && /^Digit\d$/.test(e.code)) {
      e.preventDefault();
      const target = DOCS_TURN_INTO_KEYS[e.code.slice(5)];
      if (target) void turnBlocks([cur], target);
      return;
    }
    if (mod && !e.shiftKey && !e.altKey) {
      const k = e.key.toLowerCase();
      const marks: Record<string, DocsMark> = { b: "bold", i: "italic", u: "underline", e: "code" };
      if (marks[k] && docsIsTextBlock(cur.type)) {
        e.preventDefault();
        applyMark(cur, marks[k]);
        return;
      }
      if (k === "k" && docsIsTextBlock(cur.type)) {
        e.preventDefault();
        openLink(cur);
        return;
      }
      if (k === "d") {
        e.preventDefault();
        duplicate([cur.id]);
        return;
      }
      if (k === "/") {
        e.preventDefault();
        setBlockMenu({ ids: [cur.id], anchor: el ? caretRect(el) : rows.current.get(cur.id)!.getBoundingClientRect(), view: "main" });
        return;
      }
      if (k === "a" && el) {
        const c = handles.current.get(cur.id)?.caret();
        const len = docsPlain(textOf(cur)).length;
        if (c && (len === 0 || (c.start === 0 && c.end === len))) {
          e.preventDefault();
          selectBlocks(order().map((x) => x.id));
        }
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        if (cur.type === "to_do") apply([{ op: "update", id: cur.id, props: { checked: !cur.props.checked } }]);
        if (docsFolds(cur.type, cur.props)) toggleOpen(cur.id);
        return;
      }
    }
    if (mod && e.shiftKey) {
      const k = e.key.toLowerCase();
      if ((k === "s" || k === "x") && docsIsTextBlock(cur.type)) {
        e.preventDefault();
        applyMark(cur, "strike");
        return;
      }
      if (k === "e" && docsIsTextBlock(cur.type)) {
        e.preventDefault();
        toggleEquation(cur);
        return;
      }
      if (k === "h" && docsIsTextBlock(cur.type)) {
        e.preventDefault();
        applyColor(cur, lastColor.current ?? "yellow_background");
        return;
      }
      if (k === "m" && canComment) {
        e.preventDefault();
        const c = handles.current.get(cur.id)?.caret();
        const plain = docsPlain(textOf(cur));
        if (c && c.end > c.start) props.onComment({ blockId: cur.id, start: docsCodePoints(plain, c.start), end: docsCodePoints(plain, c.end), quote: plain.slice(c.start, c.end) });
        else props.onComment({ blockId: cur.id });
        return;
      }
      if ((e.key === "ArrowUp" || e.key === "ArrowDown") && !connected) {
        e.preventDefault();
        const c = handles.current.get(cur.id)?.caret();
        const ops = shiftOps(now(), cur, e.key === "ArrowUp" ? -1 : 1);
        if (ops) {
          apply(ops, c ? { block: cur.id, ...c } : null);
          if (c) focusBlock(cur.id, c.start, c.end);
        }
        return;
      }
    }
    switch (e.key) {
      case "Enter":
        if (!e.shiftKey && docsIsTextBlock(cur.type)) {
          e.preventDefault();
          handleEnter(cur, false);
        } else if (e.shiftKey && docsIsTextBlock(cur.type)) {
          e.preventDefault();
          handleEnter(cur, true);
        }
        return;
      case "Backspace": {
        const c = handles.current.get(cur.id)?.caret();
        if (c && c.start === 0 && c.end === 0 && docsIsTextBlock(cur.type)) {
          if (backspaceAtStart(cur)) e.preventDefault();
        }
        return;
      }
      case "Delete": {
        const c = handles.current.get(cur.id)?.caret();
        const len = docsPlain(textOf(cur)).length;
        if (c && c.start === len && c.end === len && docsIsTextBlock(cur.type)) {
          const next = neighbour(cur, 1, false);
          if (next && docsIsTextBlock(next.type)) {
            e.preventDefault();
            mergeInto(cur, next);
          } else if (next && !next.type.startsWith("column")) {
            e.preventDefault();
            selectBlocks([next.id]);
          }
        }
        return;
      }
      case "ArrowUp":
      case "ArrowDown": {
        if (e.shiftKey || !el) return;
        const up = e.key === "ArrowUp";
        if (up ? !caretOnFirstLine(el) : !caretOnLastLine(el)) return;
        const target = neighbour(cur, up ? -1 : 1);
        if (!target) return;
        e.preventDefault();
        moveCaretTo(target, caretRect(el).left, up ? "last" : "first");
        return;
      }
      case "ArrowLeft":
      case "ArrowRight": {
        if (e.shiftKey) return;
        const c = handles.current.get(cur.id)?.caret();
        const len = docsPlain(textOf(cur)).length;
        if (!c || c.start !== c.end) return;
        if (e.key === "ArrowLeft" && c.start === 0) {
          const t = neighbour(cur, -1);
          if (t) {
            e.preventDefault();
            focusBlock(t.id, "end");
          }
        } else if (e.key === "ArrowRight" && c.end === len) {
          const t = neighbour(cur, 1);
          if (t) {
            e.preventDefault();
            focusBlock(t.id, "start");
          }
        }
        return;
      }
      case "Tab": {
        e.preventDefault();
        if (connected) return; // nesting moves blocks, which the platform can't do from here
        const c = handles.current.get(cur.id)?.caret();
        const ops = e.shiftKey ? outdentOps(now(), cur) : indentOps(now(), cur);
        if (ops) {
          apply(ops, c ? { block: cur.id, ...c } : null);
          if (c) focusBlock(cur.id, c.start, c.end);
        }
        return;
      }
      case "Escape":
        e.preventDefault();
        setToolbar(null);
        selectBlocks([cur.id]);
        return;
    }
  };

  // ---- menus

  const slashItems = useMemo(
    () =>
      slash
        ? filteredSlash(slash.query, { canCreatePages: editable && !connected && !root, recent: recentSlash(), hasBlockActions: true }).filter(
            (i) => !connected || (i.type ? connected.types.includes(i.type) : (i.action === "date" && !!connected.dates) || i.action === "emoji" || i.action === "turn" || i.action === "delete"),
          )
        : [],
    [slash, editable, connected, root],
  );
  const mentionChoices = useMentionChoices(orgId, mention?.query ?? "", mention?.mode ?? "all", pageId);

  const removeTrigger = (blockId: string, start: number, end: number): { block: DocsBlock; text: DocsSpan[] } | null => {
    const b = blockOf(blockId);
    if (!b) return null;
    return { block: b, text: start < 0 ? textOf(b) : docsDeleteRange(textOf(b), start, end) };
  };

  const insertBlockAt = (b: DocsBlock, remaining: DocsSpan[], op: Extract<DocsOp, { op: "create" }> | null, replaceOps?: DocsOp[]): string => {
    const snapNow = now();
    if (docsPlain(remaining) === "" && replaceOps) {
      apply([{ op: "update", id: b.id, props: { text: [] } }, ...replaceOps], { block: b.id, start: 0, end: 0 });
      return b.id;
    }
    const created = op ?? newBlock("paragraph", b.parent_id, positionAfter(snapNow, b));
    apply([{ op: "update", id: b.id, props: { text: remaining } }, { ...created, parent_id: b.parent_id, position: positionAfter(snapNow, b) }], { block: created.id, start: 0, end: 0 });
    return created.id;
  };

  /** Puts a new block where the slash command was typed: replacing the block when it's empty, else after it. */
  const placeBlock = (b: DocsBlock, text: DocsSpan[], created: Extract<DocsOp, { op: "create" }>, extra: DocsOp[] = []) => {
    const fresh = blockOf(b.id) ?? b;
    if (docsIsTextBlock(fresh.type) && docsPlain(text) === "" && siblingsOf(now(), fresh.id).length === 0) {
      apply([{ op: "delete", id: fresh.id }, { ...created, parent_id: fresh.parent_id, position: fresh.position }, ...extra], { blocks: [created.id] });
    } else {
      apply([{ op: "update", id: fresh.id, props: { text } }, { ...created, parent_id: fresh.parent_id, position: positionAfter(now(), fresh) }, ...extra], { blocks: [created.id] });
    }
    return created.id;
  };

  const keepText = (b: DocsBlock, text: DocsSpan[], start: number) => apply([{ op: "update", id: b.id, props: { text } }], { block: b.id, start: Math.max(0, start), end: Math.max(0, start) }, "merge", `text:${b.id}`);

  const pickUpload = (b: DocsBlock, kind: MediaKind) => {
    uploadTarget.current = { blockId: b.id, kind };
    if (fileInput.current) {
      fileInput.current.accept = DOCS_FILE_ACCEPT[kind];
      fileInput.current.click();
    }
  };

  const pickSlash = async (item: DocsSlashItem) => {
    if (!slash) return;
    const { blockId, start, query } = slash;
    setSlash(null);
    rememberSlash(item.id);
    const r = removeTrigger(blockId, start, start + 1 + query.length);
    if (!r) return;
    const { block: b, text } = r;
    const empty = docsPlain(text) === "";
    if (item.type) {
      if (empty && docsIsTextBlock(b.type) && (docsIsTextBlock(item.type) || item.type === "code" || item.type === "equation")) {
        const ops = turnInto(now(), b, item.type, item.props ?? {});
        apply([{ op: "update", id: b.id, props: docsIsTextBlock(item.type) ? { text: [] } : {} }, ...ops], { block: b.id, start: 0, end: 0 });
        focusBlock(b.id, "start");
      } else {
        const created = newBlock(item.type, b.parent_id, "a0", item.props ?? {});
        if (docsIsTextBlock(item.type) || item.type === "code" || item.type === "equation") focusBlock(insertBlockAt(b, text, created), "start");
        else placeBlock(b, text, created);
      }
      return;
    }
    switch (item.action) {
      case "image":
      case "video":
      case "audio":
      case "pdf":
      case "file":
        keepText(b, text, start);
        pickUpload(b, item.action);
        return;
      case "bookmark":
      case "embed": {
        keepText(b, text, start);
        const el = handles.current.get(b.id)?.el;
        setUrlForm({ blockId: b.id, rect: el ? el.getBoundingClientRect() : new DOMRect(), mode: item.action });
        return;
      }
      case "page":
      case "database": {
        keepText(b, text, start);
        if (item.arg === "linked") {
          setMention({ blockId: b.id, start: Math.max(0, start), query: "", rect: rectOfCaret(b.id), index: 0, mode: "pages", trigger: 0, purpose: "linked_db" });
          return;
        }
        const id = await (item.action === "page" ? props.onCreateSubpage() : props.onCreateDatabase(typeof item.arg === "string" ? item.arg : undefined));
        if (!id) return;
        const created = newBlock(item.action === "page" ? "page" : "database", b.parent_id, "a0", item.action === "page" ? { page_id: id } : { database_id: id });
        placeBlock(b, textOf(blockOf(b.id) ?? b), created);
        if (item.action === "page") props.onOpenPage(id);
        return;
      }
      case "link_to_page":
        keepText(b, text, start);
        setMention({ blockId: b.id, start: Math.max(0, start), query: "", rect: rectOfCaret(b.id), index: 0, mode: "pages", trigger: 0, purpose: "link_block" });
        return;
      case "mention-page":
      case "mention-person":
      case "date": {
        keepText(b, text, start);
        focusBlock(b.id, Math.max(0, start));
        setMention({ blockId: b.id, start: Math.max(0, start), query: "", rect: rectOfCaret(b.id), index: 0, trigger: 0, mode: item.action === "mention-page" ? "pages" : item.action === "date" ? "dates" : "people", purpose: "mention" });
        return;
      }
      case "reminder":
        keepText(b, text, start);
        setDateForm({ blockId: b.id, start: Math.max(0, start), rect: rectOfCaret(b.id), remind: true });
        return;
      case "emoji":
        keepText(b, docsInsertText(text, Math.max(0, start), ":"), start + 1);
        focusBlock(b.id, Math.max(0, start) + 1);
        requestAnimationFrame(() => setEmoji({ blockId: b.id, start: Math.max(0, start), query: "", rect: rectOfCaret(b.id), index: 0 }));
        return;
      case "inline-equation":
        keepText(b, text, start);
        setEqForm({ blockId: b.id, start: Math.max(0, start), end: Math.max(0, start), rect: rectOfCaret(b.id), initial: "", existing: false });
        return;
      case "columns": {
        const n = typeof item.arg === "number" ? item.arg : 2;
        const fresh = blockOf(b.id) ?? b;
        const replace = docsIsTextBlock(fresh.type) && empty && siblingsOf(now(), fresh.id).length === 0;
        if (!replace) keepText(fresh, text, start);
        const { ops, focus } = columnsOps(now(), fresh, n, replace);
        apply(ops, { block: focus, start: 0, end: 0 });
        focusBlock(focus, 0);
        return;
      }
      case "synced": {
        const original = newBlock("synced_block", b.parent_id, "a0", {});
        const inner = newBlock("paragraph", original.id, "a0", { text: empty ? [] : text });
        const fresh = blockOf(b.id) ?? b;
        apply([{ op: "delete", id: fresh.id }, { ...original, parent_id: fresh.parent_id, position: fresh.position }, inner], { block: inner.id, start: 0, end: 0 });
        focusBlock(inner.id, "end");
        return;
      }
      case "button": {
        const created = newBlock("button", b.parent_id, "a0", { label: "Button", actions: [] });
        placeBlock(b, text, created);
        requestAnimationFrame(() => {
          const btn = rows.current.get(created.id)?.querySelector<HTMLButtonElement>("button:nth-of-type(2)");
          btn?.click();
        });
        return;
      }
      case "turn":
        keepText(b, text, start);
        await turnBlocks([blockOf(b.id) ?? b], String(item.arg));
        return;
      case "color":
        keepText(b, text, start);
        setBlockColor([b.id], (item.arg as DocsColor | null) ?? null);
        return;
      case "duplicate":
        keepText(b, text, start);
        duplicate([b.id]);
        return;
      case "delete":
        removeBlocks([b.id]);
        return;
      case "move":
        keepText(b, text, start);
        props.onMoveToPage([b.id]);
        return;
      case "comment":
        keepText(b, text, start);
        if (canComment) props.onComment({ blockId: b.id });
        return;
    }
  };

  const pickMention = async (choice: MentionChoice) => {
    if (!mention) return;
    const { blockId, start, query, trigger, purpose } = mention;
    setMention(null);
    const b = blockOf(blockId);
    if (!b) return;
    const removed = docsDeleteRange(textOf(b), start, start + trigger + query.length + (mention.trigger === 2 && docsPlain(textOf(b)).slice(start + trigger + query.length, start + trigger + query.length + 2) === "]]" ? 2 : 0));
    if (choice.custom) {
      apply([{ op: "update", id: b.id, props: { text: removed } }], { block: b.id, start, end: start }, "merge", `text:${b.id}`);
      setDateForm({ blockId: b.id, start, rect: rectOfCaret(b.id), remind: false });
      return;
    }
    let pageId = choice.mention.type === "page" ? choice.mention.id : "";
    if (choice.create) {
      const id = await props.onCreateSubpage();
      if (!id) return;
      await api.updatePage(id, { title: choice.create }).catch(() => undefined);
      pageId = id;
    }
    if (purpose === "link_block" || purpose === "linked_db") {
      if (!pageId) return;
      const created = newBlock(purpose === "linked_db" ? "database" : "link_to_page", b.parent_id, "a0", purpose === "linked_db" ? { database_id: pageId } : { page_id: pageId });
      placeBlock(b, removed, created);
      return;
    }
    const mentionOut: DocsMention = choice.create ? { type: "page", id: pageId } : choice.mention;
    const chip: DocsSpan[] = [{ text: choice.text, mention: mentionOut }, { text: " " }];
    const next = docsInsertSpans(removed, start, chip);
    const at = start + choice.text.length + 1;
    apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
    focusBlock(b.id, at);
  };

  const pickEmoji = (em: string) => {
    if (!emoji) return;
    const { blockId, start, query } = emoji;
    setEmoji(null);
    const b = blockOf(blockId);
    if (!b) return;
    const next = docsInsertText(docsDeleteRange(textOf(b), start, start + 1 + query.length), start, em, {});
    const at = start + em.length;
    apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
    focusBlock(b.id, at);
  };

  const insertDate = (m: Extract<DocsMention, { type: "date" }>) => {
    if (!dateForm) return;
    const { blockId, start } = dateForm;
    setDateForm(null);
    const b = blockOf(blockId);
    if (!b) return;
    const label = m.date;
    const chip: DocsSpan[] = [{ text: label, mention: m }, { text: " " }];
    const next = docsInsertSpans(textOf(b), start, chip);
    const at = start + label.length + 1;
    apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
    focusBlock(b.id, at);
  };

  const saveEquation = (tex: string | null) => {
    if (!eqForm) return;
    const { blockId, start, end, existing } = eqForm;
    setEqForm(null);
    const b = blockOf(blockId);
    if (!b) return;
    let next = existing ? docsDeleteRange(textOf(b), start, end) : textOf(b);
    if (tex) next = docsInsertSpans(next, start, [{ text: tex, equation: true }]);
    const at = start + (tex?.length ?? 0);
    apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
    focusBlock(b.id, at);
  };

  // ---- files

  const placeFile = (f: DocsFile, target: { blockId: string; kind: MediaKind } | null, after: DocsBlock | null) => {
    const kind = target && target.kind !== "file" ? (docsFileBlockType(f.content_type, f.name) === target.kind ? target.kind : docsFileBlockType(f.content_type, f.name)) : docsFileBlockType(f.content_type, f.name);
    const b = target ? blockOf(target.blockId) : after;
    const created = newBlock(kind, b?.parent_id ?? root, "a0", { file_id: f.id, caption: [] });
    if (b && target && docsIsTextBlock(b.type) && docsPlain(textOf(b)) === "" && siblingsOf(now(), b.id).length === 0) apply([{ op: "delete", id: b.id }, { ...created, position: b.position }], { blocks: [created.id] });
    else if (b) apply([{ ...created, position: positionAfter(now(), b) }], { blocks: [created.id] });
    else apply([{ ...created, position: positionAtEnd(now(), root) }], { blocks: [created.id] });
    return created.id;
  };

  const uploadOne = async (file: File, target: { blockId: string; kind: MediaKind } | null, after: DocsBlock | null): Promise<string | null> => {
    const t = toast.loading(`Uploading ${file.name}…`);
    try {
      const f = await props.onUpload(file);
      toast.dismiss(t);
      return placeFile(f, target, after);
    } catch (e) {
      toast.dismiss(t);
      toast.error((e as { message?: string }).message ?? "Couldn't upload that file. Try again.");
      return null;
    }
  };

  const onFile = async (file: File) => {
    const target = uploadTarget.current;
    uploadTarget.current = null;
    if (!target) return;
    await uploadOne(file, target, null);
  };

  const uploadFiles = async (files: File[], after: DocsBlock | null) => {
    if (!editable || connected) return;
    let prev = after;
    for (const file of files.slice(0, 20)) {
      const id = await uploadOne(file, null, prev);
      prev = id ? (blockOf(id) ?? prev) : prev;
    }
  };

  // ---- paste and copy

  const insertDrafts = (b: DocsBlock | null, drafts: DocsDraftBlock[], caret: { start: number; end: number } | null) => {
    const snapNow = now();
    if (!drafts.length) return;
    if (!b) {
      const { ops, ids } = draftOps(drafts, root, siblingsOf(snapNow, root).at(-1)?.position ?? null, null);
      apply(ops, { blocks: ids });
      return;
    }
    const textual = docsIsTextBlock(b.type);
    const text = textOf(b);
    const c = caret ?? { start: docsPlain(text).length, end: docsPlain(text).length };
    if (textual && drafts.length === 1 && drafts[0]!.type === "paragraph") {
      const ins = drafts[0]!.props.text ?? [];
      const next = docsInsertSpans(docsDeleteRange(text, c.start, c.end), c.start, ins);
      const at = c.start + docsPlain(ins).length;
      apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
      focusBlock(b.id, at);
      return;
    }
    const [left, right] = textual ? docsSplit(docsDeleteRange(text, c.start, c.end), c.start) : [text, []];
    const ops: DocsOp[] = [];
    let rest = [...drafts];
    if (textual) {
      const first = rest[0]!;
      if (docsPlain(left) === "" && docsPlain(right) === "" && docsIsTextBlock(first.type)) {
        ops.push(...turnInto(snapNow, b, first.type, first.props), { op: "update", id: b.id, props: first.props });
        rest = rest.slice(1);
      } else if (first.type === "paragraph") {
        ops.push({ op: "update", id: b.id, props: { text: docsConcat(left, first.props.text ?? []) } });
        rest = rest.slice(1);
      } else if (docsPlain(left) === "" && docsPlain(right) === "" && siblingsOf(snapNow, b.id).length === 0) {
        ops.push({ op: "delete", id: b.id }); // an empty block gives way to what was pasted
      } else ops.push({ op: "update", id: b.id, props: { text: left } });
    }
    if (docsPlain(right)) {
      const last = rest[rest.length - 1];
      if (last && docsIsTextBlock(last.type)) rest[rest.length - 1] = { ...last, props: { ...last.props, text: docsConcat(last.props.text ?? [], right) } };
      else rest.push({ type: "paragraph", props: { text: right }, children: [] });
    }
    const next = siblingsOf(snapNow, b.parent_id).find((x) => x.position > b.position);
    const created = draftOps(rest, b.parent_id, b.position, next?.position ?? null);
    ops.push(...created.ops);
    const lastId = created.ids[created.ids.length - 1] ?? b.id;
    apply(ops, { block: lastId, start: 0, end: 0 });
    focusBlock(lastId, "end");
  };

  const insertAfter = (b: DocsBlock, drafts: DocsDraftBlock[]) => {
    const snapNow = now();
    const cur = snapNow.byId.get(b.id) ?? b;
    const next = siblingsOf(snapNow, cur.parent_id).find((x) => x.position > cur.position);
    const { ops, ids } = draftOps(drafts, cur.parent_id, cur.position, next?.position ?? null);
    apply(ops, { blocks: ids });
  };

  const pickPaste = async (choice: PasteChoice) => {
    const pm = pasteMenu;
    setPasteMenu(null);
    if (!pm || choice.id === "url") return;
    const b = blockOf(pm.blockId);
    if (!b) return;
    const removed = docsDeleteRange(textOf(b), pm.start, pm.end);
    if (choice.id === "mention") {
      const id = /\/docs\/([0-9a-f-]{36})/.exec(pm.url)?.[1];
      if (!id) return;
      const chip: DocsSpan[] = [{ text: refs.pages[id]?.title ?? "Page", mention: { type: "page", id } }];
      apply([{ op: "update", id: b.id, props: { text: docsInsertSpans(removed, pm.start, chip) } }]);
      return;
    }
    if (choice.id === "embed") {
      placeBlock(b, removed, newBlock("embed", b.parent_id, "a0", { url: pm.url, caption: [] }));
      return;
    }
    await addBookmark(b, removed, pm.url);
  };

  const addBookmark = async (b: DocsBlock, text: DocsSpan[], url: string) => {
    let p: { title?: string; description?: string; site_name?: string; preview_id?: string } = {};
    try {
      const prev = await fetchPreview.mutateAsync(url);
      p = { title: prev.title, description: prev.description, site_name: prev.site_name, preview_id: prev.id };
    } catch {
      // The bookmark still works without a preview.
    }
    const fresh = blockOf(b.id) ?? b;
    placeBlock(fresh, text, newBlock("bookmark", fresh.parent_id, "a0", { url, caption: [], ...Object.fromEntries(Object.entries(p).filter(([, v]) => v)) }));
  };

  const onPaste = (b: DocsBlock, e: ClipboardEvent<HTMLElement>) => {
    const cd = e.clipboardData;
    const cur = blockOf(b.id) ?? b;
    const c = handles.current.get(cur.id)?.caret() ?? null;
    if (cd.files.length && !connected) {
      e.preventDefault();
      void uploadFiles(Array.from(cd.files), cur);
      return;
    }
    let drafts: DocsDraftBlock[] | null = null;
    const json = cd.getData(MIME);
    if (json) {
      try {
        drafts = JSON.parse(json) as DocsDraftBlock[];
      } catch {
        drafts = null;
      }
    }
    const plain = cd.getData("text/plain");
    const url = plain.trim();
    const synced = SYNCED_PASTE_RE.exec(url);
    if (!drafts && synced && !connected && !root) {
      e.preventDefault();
      placeBlock(cur, textOf(cur), newBlock("synced_block", cur.parent_id, "a0", { source_id: synced[1]! }));
      return;
    }
    if (!drafts && c && c.end > c.start && /^https?:\/\/\S+$/.test(url) && docsSafeLink(url)) {
      e.preventDefault();
      apply([{ op: "update", id: cur.id, props: { text: docsSetLink(textOf(cur), c.start, c.end, docsSafeLink(url)) } }], { block: cur.id, ...c });
      focusBlock(cur.id, c.start, c.end);
      return;
    }
    if (!drafts && c && c.start === c.end && /^https?:\/\/\S+$/.test(url) && docsSafeLink(url) && docsIsTextBlock(cur.type)) {
      // A link: paste it as a link, then offer a mention, bookmark or embed.
      e.preventDefault();
      const href = docsSafeLink(url)!;
      const next = docsInsertSpans(textOf(cur), c.start, [{ text: url, link: href }]);
      const end = c.start + url.length;
      apply([{ op: "update", id: cur.id, props: { text: next } }], { block: cur.id, start: end, end });
      focusBlock(cur.id, end);
      if (connected) return;
      const choices: PasteChoice[] = [{ id: "url", label: "Link", icon: PASTE_ICONS.url }];
      const own = new RegExp(`^${window.location.origin.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/docs/[0-9a-f-]{36}`).test(href);
      if (own) choices.push({ id: "mention", label: "Mention", icon: PASTE_ICONS.mention });
      else {
        choices.push({ id: "bookmark", label: "Bookmark", icon: PASTE_ICONS.bookmark });
        const em = docsEmbedFor(href);
        if (em) choices.push({ id: "embed", label: `Embed ${em.name}`, icon: PASTE_ICONS.embed });
      }
      requestAnimationFrame(() => setPasteMenu({ blockId: cur.id, start: c.start, end, url: href, rect: rectOfCaret(cur.id), index: 0, choices }));
      return;
    }
    const html = cd.getData("text/html");
    if (!drafts && html) drafts = htmlToBlocks(html);
    if (!drafts && plain) drafts = docsTextToBlocks(plain);
    if (!drafts) return;
    e.preventDefault();
    insertDrafts(cur, drafts, c);
  };

  const copySelection = (e: ClipboardEvent<HTMLElement>, cut: boolean) => {
    if (!selected.size) return;
    e.preventDefault();
    const snapNow = now();
    const ids = snapNow.blocks.filter((b) => selected.has(b.id)).map((b) => b.id);
    const drafts = toDrafts(snapNow, ids);
    const blocks = snapNow.blocks.filter((b) => ids.includes(b.id) || ids.some((id) => isDescendant(snapNow, b, id)));
    const names = { user: (id: string) => refs.users[id]?.name, page: (id: string) => refs.pages[id]?.title ?? undefined };
    e.clipboardData.setData("text/plain", docsBlocksToMarkdown(blocks, names) || docsBlocksToText(blocks));
    e.clipboardData.setData(MIME, JSON.stringify(drafts));
    if (cut && editable) removeBlocks(ids);
  };

  // ---- block selection keyboard

  const onContainerKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target === container.current && (e.key === "?" || (isMod(e) && e.shiftKey && e.key === "/"))) {
      e.preventDefault();
      setHelp(true);
      return;
    }
    if (!selected.size || e.target !== container.current) return;
    const list = order();
    const ids = list.filter((b) => selected.has(b.id)).map((b) => b.id);
    const firstIdx = list.findIndex((b) => b.id === ids[0]);
    const lastIdx = list.findIndex((b) => b.id === ids[ids.length - 1]);
    const mod = isMod(e);
    if (mod && e.key.toLowerCase() === "z") {
      e.preventDefault();
      if (e.shiftKey) redo();
      else undo();
      return;
    }
    if (mod && e.key.toLowerCase() === "a") {
      e.preventDefault();
      selectBlocks(list.map((b) => b.id));
      return;
    }
    if (mod && e.altKey && /^Digit\d$/.test(e.code) && editable) {
      e.preventDefault();
      const target = DOCS_TURN_INTO_KEYS[e.code.slice(5)];
      const blocks = ids.map((id) => blockOf(id)).filter((b): b is DocsBlock => !!b);
      if (target) void turnBlocks(blocks, target);
      return;
    }
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        setSelected(new Set());
        return;
      case "ArrowUp":
      case "ArrowDown": {
        e.preventDefault();
        const up = e.key === "ArrowUp";
        if (mod && e.shiftKey && editable) {
          const snapNow = now();
          const edge = snapNow.byId.get(up ? ids[0]! : ids[ids.length - 1]!)!;
          const sib = up ? siblingsOf(snapNow, edge.parent_id).filter((x) => x.position < edge.position && !selected.has(x.id)).at(-1) : siblingsOf(snapNow, edge.parent_id).find((x) => x.position > edge.position && !selected.has(x.id));
          if (sib) apply(moveBlocksOps(snapNow, ids, sib, up ? "before" : "after"), { blocks: ids });
          return;
        }
        if (e.shiftKey) {
          const anchorIdx = list.findIndex((b) => b.id === anchor.current);
          const focusIdx = anchorIdx === firstIdx ? lastIdx : firstIdx;
          const nextFocus = Math.max(0, Math.min(list.length - 1, focusIdx + (up ? -1 : 1)));
          const [a, b] = [Math.min(anchorIdx, nextFocus), Math.max(anchorIdx, nextFocus)];
          selectBlocks(list.slice(a, b + 1).map((x) => x.id), true);
        } else {
          const i = up ? Math.max(0, firstIdx - 1) : Math.min(list.length - 1, lastIdx + 1);
          selectBlocks([list[i]!.id]);
        }
        rows.current.get(up ? list[Math.max(0, firstIdx - 1)]!.id : list[Math.min(list.length - 1, lastIdx + 1)]!.id)?.scrollIntoView({ block: "nearest" });
        return;
      }
      case "Enter": {
        e.preventDefault();
        const first = list[firstIdx];
        if (first && handles.current.has(first.id)) focusBlock(first.id, "end");
        else if (first) setBlockMenu({ ids, anchor: rows.current.get(first.id)!.getBoundingClientRect(), view: "main" });
        return;
      }
      case "Backspace":
      case "Delete":
        if (!editable) return;
        e.preventDefault();
        removeBlocks(ids);
        return;
    }
    if (mod && e.key.toLowerCase() === "d" && editable) {
      e.preventDefault();
      duplicate(ids);
      return;
    }
    if (mod && e.key === "/") {
      e.preventDefault();
      setBlockMenu({ ids, anchor: rows.current.get(ids[0]!)!.getBoundingClientRect(), view: "main" });
    }
  };

  // ---- mouse

  const onRowMouseDown = (b: DocsBlock, e: MouseEvent) => {
    const t = e.target as HTMLElement;
    if (e.shiftKey && (focused.current || anchor.current)) {
      e.preventDefault();
      const list = order();
      const from = list.findIndex((x) => x.id === (anchor.current ?? focused.current));
      const to = list.findIndex((x) => x.id === b.id);
      if (from >= 0 && to >= 0) {
        anchor.current = list[from]!.id;
        selectBlocks(list.slice(Math.min(from, to), Math.max(from, to) + 1).map((x) => x.id), true);
      }
      return;
    }
    if (t.closest("[contenteditable='true'], textarea, input, button, a, iframe, video, audio, [role=checkbox], [role=slider], [role=separator], [data-floating]")) {
      if (selected.size) setSelected(new Set());
      return;
    }
    // Blocks inside a column list, synced block or database handle their own clicks.
    if (t.closest("[data-database]") || (b.type === "column_list" || b.type === "synced_block" ? t.closest("[data-block-id]") !== rows.current.get(b.id) : false)) return;
    e.preventDefault();
    e.stopPropagation();
    selectBlocks([b.id]);
  };

  // ---- drag and drop

  const dragState = useRef<{ ids: string[]; x0: number; y0: number; active: boolean; blockId: string; handle: HTMLElement } | null>(null);

  const dropTargetAt = (ids: string[], x: number, y: number): EditorContextValue["dropTarget"] => {
    const snapNow = now();
    const dragging = new Set(ids);
    let best: EditorContextValue["dropTarget"] = null;
    let bestArea = Number.POSITIVE_INFINITY;
    for (const blk of visibleOrder(snapNow, isOpen, root)) {
      if (dragging.has(blk.id) || ids.some((id) => isDescendant(snapNow, blk, id)) || blk.type === "column") continue;
      const el = rows.current.get(blk.id);
      if (!el) continue;
      const r = el.firstElementChild?.getBoundingClientRect() ?? el.getBoundingClientRect();
      if (y < r.top - 4 || y > r.bottom + 4 || x < r.left - 56 || x > r.right + 16) continue;
      const area = r.width * r.height;
      // The innermost block under the pointer wins (blocks in columns sit inside the list's box).
      if (area > bestArea) continue;
      bestArea = area;
      const nestable = docsHoldsChildren(blk.type, blk.props) && !docsIsStructural(blk.type);
      const sideOk = !ids.some((id) => snapNow.byId.get(id)?.type.startsWith("column")) && blk.type !== "column_list";
      let place: DropPlace;
      if (sideOk && x < r.left + 24 && x >= r.left - 56) place = "left";
      else if (sideOk && x > r.right - 32) place = "right";
      else place = y < r.top + r.height / 2 ? "before" : nestable && x > r.left + 48 ? "inside" : "after";
      best = { id: blk.id, place };
    }
    return best;
  };

  const startDrag = (b: DocsBlock, e: PointerEvent<HTMLElement>) => {
    if (e.button !== 0) return;
    if (connected) {
      // The platform can't move blocks: the handle only opens the block's actions.
      e.preventDefault();
      setBlockMenu({ ids: [b.id], anchor: e.currentTarget, view: "main" });
      return;
    }
    e.preventDefault();
    const ids = selected.has(b.id) ? now().blocks.filter((x) => selected.has(x.id)).map((x) => x.id) : [b.id];
    dragState.current = { ids, x0: e.clientX, y0: e.clientY, active: false, blockId: b.id, handle: e.currentTarget };
    const onMove = (ev: globalThis.PointerEvent) => {
      const s = dragState.current;
      if (!s) return;
      if (!s.active && Math.hypot(ev.clientX - s.x0, ev.clientY - s.y0) < 4) return;
      s.active = true;
      const first = now().byId.get(s.ids[0]!);
      const label = s.ids.length > 1 ? `${s.ids.length} blocks` : first ? docsPlain(first.props.text).slice(0, 40) || DOCS_BLOCK_LABEL[first.type] : "";
      setDrag({ ids: new Set(s.ids), target: dropTargetAt(s.ids, ev.clientX, ev.clientY), x: ev.clientX, y: ev.clientY, label });
      const edge = 64;
      if (ev.clientY < edge) window.scrollBy(0, -12);
      else if (ev.clientY > window.innerHeight - edge) window.scrollBy(0, 12);
    };
    const onUp = (ev: globalThis.PointerEvent) => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      const s = dragState.current;
      dragState.current = null;
      if (!s) return;
      if (!s.active) {
        selectBlocks(s.ids);
        setBlockMenu({ ids: s.ids, anchor: s.handle, view: "main" });
        return;
      }
      const t = dropTargetAt(s.ids, ev.clientX, ev.clientY);
      setDrag(null);
      if (!t) return;
      const snapNow = now();
      const target = snapNow.byId.get(t.id) ?? null;
      if (!target) return;
      const ops = t.place === "left" || t.place === "right" ? columnDropOps(snapNow, s.ids, target, t.place) : moveBlocksOps(snapNow, s.ids, target, t.place);
      if (t.place === "inside") toggleOpen(t.id, true);
      apply(ops, { blocks: s.ids });
      tidyColumns();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  // Files dragged in from the desktop: drop line under the block they'd follow.
  const fileDropTarget = (y: number): DocsBlock | null => {
    let best: DocsBlock | null = null;
    for (const blk of visibleOrder(now(), isOpen, root)) {
      if (blk.type === "column") continue;
      const el = rows.current.get(blk.id);
      if (!el) continue;
      const r = el.getBoundingClientRect();
      if (r.top <= y) best = blk;
    }
    return best;
  };
  const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer.types).includes("Files");
  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!editable || connected || !hasFiles(e)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
    setFileDrop(fileDropTarget(e.clientY)?.id ?? "");
  };
  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    if (!editable || connected || !hasFiles(e)) return;
    e.preventDefault();
    const after = fileDropTarget(e.clientY);
    setFileDrop(null);
    void uploadFiles(Array.from(e.dataTransfer.files), after);
  };

  useEffect(() => {
    const el = container.current;
    if (!el) return undefined;
    const onMenu = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      if (!blockOf(id)) return;
      e.stopPropagation();
      const row = rows.current.get(id);
      selectBlocks([id]);
      setBlockMenu({ ids: [id], anchor: (row?.querySelector(`[data-block-handle="${id}"]`) as HTMLElement | null) ?? row ?? el, view: "main" });
    };
    el.addEventListener("docs:block-menu", onMenu);
    return () => el.removeEventListener("docs:block-menu", onMenu);
  });

  // Close the toolbar when the selection leaves the editor.
  useEffect(() => {
    const onSel = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) setToolbar(null);
    };
    document.addEventListener("selectionchange", onSel);
    return () => document.removeEventListener("selectionchange", onSel);
  }, []);

  // Scroll to #block-<id> once (and briefly mark it).
  useEffect(() => {
    if (root) return;
    const m = /^#block-([0-9a-f-]{36})$/.exec(window.location.hash);
    if (!m) return;
    const id = m[1]!;
    requestAnimationFrame(() => {
      const row = rows.current.get(id);
      if (row) {
        row.scrollIntoView({ block: "center" });
        selectBlocks([id]);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- context

  const viewersByBlock = useMemo(() => {
    const m = new Map<string, DocsViewer[]>();
    for (const v of viewers) if (v.block_id) m.set(v.block_id, [...(m.get(v.block_id) ?? []), v]);
    return m;
  }, [viewers]);

  const decorations = useMemo(() => {
    const m = new Map<string, Decoration[]>();
    for (const t of threads) {
      if (!t.block_id || !t.quote || t.resolved_at) continue;
      const b = snap.byId.get(t.block_id);
      if (!b) continue;
      const d = decorate(docsPlain(b.props.text), t, t.id === activeThread);
      if (d) m.set(b.id, [...(m.get(b.id) ?? []), d].sort((a, c) => a.start - c.start).filter((x, i, arr) => i === 0 || x.start >= arr[i - 1]!.end));
    }
    return m;
  }, [threads, snap, activeThread]);

  const nested: NestedEditorProps = { canComment, onOpenPage: props.onOpenPage, onMoveToPage: props.onMoveToPage };

  const ctx: EditorContextValue = {
    session,
    snap,
    pageId,
    orgId,
    root,
    editable,
    isReadOnly: (id) => !!connected?.readOnly.has(id),
    canMove: !connected,
    canComment,
    refs,
    viewersByBlock,
    commentCounts,
    decorations,
    selected,
    dropTarget: drag?.target ?? null,
    dragging: drag?.ids ?? new Set(),
    isOpen,
    toggleOpen,
    register: (id, h) => {
      if (h) handles.current.set(id, h);
      else handles.current.delete(id);
    },
    registerRow: (id, el) => {
      if (el) rows.current.set(id, el);
      else rows.current.delete(id);
    },
    apply,
    onTextChange,
    onTextKeyDown,
    onEnter: handleEnter,
    onPaste,
    onFocusBlock: (b) => {
      focused.current = b.id;
      anchor.current = b.id;
      if (selected.size) setSelected(new Set());
      props.onFocusBlock(b.id);
    },
    onSelectionChange: (b, c) => {
      const el = handles.current.get(b.id)?.el;
      if (c && el && c.end > c.start && (editable || canComment) && docsIsTextBlock(b.type)) setToolbar({ blockId: b.id, start: c.start, end: c.end, rect: caretRect(el) });
      else if (toolbar?.blockId === b.id) setToolbar(null);
    },
    onRowMouseDown,
    startDrag,
    openComments: props.onOpenComments,
    openPage: props.onOpenPage,
    focusBlock,
    addBlockNear,
    editInlineEquation: (b, start, end, rect) => {
      if (!editable) return;
      const tex = docsPlain(docsSlice(textOf(blockOf(b.id) ?? b), start, end));
      setEqForm({ blockId: b.id, start, end, rect, initial: tex, existing: true });
    },
    insertAfter,
    pickUpload,
    uploadFiles: (files, after) => void uploadFiles(files, after),
    nested,
  };

  const toolbarBlock = toolbar ? snap.byId.get(toolbar.blockId) : undefined;
  const toolbarText = toolbarBlock?.props.text ?? [];
  const menuBlocks = blockMenu ? blockMenu.ids.map((id) => snap.byId.get(id)).filter((b): b is DocsBlock => !!b) : [];
  const single = menuBlocks.length === 1 ? menuBlocks[0]! : null;
  const closeMenu = () => setBlockMenu(null);

  const menuItems: (FloatingMenuItem | "separator")[] = [];
  if (blockMenu?.view === "turn") {
    menuItems.push({ id: "back", label: "Back", icon: <ArrowLeft />, onSelect: () => setBlockMenu((m) => (m ? { ...m, view: "main" } : m)) }, "separator");
    for (const t of DOCS_TURN_INTO) {
      if (connected && !connected.types.includes(t.type)) continue;
      const current = single && docsTurnIntoId(single.type, single.props) === t.id;
      menuItems.push({ id: `turn-${t.id}`, label: t.label, icon: slashIcon(t.id), hint: current ? "Current" : "", onSelect: () => void turnBlocks(menuBlocks, t.id) });
    }
    if (!connected && !root && single && docsIsTextBlock(single.type)) menuItems.push({ id: "turn-page", label: "Page", icon: slashIcon("page"), hint: "⌘⌥9", onSelect: () => void turnBlocks([single], "page") });
  } else if (blockMenu?.view === "color") {
    // Rendered with ColorPalette below.
  } else if (blockMenu && editable) {
    const textual = menuBlocks.every((b) => docsIsTextBlock(b.type) || b.type === "code" || b.type === "equation");
    const anyReadOnly = menuBlocks.some((b) => connected?.readOnly.has(b.id));
    if (textual && !anyReadOnly) menuItems.push({ id: "turn", label: "Turn into", icon: <Repeat2 />, hint: "›", onSelect: () => setBlockMenu((m) => (m ? { ...m, view: "turn" } : m)) });
    if (!connected && !anyReadOnly && menuBlocks.every((b) => !docsIsStructural(b.type) || b.type === "synced_block"))
      menuItems.push({ id: "color", label: "Colour", icon: <Palette />, hint: "›", onSelect: () => setBlockMenu((m) => (m ? { ...m, view: "color" } : m)) });
    if (single?.type === "table") {
      menuItems.push({ id: "hr", label: single.props.header_row ? "Remove header row" : "Use a header row", icon: <Type />, onSelect: () => apply([{ op: "update", id: single.id, props: { header_row: !single.props.header_row } }]) });
      menuItems.push({ id: "hc", label: single.props.header_col ? "Remove header column" : "Use a header column", icon: <Type />, onSelect: () => apply([{ op: "update", id: single.id, props: { header_col: !single.props.header_col } }]) });
    }
    if (single && (single.type === "image" || single.type === "video" || single.type === "audio" || single.type === "pdf" || single.type === "file") && !single.props.url) {
      menuItems.push({ id: "replace", label: "Replace file", icon: <RefreshCw />, onSelect: () => pickUpload(single, single.type as MediaKind) });
    }
    if (single?.type === "synced_block" && !single.props.source_id) menuItems.push({ id: "sync-copy", label: "Copy and sync", icon: <Copy />, onSelect: () => copyAndSync(single.id) });
    if (single?.type === "synced_block" && single.props.source_id) {
      menuItems.push({
        id: "unsync",
        label: "Unsync",
        icon: <Unlink />,
        onSelect: () => rows.current.get(single.id)?.dispatchEvent(new CustomEvent("docs:unsync")),
      });
    }
    if (!connected && !root && menuBlocks.every((b) => !docsIsStructural(b.type) && !b.parent_id)) {
      menuItems.push({
        id: "wrap-synced",
        label: "Turn into synced block",
        icon: <RefreshCw />,
        onSelect: () => {
          const snapNow = now();
          const firstB = menuBlocks[0]!;
          const original = newBlock("synced_block", firstB.parent_id, positionBefore(snapNow, firstB), {});
          apply([original, ...moveBlocksOps(snapNow, menuBlocks.map((b) => b.id), null, "after").map((op) => (op.op === "move" ? { ...op, parent_id: original.id } : op))], { blocks: [original.id] });
        },
      });
    }
    if (menuItems.length) menuItems.push("separator");
  }
  if (blockMenu?.view === "main" && editable && !connected) {
    menuItems.push({ id: "dup", label: "Duplicate", icon: <Copy />, hint: "⌘D", onSelect: () => duplicate(blockMenu.ids) });
    if (single && !single.type.startsWith("column")) {
      menuItems.push({ id: "up", label: "Move up", icon: <ArrowUp />, hint: "⌘⇧↑", onSelect: () => apply(shiftOps(now(), single, -1) ?? [], { blocks: [single.id] }) });
      menuItems.push({ id: "down", label: "Move down", icon: <ArrowDown />, hint: "⌘⇧↓", onSelect: () => apply(shiftOps(now(), single, 1) ?? [], { blocks: [single.id] }) });
    }
    if (!root) menuItems.push({ id: "move", label: "Move to page…", icon: <CornerDownRight />, onSelect: () => props.onMoveToPage(blockMenu.ids) });
  }
  if (blockMenu?.view === "main" && single && connected?.readOnly.has(single.id)) {
    menuItems.push({ id: "platform", label: `Edit in ${connected.platform}`, icon: <Link2 />, onSelect: () => void window.open(connected.url, "_blank", "noopener,noreferrer") });
  }
  if (blockMenu?.view === "main" && single && !connected) {
    menuItems.push({
      id: "link",
      label: "Copy link to block",
      icon: <Link2 />,
      onSelect: () => {
        void navigator.clipboard?.writeText(`${window.location.origin}/docs/${pageId}#block-${single.id}`).then(
          () => toast.success("Link copied."),
          () => toast.error("Couldn't copy the link."),
        );
      },
    });
    if (canComment && !root) menuItems.push({ id: "comment", label: "Comment", icon: <MessageSquarePlus />, hint: "⌘⇧M", onSelect: () => props.onComment({ blockId: single.id }) });
  }
  if (blockMenu?.view === "main") menuItems.push({ id: "help", label: "Keyboard shortcuts", icon: <Keyboard />, hint: "⌘?", onSelect: () => setHelp(true) });
  if (blockMenu?.view === "main" && editable && !menuBlocks.some((b) => connected?.readOnly.has(b.id))) {
    menuItems.push("separator", { id: "delete", label: menuBlocks.length > 1 ? `Delete ${menuBlocks.length} blocks` : "Delete", icon: <Trash2 />, destructive: true, hint: "Del", onSelect: () => removeBlocks(blockMenu.ids) });
  }
  const menuColor = single?.props.color ?? null;
  // Submenu entries switch the menu's view; every other entry closes it.
  const menuList = menuItems.map((it) => (it === "separator" || ["turn", "color", "back"].includes(it.id) ? it : { ...it, onSelect: () => (closeMenu(), it.onSelect()) }));

  return (
    <EditorContext.Provider value={ctx}>
      <div
        ref={container}
        tabIndex={-1}
        aria-label={root ? "Synced content" : "Page content"}
        className={cn("relative flex flex-col outline-none", fileDrop !== null && "rounded-md ring-2 ring-primary/40")}
        onKeyDown={onContainerKeyDown}
        onCopy={(e) => copySelection(e, false)}
        onCut={(e) => copySelection(e, true)}
        onDragOver={onDragOver}
        onDragLeave={(e) => {
          if (!container.current?.contains(e.relatedTarget as Node)) setFileDrop(null);
        }}
        onDrop={onDrop}
        onPaste={(e) => {
          if (!selected.size || !editable) return;
          const last = now().blocks.filter((b) => selected.has(b.id)).at(-1) ?? null;
          if (e.clipboardData.files.length && !connected) {
            e.preventDefault();
            void uploadFiles(Array.from(e.clipboardData.files), last);
            return;
          }
          const json = e.clipboardData.getData(MIME);
          let drafts: DocsDraftBlock[] | null = null;
          try {
            drafts = json ? (JSON.parse(json) as DocsDraftBlock[]) : null;
          } catch {
            drafts = null;
          }
          drafts = drafts ?? htmlToBlocks(e.clipboardData.getData("text/html")) ?? docsTextToBlocks(e.clipboardData.getData("text/plain"));
          if (!drafts?.length) return;
          e.preventDefault();
          const snapNow = now();
          const parent = last?.parent_id ?? root;
          const next = last ? siblingsOf(snapNow, parent).find((x) => x.position > last.position) : undefined;
          const { ops, ids } = draftOps(drafts, parent, last?.position ?? null, next?.position ?? null);
          apply(ops, { blocks: ids });
          selectBlocks(ids);
        }}
        onBlur={(e) => {
          if (!container.current?.contains(e.relatedTarget as Node)) {
            focused.current = null;
            props.onFocusBlock(null);
          }
        }}
      >
        <BlockChildren parentId={root} depth={0} />
        {editable ? (
          <button
            type="button"
            aria-label="Add a block at the end"
            className={cn("flex w-full cursor-text items-start text-left type-body text-fg-subtle", root ? "min-h-6" : "min-h-24")}
            tabIndex={snap.blocks.length ? -1 : 0}
            onClick={() => {
              const snapNow = now();
              const last = siblingsOf(snapNow, root).at(-1);
              if (last && docsIsTextBlock(last.type) && docsPlain(last.props.text) === "" && siblingsOf(snapNow, last.id).length === 0) {
                focusBlock(last.id, "end");
                return;
              }
              const para = newBlock("paragraph", root, positionAtEnd(snapNow, root));
              apply([para], { block: para.id, start: 0, end: 0 });
              focusBlock(para.id, 0);
            }}
          >
            {snap.blocks.length || root ? null : <span className="py-1">Start writing, or press / for commands</span>}
          </button>
        ) : null}
        {fileDrop !== null ? (
          <div aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 flex justify-center pb-2">
            <span className="rounded-pill bg-primary px-3 py-1 type-label text-primary-fg shadow-sm">Drop to add files</span>
          </div>
        ) : null}
      </div>
      <input
        ref={fileInput}
        type="file"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (f) void onFile(f);
        }}
      />
      {slash ? <SlashMenu state={slash} items={slashItems} onPick={(it) => void pickSlash(it)} onClose={() => setSlash(null)} onHover={(i) => setSlash((s) => (s ? { ...s, index: i } : s))} /> : null}
      {mention ? <MentionMenu state={mention} choices={mentionChoices} onPick={(c) => void pickMention(c)} onClose={() => setMention(null)} onHover={(i) => setMention((s) => (s ? { ...s, index: i } : s))} /> : null}
      {emoji ? <EmojiMenu state={emoji} onPick={pickEmoji} onClose={() => setEmoji(null)} onHover={(i) => setEmoji((s) => (s ? { ...s, index: i } : s))} /> : null}
      {pasteMenu ? <PasteMenu anchor={pasteMenu.rect} choices={pasteMenu.choices} index={pasteMenu.index} onPick={(c) => void pickPaste(c)} onClose={() => setPasteMenu(null)} onHover={(i) => setPasteMenu((s) => (s ? { ...s, index: i } : s))} /> : null}
      {toolbar && toolbarBlock && !linkForm && !eqForm ? (
        <InlineToolbar
          state={toolbar}
          editable={editable}
          canComment={canComment && !root}
          active={{
            bold: docsHasMark(toolbarText, toolbar.start, toolbar.end, "bold"),
            italic: docsHasMark(toolbarText, toolbar.start, toolbar.end, "italic"),
            underline: docsHasMark(toolbarText, toolbar.start, toolbar.end, "underline"),
            strike: docsHasMark(toolbarText, toolbar.start, toolbar.end, "strike"),
            code: docsHasMark(toolbarText, toolbar.start, toolbar.end, "code"),
            link: docsHasMark(toolbarText, toolbar.start, toolbar.end, "link"),
            equation: docsHasMark(toolbarText, toolbar.start, toolbar.end, "equation"),
          }}
          color={docsColorAt(toolbarText, toolbar.start, toolbar.end)}
          lastColor={lastColor.current}
          onMark={(m) => applyMark(toolbarBlock, m)}
          onLink={() => openLink(toolbarBlock)}
          onEquation={() => {
            toggleEquation(toolbarBlock, toolbar);
            setToolbar(null);
          }}
          onColor={(c) => applyColor(toolbarBlock, c, toolbar)}
          onComment={() => {
            const plain = docsPlain(toolbarText);
            props.onComment({ blockId: toolbarBlock.id, start: docsCodePoints(plain, toolbar.start), end: docsCodePoints(plain, toolbar.end), quote: plain.slice(toolbar.start, toolbar.end) });
            setToolbar(null);
          }}
          onClose={() => setToolbar(null)}
        />
      ) : null}
      {linkForm ? (
        <LinkForm
          anchor={linkForm.rect}
          initial={linkForm.initial}
          onClose={() => setLinkForm(null)}
          onApply={(href) => {
            const b = blockOf(linkForm.blockId);
            setLinkForm(null);
            if (!b) return;
            apply([{ op: "update", id: b.id, props: { text: docsSetLink(textOf(b), linkForm.start, linkForm.end, href) } }], { block: b.id, start: linkForm.end, end: linkForm.end });
            focusBlock(b.id, linkForm.end);
          }}
          onRemove={
            linkForm.existing
              ? () => {
                  const b = blockOf(linkForm.blockId);
                  setLinkForm(null);
                  if (b) apply([{ op: "update", id: b.id, props: { text: docsSetLink(textOf(b), linkForm.start, linkForm.end, null) } }]);
                }
              : undefined
          }
        />
      ) : null}
      {urlForm ? (
        <UrlForm
          anchor={urlForm.rect}
          mode={urlForm.mode}
          busy={fetchPreview.isPending}
          onClose={() => setUrlForm(null)}
          onSave={(url, as) => {
            const b = blockOf(urlForm.blockId);
            setUrlForm(null);
            if (!b) return;
            if (as === "embed") placeBlock(b, textOf(b), newBlock("embed", b.parent_id, "a0", { url, caption: [] }));
            else void addBookmark(b, textOf(b), url);
          }}
        />
      ) : null}
      {dateForm ? <DateForm anchor={dateForm.rect} remind={dateForm.remind} onClose={() => setDateForm(null)} onSave={insertDate} /> : null}
      {eqForm ? <EquationForm anchor={eqForm.rect} initial={eqForm.initial} onClose={() => setEqForm(null)} onSave={(t) => saveEquation(t)} {...(eqForm.existing ? { onRemove: () => saveEquation(null) } : {})} /> : null}
      {blockMenu && (menuItems.length || blockMenu.view === "color") ? (
        <Floating open anchor={blockMenu.anchor} onClose={closeMenu} autoFocus label="Block actions" placement="bottom-start">
          <div className="flex max-h-[min(70dvh,32rem)] flex-col overflow-y-auto">
            {blockMenu.view === "color" ? (
              <>
                <button type="button" className="flex min-h-control items-center gap-3 rounded-sm px-3 type-body text-fg hover:bg-sunken focus-visible:bg-sunken focus-visible:outline-none [&_svg]:size-4" onClick={() => setBlockMenu((m) => (m ? { ...m, view: "main" } : m))}>
                  <ArrowLeft aria-hidden />
                  Back
                </button>
                <ColorPalette
                  current={menuColor}
                  last={lastColor.current}
                  onPick={(c) => {
                    closeMenu();
                    setBlockColor(blockMenu.ids, c);
                  }}
                />
              </>
            ) : (
              <FloatingMenu items={menuList} onDone={() => {}} />
            )}
          </div>
        </Floating>
      ) : null}
      <ShortcutsSheet open={help} onOpenChange={setHelp} />
      {drag ? (
        <div aria-hidden style={{ position: "fixed", left: drag.x + 12, top: drag.y + 8 }} className={cn("pointer-events-none z-(--app-z-popover) max-w-60 truncate rounded-md border border-border bg-surface-raised px-3 py-1 type-label text-fg shadow-md")}>
          {drag.label}
        </div>
      ) : null}
    </EditorContext.Provider>
  );
}

function isDescendant(snap: DocsSessionSnapshot, b: DocsBlock, ancestorId: string): boolean {
  let p = b.parent_id;
  let depth = 0;
  while (p && depth++ < 64) {
    if (p === ancestorId) return true;
    p = snap.byId.get(p)?.parent_id ?? null;
  }
  return false;
}

