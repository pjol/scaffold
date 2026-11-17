"use client";

import type { DocsBlock, DocsDraftBlock, DocsOp, DocsPageSession, DocsRefs, DocsSessionSnapshot, DocsSpan, DocsViewer } from "@scaffoldapp/shared";
import { createContext, useContext, type ClipboardEvent, type KeyboardEvent, type PointerEvent } from "react";
import type { Decoration } from "./dom";
import type { RichTextHandle } from "./RichText";

/** What undo and redo restore. */
export type EditorSelection = { block: string; start: number; end: number } | { blocks: string[] } | null;

export interface EditorContextValue {
  session: DocsPageSession<EditorSelection>;
  snap: DocsSessionSnapshot;
  pageId: string;
  orgId: string;
  /** The block whose children this editor shows (a synced copy's original), or null for the whole page. */
  root: string | null;
  editable: boolean;
  /** A block shown but not changed here (connected pages: "Edit in Notion"). */
  isReadOnly: (id: string) => boolean;
  /** Blocks can be dragged and moved (not on connected pages). */
  canMove: boolean;
  canComment: boolean;
  refs: DocsRefs;
  viewersByBlock: ReadonlyMap<string, DocsViewer[]>;
  commentCounts: Readonly<Record<string, number>>;
  decorations: ReadonlyMap<string, Decoration[]>;
  selected: ReadonlySet<string>;
  dropTarget: { id: string; place: DropPlace } | null;
  dragging: ReadonlySet<string>;
  isOpen: (id: string) => boolean;
  toggleOpen: (id: string, open?: boolean) => void;
  register: (id: string, handle: RichTextHandle | null) => void;
  registerRow: (id: string, el: HTMLElement | null) => void;
  apply: (ops: DocsOp[], after?: EditorSelection, history?: "push" | "merge" | "skip", key?: string) => void;
  onTextChange: (b: DocsBlock, spans: DocsSpan[], caret: { start: number; end: number } | null) => void;
  onTextKeyDown: (b: DocsBlock, e: KeyboardEvent<HTMLDivElement>) => void;
  onEnter: (b: DocsBlock, shift: boolean) => void;
  onPaste: (b: DocsBlock, e: ClipboardEvent<HTMLElement>) => void;
  onFocusBlock: (b: DocsBlock) => void;
  onSelectionChange: (b: DocsBlock, caret: { start: number; end: number } | null) => void;
  onRowMouseDown: (b: DocsBlock, e: React.MouseEvent) => void;
  startDrag: (b: DocsBlock, e: PointerEvent<HTMLElement>) => void;
  openComments: (blockId: string | null, threadId?: string) => void;
  openPage: (id: string) => void;
  focusBlock: (id: string, at?: number | "start" | "end") => void;
  /** A new empty paragraph right after (or before) a block, focused. */
  addBlockNear: (b: DocsBlock, where: "before" | "after") => void;
  /** Opens the editor for an inline equation (offsets of its chip in the block's text). */
  editInlineEquation: (b: DocsBlock, start: number, end: number, rect: DOMRect) => void;
  /** Inserts draft blocks right after a block (buttons). */
  insertAfter: (b: DocsBlock, drafts: DocsDraftBlock[]) => void;
  /** Opens the upload picker for a media block's kind, replacing `b` (an empty block) or after it. */
  pickUpload: (b: DocsBlock, kind: "image" | "video" | "audio" | "pdf" | "file") => void;
  /** Uploads files into the page after `b` (drop, paste). */
  uploadFiles: (files: File[], after: DocsBlock | null) => void;
  /** Builds a page's content with blocks from another page (synced copies) using the same handlers. */
  nested: NestedEditorProps;
}

/** How a block may land when dragged: above, below, nested, or beside it as columns. */
export type DropPlace = "before" | "after" | "inside" | "left" | "right";

/** What a nested editor (a synced copy showing its original) borrows from the page's editor. */
export interface NestedEditorProps {
  canComment: boolean;
  onOpenPage: (id: string) => void;
  onMoveToPage: (blockIds: string[]) => void;
}

export const EditorContext = createContext<EditorContextValue | null>(null);

export function useEditor(): EditorContextValue {
  const ctx = useContext(EditorContext);
  if (!ctx) throw new Error("useEditor outside the editor");
  return ctx;
}
