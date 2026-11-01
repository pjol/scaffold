"use client";

import {
  DOCS_BLOCK_LABEL,
  docsBlocksToMarkdown,
  docsBlocksToText,
  docsCodePoints,
  docsConcat,
  docsDeleteRange,
  docsHasMark,
  docsInsertSpans,
  docsInsertText,
  docsIsTextBlock,
  docsKeyBetween,
  docsLinkAt,
  docsPlain,
  docsSafeLink,
  docsSetLink,
  docsShortcut,
  docsSlice,
  docsSplit,
  docsTextToBlocks,
  docsToggleMark,
  type DocsBlock,
  type DocsBlockType,
  type DocsComment,
  type DocsDraftBlock,
  type DocsFile,
  type DocsMark,
  type DocsOp,
  type DocsPageSession,
  type DocsRefs,
  type DocsSessionSnapshot,
  type DocsSlashItem,
  type DocsSpan,
  type DocsViewer,
} from "@scaffoldapp/shared";
import { cn, toast } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, Copy, CornerDownRight, Link2, MessageSquarePlus, Trash2, Type } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent, type MouseEvent, type PointerEvent } from "react";
import { Floating, FloatingMenu, type FloatingMenuItem } from "../Floating";
import { BlockChildren } from "./BlockTree";
import { EditorContext, type EditorContextValue, type EditorSelection } from "./context";
import { caretOnFirstLine, caretOnLastLine, caretRect, placeCaretAtX, type Decoration } from "./dom";
import { BookmarkForm, filteredSlash, InlineToolbar, LinkForm, MentionMenu, SlashMenu, slashIcon, useMentionChoices, type CaretMenuState, type MentionChoice, type ToolbarState } from "./menus";
import {
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

const MIME = "application/x-docs-blocks";
const LISTY = new Set<DocsBlockType>(["bulleted_list", "numbered_list", "to_do", "toggle"]);
const TURN_INTO: DocsBlockType[] = ["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout", "code"];

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
  onCreateDatabase: () => Promise<string | null>;
  onUpload: (file: File) => Promise<DocsFile>;
  onMoveToPage: (blockIds: string[]) => void;
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

/** Applies **x**, *x*, _x_, ~~x~~ and `x` when the closing delimiter was just typed. */
function inlineShortcut(spans: DocsSpan[], caret: number): { spans: DocsSpan[]; caret: number; mark: DocsMark } | null {
  const before = docsPlain(spans).slice(0, caret);
  const rules: [RegExp, number, DocsMark][] = [
    [/(?:^|[^*])\*\*([^*\n]+)\*\*$/, 2, "bold"],
    [/(?:^|[^_])__([^_\n]+)__$/, 2, "bold"],
    [/(?:^|[^~])~~([^~\n]+)~~$/, 2, "strike"],
    [/(?:^|[^`])`([^`\n]+)`$/, 1, "code"],
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
    if (docsSlice(spans, openStart, caret).some((s) => s.mention || s.code)) continue;
    let next = docsDeleteRange(spans, closeStart, caret);
    next = docsDeleteRange(next, openStart, innerStart);
    next = docsToggleMark(next, openStart, openStart + inner.length, mark);
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

/**
 * The block editor: one contenteditable per text block over the span model, keyboard
 * editing (Enter splits, Backspace merges, arrows cross blocks, Tab nests), "/" and "@"
 * menus, Markdown shortcuts, a formatting toolbar, block selection (Escape, shift-click,
 * select all) with delete, move, turn into, copy and paste, drag handles, undo and redo.
 */
export function Editor(props: EditorProps) {
  const { pageId, orgId, session, snap, refs, editable, canComment, viewers, commentCounts, threads, activeThread } = props;
  const handles = useRef(new Map<string, RichTextHandle>());
  const rows = useRef(new Map<string, HTMLElement>());
  const container = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const pendingFocus = useRef<{ id: string; at: number | "start" | "end"; end?: number } | null>(null);
  const focused = useRef<string | null>(null);
  const anchor = useRef<string | null>(null);
  const uploadTarget = useRef<{ blockId: string; kind: "image" | "file" } | null>(null);
  // After **x** turns bold, the next typed text isn't: the mark stops at the caret.
  const sticky = useRef<{ blockId: string; at: number; mark: DocsMark } | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [slash, setSlash] = useState<CaretMenuState | null>(null);
  const [mention, setMention] = useState<(CaretMenuState & { mode: "all" | "people" | "pages" | "dates"; trigger: number }) | null>(null);
  const [toolbar, setToolbar] = useState<ToolbarState | null>(null);
  const [linkForm, setLinkForm] = useState<{ blockId: string; start: number; end: number; rect: DOMRect; initial: string; existing: boolean } | null>(null);
  const [bookmarkForm, setBookmarkForm] = useState<{ blockId: string; rect: DOMRect } | null>(null);
  const [blockMenu, setBlockMenu] = useState<{ ids: string[]; anchor: HTMLElement | DOMRect } | null>(null);
  const [drag, setDrag] = useState<{ ids: Set<string>; target: EditorContextValue["dropTarget"]; x: number; y: number; label: string } | null>(null);
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
    // Reveal it inside closed toggles.
    let p = blockOf(id)?.parent_id;
    while (p) {
      const pb = blockOf(p);
      if (pb?.type === "toggle") toggleOpen(pb.id, true);
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
    if (ids.length) {
      (document.activeElement as HTMLElement | null)?.blur?.();
      container.current?.focus({ preventScroll: true });
    }
  };

  const order = () => visibleOrder(now(), isOpen);

  // ---- text editing

  const closeMenusFor = (id: string) => {
    if (slash?.blockId === id) setSlash(null);
    if (mention?.blockId === id) setMention(null);
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

    if (cur.type === "paragraph" && inserted === " ") {
      const prefix = plain.slice(0, c - 1);
      const sc = /^\S{1,4}$/.test(prefix) ? docsShortcut(prefix) : null;
      if (sc && sc.type !== "divider" && sc.type !== "code") {
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
    if (inserted && "*_~`".includes(inserted)) {
      const r = inlineShortcut(spans, c);
      if (r) {
        spans = r.spans;
        caretAfter = r.caret;
        sticky.current = { blockId: cur.id, at: r.caret, mark: r.mark };
      }
    }
    // "/" and "@" menus.
    const rectNow = () => {
      const el = handles.current.get(cur.id)?.el;
      return el ? caretRect(el) : new DOMRect();
    };
    if (inserted === "/" && (c === 1 || /\s/.test(plain[c - 2] ?? " "))) {
      setSlash({ blockId: cur.id, start: c - 1, query: "", rect: rectNow(), index: 0 });
    } else if (slash && slash.blockId === cur.id) {
      const q = plain.slice(slash.start + 1, c);
      if (c <= slash.start || plain[slash.start] !== "/" || q.length > 24 || /\n|\s\s/.test(q)) setSlash(null);
      else setSlash({ ...slash, query: q, index: 0 });
    }
    if (inserted === "@" && (c === 1 || /\s/.test(plain[c - 2] ?? " "))) {
      setMention({ blockId: cur.id, start: c - 1, query: "", rect: rectNow(), index: 0, mode: "all", trigger: 1 });
    } else if (mention && mention.blockId === cur.id) {
      const q = plain.slice(mention.start + mention.trigger, c);
      if (c < mention.start + mention.trigger || (mention.trigger && plain[mention.start] !== "@") || q.length > 40 || /\n/.test(q)) setMention(null);
      else setMention({ ...mention, query: q, index: 0 });
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
    if (cur.type === "toggle" && isOpen(cur.id) && c.start >= plain.length) {
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

  const mergeInto = (target: DocsBlock, source: DocsBlock) => {
    const snapNow = now();
    const at = docsPlain(textOf(target)).length;
    const ops: DocsOp[] = [{ op: "update", id: target.id, props: { text: docsConcat(textOf(target), textOf(source)) } }];
    const kids = siblingsOf(snapNow, source.id);
    if (kids.length) {
      const nestable = ["paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"].includes(target.type);
      ops.push(...moveBlocksOps(snapNow, kids.map((k) => k.id), nestable ? target : source, nestable ? "inside" : "after"));
    }
    ops.push({ op: "delete", id: source.id });
    apply(ops, { block: target.id, start: at, end: at });
    focusBlock(target.id, at);
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
      if (docsPlain(textOf(b)) === "") {
        apply([{ op: "delete", id: b.id }], { blocks: [prev.id] });
      }
      selectBlocks([prev.id]);
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
    if (prev) focusBlock(prev.id, "end");
  };

  const onTextKeyDown = (b: DocsBlock, e: KeyboardEvent<HTMLDivElement>) => {
    const cur = blockOf(b.id) ?? b;
    const menu = slash?.blockId === cur.id ? "slash" : mention?.blockId === cur.id ? "mention" : null;
    if (menu) {
      const count = menu === "slash" ? slashItems.length : mentionChoices.length;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const d = e.key === "ArrowDown" ? 1 : -1;
        if (menu === "slash") setSlash((s) => (s ? { ...s, index: (s.index + d + count) % Math.max(1, count) } : s));
        else setMention((s) => (s ? { ...s, index: (s.index + d + count) % Math.max(1, count) } : s));
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        if (menu === "slash" && slashItems[slash!.index]) void pickSlash(slashItems[slash!.index]!);
        if (menu === "mention" && mentionChoices[mention!.index]) pickMention(mentionChoices[mention!.index]!);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSlash(null);
        setMention(null);
        return;
      }
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
    if (!editable) {
      if (e.key === "Escape") selectBlocks([cur.id]);
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
        setBlockMenu({ ids: [cur.id], anchor: el ? caretRect(el) : rows.current.get(cur.id)!.getBoundingClientRect() });
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
        if (cur.type === "toggle") toggleOpen(cur.id);
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
      if (e.key === "ArrowUp" || e.key === "ArrowDown") {
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
          } else if (next) {
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

  const slashItems = useMemo(() => (slash ? filteredSlash(slash.query, { canCreatePages: editable }) : []), [slash, editable]);
  const mentionChoices = useMentionChoices(orgId, mention?.query ?? "", mention?.mode ?? "all", pageId);

  const removeTrigger = (blockId: string, start: number, end: number): { block: DocsBlock; text: DocsSpan[] } | null => {
    const b = blockOf(blockId);
    if (!b) return null;
    return { block: b, text: docsDeleteRange(textOf(b), start, end) };
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

  const pickSlash = async (item: DocsSlashItem) => {
    if (!slash) return;
    const { blockId, start, query } = slash;
    setSlash(null);
    const r = removeTrigger(blockId, start, start + 1 + query.length);
    if (!r) return;
    const { block: b, text } = r;
    const empty = docsPlain(text) === "";
    if (item.type) {
      if (empty && docsIsTextBlock(b.type)) {
        const ops = turnInto(now(), b, item.type, item.props ?? {});
        apply([{ op: "update", id: b.id, props: docsIsTextBlock(item.type) ? { text: [] } : {} }, ...ops], { block: b.id, start: 0, end: 0 });
        focusBlock(b.id, "start");
      } else {
        const id = insertBlockAt(b, text, newBlock(item.type, b.parent_id, "a0", item.props ?? {}));
        focusBlock(id, "start");
      }
      return;
    }
    switch (item.action) {
      case "image":
      case "file":
        apply([{ op: "update", id: b.id, props: { text } }], { block: b.id, start, end: start }, "merge", `text:${b.id}`);
        uploadTarget.current = { blockId: b.id, kind: item.action };
        if (fileInput.current) {
          fileInput.current.accept = item.action === "image" ? "image/png,image/jpeg,image/webp,image/gif,image/heic" : "";
          fileInput.current.click();
        }
        return;
      case "bookmark": {
        apply([{ op: "update", id: b.id, props: { text } }], { block: b.id, start, end: start }, "merge", `text:${b.id}`);
        const el = handles.current.get(b.id)?.el;
        setBookmarkForm({ blockId: b.id, rect: el ? el.getBoundingClientRect() : new DOMRect() });
        return;
      }
      case "page":
      case "database": {
        apply([{ op: "update", id: b.id, props: { text } }], { block: b.id, start, end: start }, "merge", `text:${b.id}`);
        const id = await (item.action === "page" ? props.onCreateSubpage() : props.onCreateDatabase());
        if (!id) return;
        const fresh = blockOf(b.id) ?? b;
        const created = newBlock(item.action === "page" ? "page" : "database", fresh.parent_id, "a0", item.action === "page" ? { page_id: id } : { database_id: id });
        const replace = docsPlain(textOf(fresh)) === "";
        if (replace) apply([{ op: "delete", id: fresh.id }, { ...created, position: fresh.position }], { blocks: [created.id] });
        else insertBlockAt(fresh, textOf(fresh), created);
        if (item.action === "page") props.onOpenPage(id);
        return;
      }
      case "mention-page":
      case "mention-person":
      case "date": {
        apply([{ op: "update", id: b.id, props: { text } }], { block: b.id, start, end: start }, "merge", `text:${b.id}`);
        focusBlock(b.id, start);
        const el = handles.current.get(b.id)?.el;
        setMention({ blockId: b.id, start, query: "", rect: el ? caretRect(el) : new DOMRect(), index: 0, trigger: 0, mode: item.action === "mention-page" ? "pages" : item.action === "date" ? "dates" : "people" });
        return;
      }
    }
  };

  const pickMention = (choice: MentionChoice) => {
    if (!mention) return;
    const { blockId, start, query, trigger } = mention;
    setMention(null);
    const b = blockOf(blockId);
    if (!b) return;
    const removed = docsDeleteRange(textOf(b), start, start + trigger + query.length);
    const chip: DocsSpan[] = [{ text: choice.text, mention: choice.mention }, { text: " " }];
    const next = docsInsertSpans(removed, start, chip);
    const at = start + choice.text.length + 1;
    apply([{ op: "update", id: b.id, props: { text: next } }], { block: b.id, start: at, end: at });
    focusBlock(b.id, at);
  };

  const onFile = async (file: File) => {
    const target = uploadTarget.current;
    uploadTarget.current = null;
    if (!target) return;
    const t = toast.loading(`Uploading ${file.name}…`);
    try {
      const f = await props.onUpload(file);
      toast.dismiss(t);
      const b = blockOf(target.blockId);
      const kind = target.kind === "image" && f.content_type.startsWith("image/") ? "image" : "file";
      const created = newBlock(kind, b?.parent_id ?? null, "a0", { file_id: f.id, caption: [] });
      if (b && docsIsTextBlock(b.type) && docsPlain(textOf(b)) === "") apply([{ op: "delete", id: b.id }, { ...created, position: b.position }], { blocks: [created.id] });
      else if (b) apply([{ ...created, position: positionAfter(now(), b) }], { blocks: [created.id] });
      else apply([{ ...created, position: positionAtEnd(now(), null) }], { blocks: [created.id] });
    } catch (e) {
      toast.dismiss(t);
      toast.error((e as { message?: string }).message ?? "Couldn't upload that file. Try again.");
    }
  };

  // ---- paste and copy

  const insertDrafts = (b: DocsBlock | null, drafts: DocsDraftBlock[], caret: { start: number; end: number } | null) => {
    const snapNow = now();
    if (!drafts.length) return;
    if (!b) {
      const { ops, ids } = draftOps(drafts, null, siblingsOf(snapNow, null).at(-1)?.position ?? null, null);
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
      if (docsPlain(left) === "" && docsPlain(right) === "") {
        ops.push(...turnInto(snapNow, b, first.type, first.props), { op: "update", id: b.id, props: first.props });
        rest = rest.slice(1);
      } else if (first.type === "paragraph") {
        ops.push({ op: "update", id: b.id, props: { text: docsConcat(left, first.props.text ?? []) } });
        rest = rest.slice(1);
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

  const onPaste = (b: DocsBlock, e: ClipboardEvent<HTMLElement>) => {
    const cd = e.clipboardData;
    const cur = blockOf(b.id) ?? b;
    const c = handles.current.get(cur.id)?.caret() ?? null;
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
    if (!drafts && c && c.end > c.start && /^https?:\/\/\S+$/.test(url) && docsSafeLink(url)) {
      e.preventDefault();
      apply([{ op: "update", id: cur.id, props: { text: docsSetLink(textOf(cur), c.start, c.end, docsSafeLink(url)) } }], { block: cur.id, ...c });
      focusBlock(cur.id, c.start, c.end);
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
        else if (first) setBlockMenu({ ids, anchor: rows.current.get(first.id)!.getBoundingClientRect() });
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
      setBlockMenu({ ids, anchor: rows.current.get(ids[0]!)!.getBoundingClientRect() });
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
    if (t.closest("[contenteditable='true'], textarea, input, button, a, [role=checkbox], [data-floating]")) {
      if (selected.size) setSelected(new Set());
      return;
    }
    if (!t.closest("[data-database]")) {
      e.preventDefault();
      selectBlocks([b.id]);
    }
  };

  // ---- drag and drop

  const dragState = useRef<{ ids: string[]; x0: number; y0: number; active: boolean; blockId: string; handle: HTMLElement } | null>(null);

  const startDrag = (b: DocsBlock, e: PointerEvent<HTMLElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const ids = selected.has(b.id) ? now().blocks.filter((x) => selected.has(x.id)).map((x) => x.id) : [b.id];
    dragState.current = { ids, x0: e.clientX, y0: e.clientY, active: false, blockId: b.id, handle: e.currentTarget };
    const target = (x: number, y: number): EditorContextValue["dropTarget"] => {
      const snapNow = now();
      const dragging = new Set(ids);
      let best: EditorContextValue["dropTarget"] = null;
      for (const blk of visibleOrder(snapNow, isOpen)) {
        if (dragging.has(blk.id) || ids.some((id) => isDescendant(snapNow, blk, id))) continue;
        const el = rows.current.get(blk.id);
        if (!el) continue;
        const r = el.firstElementChild?.getBoundingClientRect() ?? el.getBoundingClientRect();
        if (y < r.top - 4 || y > r.bottom + 4) continue;
        const nestable = ["paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"].includes(blk.type);
        const place = y < r.top + r.height / 2 ? "before" : nestable && x > r.left + 48 ? "inside" : "after";
        best = { id: blk.id, place };
      }
      return best;
    };
    const onMove = (ev: globalThis.PointerEvent) => {
      const s = dragState.current;
      if (!s) return;
      if (!s.active && Math.hypot(ev.clientX - s.x0, ev.clientY - s.y0) < 4) return;
      s.active = true;
      const first = now().byId.get(s.ids[0]!);
      const label = s.ids.length > 1 ? `${s.ids.length} blocks` : first ? docsPlain(first.props.text).slice(0, 40) || DOCS_BLOCK_LABEL[first.type] : "";
      setDrag({ ids: new Set(s.ids), target: target(ev.clientX, ev.clientY), x: ev.clientX, y: ev.clientY, label });
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
        setBlockMenu({ ids: s.ids, anchor: s.handle });
        return;
      }
      const t = target(ev.clientX, ev.clientY);
      setDrag(null);
      if (!t) return;
      const snapNow = now();
      const ops = moveBlocksOps(snapNow, s.ids, snapNow.byId.get(t.id) ?? null, t.place);
      if (t.place === "inside") toggleOpen(t.id, true);
      apply(ops, { blocks: s.ids });
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  useEffect(() => {
    const el = container.current;
    if (!el) return undefined;
    const onMenu = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      const row = rows.current.get(id);
      selectBlocks([id]);
      setBlockMenu({ ids: [id], anchor: (row?.querySelector(`[data-block-handle="${id}"]`) as HTMLElement | null) ?? row ?? el });
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

  // Scroll to #block-<id> once.
  useEffect(() => {
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

  const ctx: EditorContextValue = {
    session,
    snap,
    pageId,
    orgId,
    editable,
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
  };

  const toolbarBlock = toolbar ? snap.byId.get(toolbar.blockId) : undefined;
  const toolbarText = toolbarBlock?.props.text ?? [];
  const menuBlocks = blockMenu ? blockMenu.ids.map((id) => snap.byId.get(id)).filter((b): b is DocsBlock => !!b) : [];
  const single = menuBlocks.length === 1 ? menuBlocks[0]! : null;

  const menuItems: (FloatingMenuItem | "separator")[] = [];
  if (blockMenu && editable) {
    for (const t of TURN_INTO) {
      if (single && single.type === t) continue;
      if (!menuBlocks.every((b) => docsIsTextBlock(b.type) || b.type === "code")) break;
      menuItems.push({ id: `turn-${t}`, label: `Turn into ${DOCS_BLOCK_LABEL[t].toLowerCase()}`, icon: slashIcon(t === "code" ? "code" : t), onSelect: () => apply(menuBlocks.flatMap((b) => turnInto(now(), b, t)), { blocks: blockMenu.ids }) });
    }
    if (single?.type === "image") {
      for (const w of [25, 50, 75, 100]) menuItems.push({ id: `w-${w}`, label: `Width ${w}%`, icon: <Type />, hint: single.props.width === w ? "Current" : "", onSelect: () => apply([{ op: "update", id: single.id, props: { width: w } }]) });
    }
    if (single?.type === "table") {
      menuItems.push({ id: "hr", label: single.props.header_row ? "Remove header row" : "Use a header row", icon: <Type />, onSelect: () => apply([{ op: "update", id: single.id, props: { header_row: !single.props.header_row } }]) });
      menuItems.push({ id: "hc", label: single.props.header_col ? "Remove header column" : "Use a header column", icon: <Type />, onSelect: () => apply([{ op: "update", id: single.id, props: { header_col: !single.props.header_col } }]) });
      const rowsN = single.props.rows?.length ?? 0;
      const colsN = Math.max(0, ...(single.props.rows ?? []).map((r) => r.length));
      if (rowsN > 1) menuItems.push({ id: "drop-row", label: "Delete last row", icon: <Trash2 />, onSelect: () => apply([{ op: "update", id: single.id, props: { rows: single.props.rows!.slice(0, -1) } }]) });
      if (colsN > 1) menuItems.push({ id: "drop-col", label: "Delete last column", icon: <Trash2 />, onSelect: () => apply([{ op: "update", id: single.id, props: { rows: single.props.rows!.map((r) => r.slice(0, colsN - 1)) } }]) });
    }
    if (menuItems.length) menuItems.push("separator");
    menuItems.push({ id: "dup", label: "Duplicate", icon: <Copy />, hint: "⌘D", onSelect: () => duplicate(blockMenu.ids) });
    if (single) {
      menuItems.push({ id: "up", label: "Move up", icon: <ArrowUp />, onSelect: () => apply(shiftOps(now(), single, -1) ?? [], { blocks: [single.id] }) });
      menuItems.push({ id: "down", label: "Move down", icon: <ArrowDown />, onSelect: () => apply(shiftOps(now(), single, 1) ?? [], { blocks: [single.id] }) });
    }
    menuItems.push({ id: "move", label: "Move to page…", icon: <CornerDownRight />, onSelect: () => props.onMoveToPage(blockMenu.ids) });
  }
  if (blockMenu && single) {
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
    if (canComment) menuItems.push({ id: "comment", label: "Comment", icon: <MessageSquarePlus />, onSelect: () => props.onComment({ blockId: single.id }) });
  }
  if (blockMenu && editable) {
    menuItems.push("separator", { id: "delete", label: menuBlocks.length > 1 ? `Delete ${menuBlocks.length} blocks` : "Delete", icon: <Trash2 />, destructive: true, hint: "Del", onSelect: () => removeBlocks(blockMenu.ids) });
  }

  return (
    <EditorContext.Provider value={ctx}>
      <div
        ref={container}
        tabIndex={-1}
        aria-label="Page content"
        className="relative flex flex-col outline-none"
        onKeyDown={onContainerKeyDown}
        onCopy={(e) => copySelection(e, false)}
        onCut={(e) => copySelection(e, true)}
        onPaste={(e) => {
          if (!selected.size || !editable) return;
          const last = now().blocks.filter((b) => selected.has(b.id)).at(-1) ?? null;
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
          const parent = last?.parent_id ?? null;
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
        <BlockChildren parentId={null} depth={0} />
        {editable ? (
          <button
            type="button"
            aria-label="Add a block at the end"
            className="flex min-h-24 w-full cursor-text items-start text-left type-body text-fg-subtle"
            tabIndex={snap.blocks.length ? -1 : 0}
            onClick={() => {
              const snapNow = now();
              const last = siblingsOf(snapNow, null).at(-1);
              if (last && docsIsTextBlock(last.type) && docsPlain(last.props.text) === "" && siblingsOf(snapNow, last.id).length === 0) {
                focusBlock(last.id, "end");
                return;
              }
              const para = newBlock("paragraph", null, positionAtEnd(snapNow, null));
              apply([para], { block: para.id, start: 0, end: 0 });
              focusBlock(para.id, 0);
            }}
          >
            {snap.blocks.length ? null : <span className="py-1">Start writing, or press / for commands</span>}
          </button>
        ) : null}
      </div>
      <input ref={fileInput} type="file" hidden onChange={(e) => {
        const f = e.target.files?.[0];
        e.target.value = "";
        if (f) void onFile(f);
      }} />
      {slash ? <SlashMenu state={slash} items={slashItems} onPick={(it) => void pickSlash(it)} onClose={() => setSlash(null)} onHover={(i) => setSlash((s) => (s ? { ...s, index: i } : s))} /> : null}
      {mention ? <MentionMenu state={mention} choices={mentionChoices} onPick={pickMention} onClose={() => setMention(null)} onHover={(i) => setMention((s) => (s ? { ...s, index: i } : s))} /> : null}
      {toolbar && toolbarBlock && !linkForm ? (
        <InlineToolbar
          state={toolbar}
          editable={editable}
          canComment={canComment}
          active={{
            bold: docsHasMark(toolbarText, toolbar.start, toolbar.end, "bold"),
            italic: docsHasMark(toolbarText, toolbar.start, toolbar.end, "italic"),
            underline: docsHasMark(toolbarText, toolbar.start, toolbar.end, "underline"),
            strike: docsHasMark(toolbarText, toolbar.start, toolbar.end, "strike"),
            code: docsHasMark(toolbarText, toolbar.start, toolbar.end, "code"),
            link: docsHasMark(toolbarText, toolbar.start, toolbar.end, "link"),
          }}
          onMark={(m) => applyMark(toolbarBlock, m)}
          onLink={() => openLink(toolbarBlock)}
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
      {bookmarkForm ? (
        <BookmarkForm
          anchor={bookmarkForm.rect}
          onClose={() => setBookmarkForm(null)}
          onSave={(url, title) => {
            const b = blockOf(bookmarkForm.blockId);
            setBookmarkForm(null);
            const created = newBlock("bookmark", b?.parent_id ?? null, "a0", { url, title, caption: [] });
            if (b && docsIsTextBlock(b.type) && docsPlain(textOf(b)) === "") apply([{ op: "delete", id: b.id }, { ...created, position: b.position }], { blocks: [created.id] });
            else apply([{ ...created, position: b ? positionAfter(now(), b) : positionAtEnd(now(), null) }], { blocks: [created.id] });
          }}
        />
      ) : null}
      {blockMenu && menuItems.length ? (
        <Floating open anchor={blockMenu.anchor} onClose={() => setBlockMenu(null)} autoFocus label="Block actions" placement="bottom-start">
          <div className="flex max-h-[min(70dvh,32rem)] flex-col overflow-y-auto">
            <FloatingMenu items={menuItems} onDone={() => setBlockMenu(null)} />
          </div>
        </Floating>
      ) : null}
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
