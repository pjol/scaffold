/**
 * DocsPageSession: the client side of a page's block operations, platform-neutral (web
 * editor and mobile editor share it).
 *
 * - `server` is the last confirmed state; local operations apply at once to the view
 *   (server + in flight + pending) and are sent in batches (one request at a time).
 * - Operations from others arrive as `docs.ops` events; each block carries the page
 *   version that last changed it, so stale states are ignored. A gap in versions, a
 *   reconnect or a history restore triggers `reconcile()` (GET …/sync?since=version).
 * - Text edits send the text they started from (`base_text`); when someone else changed
 *   the same block meanwhile, the server merges both edits (docsMergeText), and the view
 *   merges remote text into unsent local text the same way.
 * - Undo and redo replay inverse operations, so they work across collaborators.
 */
import { docsComparePosition } from "./position";
import { docsMergeText } from "./rich";
import type { DocsBlock, DocsBlockProps, DocsOp, DocsOpsResult, DocsPage, DocsSpan, DocsSyncResult } from "./types";

export type DocsSaveState = "saved" | "saving" | "offline" | "error";

export interface DocsSessionTransport {
  send(ops: DocsOp[]): Promise<DocsOpsResult>;
  sync(since: number): Promise<DocsSyncResult>;
}

export interface DocsSessionOptions {
  pageId: string;
  /** Unique per open copy of the page (events it caused come back with it). */
  clientId: string;
  transport: DocsSessionTransport;
  blocks: readonly DocsBlock[];
  version: number;
  /** Page fields that came with a sync (title, lock…). */
  onPage?: (page: DocsPage) => void;
  /** The server refused a change (locked page, lost access); the view was reset to the server's. */
  onRejected?: (message: string, code: string) => void;
  debounceMs?: number;
}

export interface DocsSessionSnapshot {
  /** Every block in document order (parents before children). */
  blocks: readonly DocsBlock[];
  byId: ReadonlyMap<string, DocsBlock>;
  /** Ordered children per parent id ("" = top level). */
  children: ReadonlyMap<string, readonly DocsBlock[]>;
  version: number;
  saveState: DocsSaveState;
  error: string | null;
  canUndo: boolean;
  canRedo: boolean;
  /** Blocks someone else changed in the last remote batch. */
  remoteChanged: ReadonlySet<string>;
  revision: number;
}

export interface DocsApplyOptions<S> {
  /** "push" a new undo step (default), "merge" into the previous step with the same key within 1 s, or "skip". */
  history?: "push" | "merge" | "skip";
  key?: string;
  selectionBefore?: S;
  selectionAfter?: S;
}

interface UndoEntry<S> {
  undo: DocsOp[];
  redo: DocsOp[];
  before: S | undefined;
  after: S | undefined;
  key: string | undefined;
  at: number;
}

type WireOp = DocsOp & { base_text?: DocsSpan[] };

const TEXT_TYPES = new Set(["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"]);
const MERGE_WINDOW_MS = 1000;
const MAX_UNDO = 200;
const MAX_BATCH = 400;

function nowIso(): string {
  return new Date().toISOString();
}

/** The ids of `id` and everything nested in it. */
function subtree(map: ReadonlyMap<string, DocsBlock>, id: string): string[] {
  const kids = new Map<string, string[]>();
  for (const b of map.values()) {
    if (!b.parent_id) continue;
    const list = kids.get(b.parent_id);
    if (list) list.push(b.id);
    else kids.set(b.parent_id, [b.id]);
  }
  const out: string[] = [];
  const walk = (x: string, depth: number) => {
    out.push(x);
    if (depth < 64) for (const k of kids.get(x) ?? []) walk(k, depth + 1);
  };
  if (map.has(id)) walk(id, 0);
  return out;
}

function applyOp(map: Map<string, DocsBlock>, op: DocsOp, pageId: string): void {
  switch (op.op) {
    case "create": {
      const t = nowIso();
      map.set(op.id, {
        id: op.id,
        page_id: pageId,
        parent_id: op.parent_id,
        type: op.type,
        props: op.props,
        position: op.position,
        version: 0,
        created_by: null,
        updated_by: null,
        created_at: t,
        updated_at: t,
      });
      return;
    }
    case "update": {
      const b = map.get(op.id);
      if (b) map.set(op.id, { ...b, ...(op.type ? { type: op.type } : {}), props: { ...b.props, ...op.props } });
      return;
    }
    case "move": {
      const b = map.get(op.id);
      if (b) map.set(op.id, { ...b, parent_id: op.parent_id, position: op.position });
      return;
    }
    case "delete":
      for (const id of subtree(map, op.id)) map.delete(id);
  }
}

/** Operations that undo `op`, computed against `map` before it applies. */
function inverseOf(map: ReadonlyMap<string, DocsBlock>, op: DocsOp): DocsOp[] {
  switch (op.op) {
    case "create":
      return [{ op: "delete", id: op.id }];
    case "delete": {
      const ids = new Set(subtree(map, op.id));
      return orderBlocks(map)
        .blocks.filter((b) => ids.has(b.id))
        .map((b) => ({ op: "create" as const, id: b.id, type: b.type, parent_id: b.parent_id, position: b.position, props: b.props }));
    }
    case "update": {
      const b = map.get(op.id);
      if (!b) return [];
      const props: Record<string, unknown> = {};
      for (const k of Object.keys(op.props)) props[k] = (b.props as Record<string, unknown>)[k] ?? null;
      return [{ op: "update", id: op.id, ...(op.type && op.type !== b.type ? { type: b.type } : {}), props: props as Partial<DocsBlockProps> }];
    }
    case "move": {
      const b = map.get(op.id);
      return b ? [{ op: "move", id: op.id, parent_id: b.parent_id, position: b.position }] : [];
    }
  }
}

/** Document order: siblings by (position, id), children right after their parent. */
export function orderBlocks(map: ReadonlyMap<string, DocsBlock>): { blocks: DocsBlock[]; children: Map<string, DocsBlock[]> } {
  const children = new Map<string, DocsBlock[]>();
  for (const b of map.values()) {
    const key = b.parent_id && map.has(b.parent_id) ? b.parent_id : "";
    const list = children.get(key);
    if (list) list.push(b);
    else children.set(key, [b]);
  }
  for (const list of children.values()) list.sort(docsComparePosition);
  const blocks: DocsBlock[] = [];
  const walk = (key: string, depth: number) => {
    for (const b of children.get(key) ?? []) {
      blocks.push(b);
      if (depth < 64) walk(b.id, depth + 1);
    }
  };
  walk("", 0);
  return { blocks, children };
}

export class DocsPageSession<S = unknown> {
  readonly pageId: string;
  readonly clientId: string;
  private readonly opts: DocsSessionOptions;
  private server = new Map<string, DocsBlock>();
  private pending: WireOp[] = [];
  private inflight: WireOp[] | null = null;
  private view = new Map<string, DocsBlock>();
  private version: number;
  private seen = new Set<number>();
  private undoStack: UndoEntry<S>[] = [];
  private redoStack: UndoEntry<S>[] = [];
  private state: DocsSaveState = "saved";
  private error: string | null = null;
  private remoteChanged: Set<string> = new Set();
  private listeners = new Set<() => void>();
  private snapshot: DocsSessionSnapshot | null = null;
  private revision = 0;
  private flushTimer: ReturnType<typeof setTimeout> | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private gapTimer: ReturnType<typeof setTimeout> | null = null;
  private retries = 0;
  private syncing = false;
  private syncAgain = false;
  private disposed = false;

  constructor(opts: DocsSessionOptions) {
    this.opts = opts;
    this.pageId = opts.pageId;
    this.clientId = opts.clientId;
    this.version = opts.version;
    for (const b of opts.blocks) this.server.set(b.id, b);
    this.rebuild();
  }

  // ---- reading

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  };

  getSnapshot = (): DocsSessionSnapshot => {
    if (!this.snapshot) {
      const { blocks, children } = orderBlocks(this.view);
      this.snapshot = {
        blocks,
        byId: this.view,
        children,
        version: this.version,
        saveState: this.state,
        error: this.error,
        canUndo: this.undoStack.length > 0,
        canRedo: this.redoStack.length > 0,
        remoteChanged: this.remoteChanged,
        revision: this.revision,
      };
    }
    return this.snapshot;
  };

  block(id: string): DocsBlock | undefined {
    return this.view.get(id);
  }

  /** Changes not yet confirmed by the server. */
  hasUnsaved(): boolean {
    return this.pending.length > 0 || this.inflight !== null;
  }

  // ---- local edits

  /** Applies operations now and queues them for the server. */
  apply(ops: DocsOp[], options: DocsApplyOptions<S> = {}): void {
    if (ops.length === 0 || this.disposed) return;
    const scratch = new Map(this.view);
    const undo: DocsOp[] = [];
    for (const op of ops) {
      undo.unshift(...inverseOf(scratch, op));
      applyOp(scratch, op, this.pageId);
    }
    const history = options.history ?? "push";
    if (history !== "skip") {
      const last = this.undoStack[this.undoStack.length - 1];
      const t = Date.now();
      if (history === "merge" && last && options.key && last.key === options.key && t - last.at < MERGE_WINDOW_MS) {
        last.undo = [...undo, ...last.undo];
        last.redo = [...last.redo, ...ops];
        last.after = options.selectionAfter;
        last.at = t;
      } else {
        this.undoStack.push({ undo, redo: ops, before: options.selectionBefore, after: options.selectionAfter, key: options.key, at: t });
        if (this.undoStack.length > MAX_UNDO) this.undoStack.shift();
      }
      this.redoStack = [];
    }
    for (const op of ops) this.enqueue(op);
    this.view = scratch;
    this.changed();
    this.scheduleFlush();
  }

  /** Undoes the last step; returns the selection to restore. */
  undo(): S | undefined {
    const e = this.undoStack.pop();
    if (!e) return undefined;
    this.redoStack.push(e);
    this.replay(e.undo);
    return e.before;
  }

  redo(): S | undefined {
    const e = this.redoStack.pop();
    if (!e) return undefined;
    this.undoStack.push(e);
    this.replay(e.redo);
    return e.after;
  }

  private replay(ops: DocsOp[]): void {
    // Steps may refer to blocks someone else deleted: skip those.
    const scratch = new Map(this.view);
    const live: DocsOp[] = [];
    for (const op of ops) {
      if (op.op !== "create" && !scratch.has(op.id)) continue;
      if (op.op === "create" && scratch.has(op.id)) continue;
      live.push(op);
      applyOp(scratch, op, this.pageId);
    }
    for (const op of live) this.enqueue(op);
    this.view = scratch;
    this.changed();
    this.scheduleFlush();
  }

  private baseText(id: string): DocsSpan[] | undefined {
    const b = this.server.get(id);
    if (!b || !TEXT_TYPES.has(b.type)) return undefined;
    let text = b.props.text ?? [];
    for (const op of this.inflight ?? []) {
      if (op.op === "update" && op.id === id && op.props.text) text = op.props.text;
    }
    return text;
  }

  private enqueue(raw: DocsOp): void {
    let op: WireOp = raw;
    if (op.op === "update") {
      const u = op;
      const ci = this.pending.findIndex((p) => p.op === "create" && p.id === u.id);
      if (ci >= 0) {
        const c = this.pending[ci] as Extract<DocsOp, { op: "create" }>;
        this.pending[ci] = { ...c, ...(u.type ? { type: u.type } : {}), props: { ...c.props, ...u.props } as DocsBlockProps };
        return;
      }
      let li = -1;
      for (let i = this.pending.length - 1; i >= 0; i--) {
        if (this.pending[i]!.id === u.id) {
          li = i;
          break;
        }
      }
      const prev = li >= 0 ? this.pending[li]! : undefined;
      if (prev && prev.op === "update") {
        this.pending[li] = { ...prev, ...(u.type ? { type: u.type } : {}), props: { ...prev.props, ...u.props } };
        const merged = this.pending[li]!;
        if (u.props.text && !merged.base_text) {
          const base = this.baseText(u.id);
          if (base) merged.base_text = base;
        }
        return;
      }
      if (u.props.text) {
        const base = this.baseText(u.id);
        if (base) op = { ...u, base_text: base };
      }
    }
    this.pending.push(op);
  }

  // ---- sending

  private scheduleFlush(): void {
    if (this.flushTimer || this.retryTimer) return;
    this.flushTimer = setTimeout(() => {
      this.flushTimer = null;
      void this.flush();
    }, this.opts.debounceMs ?? 250);
  }

  /** Sends what's pending now (one request at a time). */
  async flush(): Promise<void> {
    if (this.inflight || this.pending.length === 0) return;
    if (this.flushTimer) {
      clearTimeout(this.flushTimer);
      this.flushTimer = null;
    }
    const batch = this.pending.slice(0, MAX_BATCH);
    this.pending = this.pending.slice(MAX_BATCH);
    this.inflight = batch;
    this.setState("saving", null);
    let res: DocsOpsResult;
    try {
      res = await this.opts.transport.send(batch);
    } catch (e) {
      const err = e as { status?: number; code?: string; message?: string };
      const status = typeof err.status === "number" ? err.status : 0;
      if (status === 0 || status === 408 || status === 429 || status >= 500) {
        this.pending = [...batch, ...this.pending];
        this.inflight = null;
        this.setState(status === 0 ? "offline" : "error", status === 0 ? null : (err.message ?? null));
        const delay = Math.min(15000, 1000 * 2 ** this.retries++);
        this.retryTimer = setTimeout(() => {
          this.retryTimer = null;
          void this.flush();
        }, delay);
      } else {
        // The server refused the batch (locked, trashed, access lost, invalid): drop it
        // and go back to what the server has.
        this.inflight = null;
        this.pending = [];
        this.setState("error", err.message ?? "Couldn't save that change.");
        this.opts.onRejected?.(err.message ?? "Couldn't save that change.", err.code ?? "rejected");
        void this.reconcile(true);
      }
      return;
    }
    this.retries = 0;
    this.inflight = null;
    this.absorb(res, true);
    const refused = (res.rejected ?? []).find((r) => r.code !== "gone");
    if (refused) {
      this.opts.onRejected?.(refused.message, refused.code);
      void this.reconcile();
    }
    this.afterSend();
  }

  private afterSend(): void {
    if (this.pending.length > 0) void this.flush();
    else if (!this.inflight) this.setState("saved", null);
  }

  /** Sends anything queued right away (navigating away, the tab closing). */
  flushNow(): void {
    if (this.retryTimer) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    void this.flush();
  }

  // ---- receiving

  /** A `docs.ops` event for this page. */
  receive(ev: DocsOpsResult): void {
    if (ev.page_id !== this.pageId || this.disposed) return;
    this.absorb(ev, ev.client_id === this.clientId);
  }

  private absorb(ev: DocsOpsResult, mine: boolean): void {
    const before = new Map<string, DocsSpan[]>();
    const changed = new Set<string>();
    for (const b of ev.blocks ?? []) {
      const cur = this.server.get(b.id);
      if (cur && cur.version > b.version) continue;
      if (cur && TEXT_TYPES.has(cur.type)) before.set(b.id, cur.props.text ?? []);
      this.server.set(b.id, b);
      changed.add(b.id);
    }
    for (const id of ev.deleted ?? []) {
      this.server.delete(id);
      changed.add(id);
    }
    if (!mine) {
      // Merge someone else's text into what we haven't sent yet.
      for (const op of this.pending) {
        if (op.op !== "update" || !op.props.text || !before.has(op.id)) continue;
        if (this.inflight?.some((x) => x.id === op.id)) continue;
        const remote = this.server.get(op.id)?.props.text ?? [];
        op.props = { ...op.props, text: docsMergeText(op.base_text ?? before.get(op.id)!, op.props.text, remote) };
        op.base_text = remote;
      }
      this.remoteChanged = changed;
    }
    this.noteVersion(ev.version);
    this.rebuild();
  }

  private noteVersion(v: number): void {
    if (v <= this.version) return;
    this.seen.add(v);
    while (this.seen.has(this.version + 1)) {
      this.seen.delete(this.version + 1);
      this.version++;
    }
    if (this.seen.size > 0 && !this.gapTimer) {
      this.gapTimer = setTimeout(() => {
        this.gapTimer = null;
        if (this.seen.size > 0) void this.reconcile();
      }, 1500);
    }
  }

  /** Catches up with the server (after a reconnect, a gap, a restore). `full` reloads every block. */
  async reconcile(full = false): Promise<void> {
    if (this.disposed) return;
    if (this.syncing) {
      this.syncAgain = true;
      return;
    }
    this.syncing = true;
    try {
      const res = await this.opts.transport.sync(full ? 0 : this.version);
      if (res.full || full) this.server = new Map(res.blocks.map((b) => [b.id, b]));
      else {
        for (const id of res.deleted ?? []) this.server.delete(id);
        for (const b of res.blocks ?? []) {
          const cur = this.server.get(b.id);
          if (!cur || cur.version <= b.version) this.server.set(b.id, b);
        }
      }
      this.version = Math.max(full ? res.version : this.version, res.version);
      for (const v of [...this.seen]) if (v <= this.version) this.seen.delete(v);
      this.opts.onPage?.(res.page);
      this.rebuild();
      if (this.state === "error" && !this.hasUnsaved()) this.setState("saved", null);
    } catch {
      // Next reconnect or edit tries again.
    } finally {
      this.syncing = false;
      if (this.syncAgain) {
        this.syncAgain = false;
        void this.reconcile();
      }
    }
  }

  // ---- internals

  private rebuild(): void {
    const view = new Map(this.server);
    for (const op of this.inflight ?? []) applyOp(view, op, this.pageId);
    for (const op of this.pending) applyOp(view, op, this.pageId);
    this.view = view;
    this.changed();
  }

  private setState(state: DocsSaveState, error: string | null): void {
    if (this.state === state && this.error === error) return;
    this.state = state;
    this.error = error;
    this.changed();
  }

  private changed(): void {
    this.snapshot = null;
    this.revision++;
    for (const fn of [...this.listeners]) fn();
  }

  /** Stops timers after sending what's queued. */
  dispose(): void {
    this.flushNow();
    this.disposed = true;
    if (this.gapTimer) clearTimeout(this.gapTimer);
    this.listeners.clear();
  }
}
