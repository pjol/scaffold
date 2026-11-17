"use client";

import { reposHighlightLines, reposLanguageOf, useReposBlob, type ReposDiffFile, type ReposDiffLine, type ReposThread, type ReposToken } from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, IconButton, SegmentedControl, Skeleton, SkeletonGroup, TextArea, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronRight, FileCode, FilePen, MessageSquarePlus } from "lucide-react";
import Link from "next/link";
import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { TokenLine } from "./code";

export type DiffMode = "unified" | "split";

const MODE_KEY = "repos.diff-mode";
/** Changed lines above which a file starts collapsed. */
const COLLAPSE_LINES = 400;
/** Total lines above which diff lines aren't highlighted. */
const HIGHLIGHT_LIMIT = 4000;

/** The person's unified/split choice, remembered on this device. */
export function useDiffMode(): [DiffMode, (m: DiffMode) => void] {
  const [mode, setMode] = useState<DiffMode>("unified");
  useEffect(() => {
    try {
      const v = localStorage.getItem(MODE_KEY);
      if (v === "split" || v === "unified") setMode(v);
    } catch {
      /* storage unavailable: keep the default */
    }
  }, []);
  return [
    mode,
    (m) => {
      setMode(m);
      try {
        localStorage.setItem(MODE_KEY, m);
      } catch {
        /* not remembered */
      }
    },
  ];
}

export function DiffModeToggle({ mode, onChange }: { mode: DiffMode; onChange: (m: DiffMode) => void }) {
  return (
    <SegmentedControl<DiffMode>
      aria-label="Diff layout"
      fullWidth={false}
      value={mode}
      onValueChange={onChange}
      options={[
        { value: "unified", label: "Unified" },
        { value: "split", label: "Split" },
      ]}
      className="hidden md:inline-flex"
    />
  );
}

/** Where line comments go and how to make one. */
export interface DiffComments {
  threads: ReposThread[];
  canComment: boolean;
  /** `pending`: held in the person's review until they submit it. */
  onComment: (c: { path: string; line: number; side: "old" | "new"; body: string; pending?: boolean }) => Promise<unknown>;
  renderThread: (t: ReposThread) => ReactNode;
  /** Offer "Add to review" (a pending review) beside "Comment now". */
  reviews?: boolean;
  /** Offer "Suggest a change" on new-side lines (people who can push to the head). */
  suggestions?: boolean;
}

/** Per-person "Viewed" marks on files. */
export interface DiffViewed {
  paths: string[];
  onToggle: (path: string, viewed: boolean) => void;
}

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|bmp|ico)$/i;

export interface DiffViewProps {
  files: ReposDiffFile[];
  mode: DiffMode;
  /** Links each file header to the file at this ref. */
  repoId: string;
  headRef?: string;
  comments?: DiffComments;
  /** Fetch one file's full patch (for files cut by the size limits). */
  loadFile?: (path: string) => Promise<ReposDiffFile | null>;
  filesTruncated?: boolean;
  /** Show the file list beside the diff (≥ 1024 px). */
  tree?: boolean;
  /** "Viewed" checkboxes (pull requests). */
  viewed?: DiffViewed;
  /** Show changed images side by side, read at these commits. */
  images?: { base: string; head: string };
}

const STATUS_TONE = { added: "success", deleted: "danger", renamed: "info", copied: "info", modified: "neutral", type_changed: "warning" } as const;
const STATUS_LETTER = { added: "A", deleted: "D", renamed: "R", copied: "C", modified: "M", type_changed: "T" } as const;

function fileAnchor(path: string): string {
  return "diff-" + path.replace(/[^a-zA-Z0-9]/g, "-");
}

/** Changed files with unified or split diffs, an optional file list, and line comments. */
export function DiffView({ files, mode, repoId, headRef, comments, loadFile, filesTruncated, tree = true, viewed, images }: DiffViewProps) {
  const totalLines = useMemo(() => files.reduce((n, f) => n + f.hunks.reduce((m, h) => m + h.lines.length, 0), 0), [files]);
  const highlight = totalLines <= HIGHLIGHT_LIMIT;
  if (files.length === 0) {
    return <p className="type-body text-fg-muted">No files changed.</p>;
  }
  const list = (
    <div className="flex min-w-0 flex-col gap-4">
      {filesTruncated ? <p className="type-label text-fg-muted">This change touches too many files to show them all here. Clone the repository to see the rest.</p> : null}
      {files.map((f) => (
        <DiffFileCard
          key={f.path + f.old_path}
          file={f}
          mode={mode}
          repoId={repoId}
          headRef={headRef}
          highlight={highlight}
          comments={comments}
          loadFile={loadFile}
          viewed={viewed}
          images={images}
        />
      ))}
    </div>
  );
  if (!tree || files.length < 2) return list;
  return (
    <div className="lg:grid lg:grid-cols-[15rem_minmax(0,1fr)] lg:gap-6">
      <nav aria-label="Changed files" className="sticky top-20 hidden max-h-[calc(100dvh-6rem)] self-start overflow-y-auto lg:block">
        <ul className="flex flex-col gap-0.5">
          {files.map((f) => (
            <li key={f.path + f.old_path}>
              <a
                href={`#${fileAnchor(f.path)}`}
                className="flex min-h-8 items-center gap-2 rounded-sm px-2 type-label text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                title={f.path}
              >
                <span className={cn("w-3 shrink-0 font-mono type-caption", f.status === "added" ? "text-success-strong" : f.status === "deleted" ? "text-danger-strong" : "text-fg-muted")}>
                  {STATUS_LETTER[f.status]}
                </span>
                <span className="min-w-0 flex-1 truncate">{f.path.split("/").pop()}</span>
                <span className="shrink-0 type-caption tabular-nums text-fg-subtle">{f.additions + f.deletions}</span>
              </a>
            </li>
          ))}
        </ul>
      </nav>
      {list}
    </div>
  );
}

function DiffFileCard({
  file,
  mode,
  repoId,
  headRef,
  highlight,
  comments,
  loadFile,
  viewed,
  images,
}: {
  file: ReposDiffFile;
  mode: DiffMode;
  repoId: string;
  headRef?: string | undefined;
  highlight: boolean;
  comments?: DiffComments | undefined;
  loadFile?: ((path: string) => Promise<ReposDiffFile | null>) | undefined;
  viewed?: DiffViewed | undefined;
  images?: { base: string; head: string } | undefined;
}) {
  const [full, setFull] = useState<ReposDiffFile | null>(null);
  const [loading, setLoading] = useState(false);
  const f = full ?? file;
  const big = f.additions + f.deletions > COLLAPSE_LINES;
  const isViewed = !!viewed?.paths.includes(f.path);
  const [open, setOpen] = useState(!big && f.status !== "deleted" && !isViewed);
  const isImage = f.binary && images && (IMAGE_EXT.test(f.path) || IMAGE_EXT.test(f.old_path));
  const threads = useMemo(() => (comments ? comments.threads.filter((t) => t.root.path === f.path) : []), [comments, f.path]);
  const viewHref = f.status !== "deleted" ? `/repos/${repoId}/blob?${new URLSearchParams({ path: f.path, ...(headRef ? { ref: headRef } : {}) }).toString()}` : null;

  async function load() {
    if (!loadFile) return;
    setLoading(true);
    try {
      const got = await loadFile(f.path);
      if (got) {
        setFull(got);
        setOpen(true);
      }
    } catch {
      toast.error("Couldn't load this file's changes.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card padding="none" id={fileAnchor(f.path)} className="scroll-mt-24 overflow-hidden">
      <div className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-sunken px-2 py-1">
        <IconButton label={open ? "Collapse file" : "Expand file"} icon={open ? <ChevronDown /> : <ChevronRight />} variant="ghost" size="sm" onClick={() => setOpen(!open)} />
        <span className="flex min-w-0 flex-1 items-center gap-2 font-mono type-label">
          <span className="truncate" title={f.path}>
            {f.old_path !== f.path ? (
              <>
                <span className="text-fg-muted">{f.old_path}</span> → {f.path}
              </>
            ) : (
              f.path
            )}
          </span>
        </span>
        <Badge tone={STATUS_TONE[f.status]} className="hidden sm:inline-flex">
          {f.status.replace("_", " ")}
        </Badge>
        <span className="type-caption tabular-nums">
          <span className="text-success-strong">+{f.additions}</span> <span className="text-danger-strong">−{f.deletions}</span>
        </span>
        {viewHref ? (
          <Button asChild variant="ghost" size="sm" leftIcon={<FileCode />}>
            <Link href={viewHref} aria-label={`View ${f.path}`}>
              <span className="hidden sm:inline">View</span>
            </Link>
          </Button>
        ) : null}
        {viewed ? (
          <span className="flex items-center rounded-sm border border-border px-2">
            <Checkbox
              checked={isViewed}
              onCheckedChange={(on) => {
                viewed.onToggle(f.path, on);
                setOpen(!on);
              }}
              label={<span className="type-caption">Viewed</span>}
            />
          </span>
        ) : null}
      </div>
      {!open ? null : isImage ? (
        <ImageDiff repoId={repoId} file={f} refs={images!} />
      ) : f.binary ? (
        <p className="px-4 py-3 type-label text-fg-muted">Binary file not shown.</p>
      ) : f.truncated ? (
        <div className="flex items-center gap-3 px-4 py-3 type-label text-fg-muted">
          <span>This file's diff is large.</span>
          {loadFile ? (
            <Button size="sm" variant="outline" loading={loading} onClick={() => void load()}>
              Load diff
            </Button>
          ) : null}
        </div>
      ) : f.hunks.length === 0 ? (
        <p className="px-4 py-3 type-label text-fg-muted">{f.status === "renamed" ? "Renamed without changes." : "Only the file mode changed."}</p>
      ) : (
        <FileDiff file={f} mode={mode} highlight={highlight} comments={comments} threads={threads} />
      )}
      {!open && big ? (
        <div className="px-4 py-3">
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            Show {f.additions + f.deletions} changed lines
          </Button>
        </div>
      ) : null}
    </Card>
  );
}

type Row =
  | { kind: "hunk"; header: string }
  | { kind: "line"; line: ReposDiffLine }
  | { kind: "pair"; left: ReposDiffLine | null; right: ReposDiffLine | null };

function useLineTokens(lines: ReposDiffLine[], language: string, highlight: boolean): Map<ReposDiffLine, ReposToken[]> {
  return useMemo(() => {
    const m = new Map<ReposDiffLine, ReposToken[]>();
    for (const l of lines) {
      if (l.type === "note") continue;
      m.set(l, highlight ? (reposHighlightLines(l.text, language)[0] ?? []) : l.text ? [{ text: l.text }] : []);
    }
    return m;
  }, [lines, language, highlight]);
}

function FileDiff({
  file,
  mode,
  highlight,
  comments,
  threads,
}: {
  file: ReposDiffFile;
  mode: DiffMode;
  highlight: boolean;
  comments?: DiffComments | undefined;
  threads: ReposThread[];
}) {
  const language = reposLanguageOf(file.path);
  const allLines = useMemo(() => file.hunks.flatMap((h) => h.lines), [file]);
  const tokens = useLineTokens(allLines, language, highlight);
  const [composer, setComposer] = useState<{ line: number; side: "old" | "new" } | null>(null);
  const at = (side: "old" | "new", n: number) => threads.filter((t) => t.root.side === side && t.root.line === n && !t.outdated);

  const rows: Row[] = useMemo(() => {
    const out: Row[] = [];
    for (const h of file.hunks) {
      out.push({ kind: "hunk", header: `@@ -${h.old_start},${h.old_lines} +${h.new_start},${h.new_lines} @@ ${h.header}`.trim() });
      if (mode === "unified") {
        for (const l of h.lines) out.push({ kind: "line", line: l });
        continue;
      }
      // Split: pair each run of deletions with the additions that follow it.
      let i = 0;
      const ls = h.lines;
      while (i < ls.length) {
        const l = ls[i]!;
        if (l.type === "context" || l.type === "note") {
          out.push({ kind: "pair", left: l, right: l.type === "note" ? null : l });
          i++;
          continue;
        }
        const dels: ReposDiffLine[] = [];
        const adds: ReposDiffLine[] = [];
        while (i < ls.length && ls[i]!.type === "del") dels.push(ls[i++]!);
        while (i < ls.length && ls[i]!.type === "add") adds.push(ls[i++]!);
        for (let k = 0; k < Math.max(dels.length, adds.length); k++) out.push({ kind: "pair", left: dels[k] ?? null, right: adds[k] ?? null });
      }
    }
    return out;
  }, [file, mode]);

  const cols = mode === "unified" ? 3 : 4;
  const lineText = (side: "old" | "new", n: number) => allLines.find((l) => (side === "new" ? l.new === n && l.type !== "del" : l.old === n && l.type === "del"))?.text ?? "";
  const extra = (side: "old" | "new", n: number) => {
    const ts = n ? at(side, n) : [];
    const open = composer && composer.side === side && composer.line === n;
    if (!ts.length && !open) return null;
    return (
      <tr key={`x-${side}-${n}`}>
        <td colSpan={cols} className="border-y border-border bg-canvas px-3 py-3 font-sans">
          <div className="flex max-w-3xl flex-col gap-3">
            {ts.map((t) => (
              <Fragment key={t.root.id}>{comments?.renderThread(t)}</Fragment>
            ))}
            {open && comments ? (
              <LineComposer
                reviews={!!comments.reviews}
                suggestion={comments.suggestions && side === "new" ? lineText(side, n) : undefined}
                onCancel={() => setComposer(null)}
                onSubmit={async (body, pending) => {
                  await comments.onComment({ path: file.path, line: n, side, body, ...(pending ? { pending: true } : {}) });
                  setComposer(null);
                }}
              />
            ) : null}
          </div>
        </td>
      </tr>
    );
  };

  const num = (l: ReposDiffLine | null, side: "old" | "new") => {
    const n = l ? (side === "old" ? l.old : l.new) : 0;
    const canAdd = !!comments?.canComment && !!l && !!n && l.type !== "note" && (side === "new" ? l.type !== "del" : l.type === "del");
    return (
      <td className="group w-1 select-none whitespace-nowrap px-0 text-right align-top text-fg-subtle tabular-nums">
        <span className="relative flex min-w-12 items-center justify-end px-2">
          {canAdd ? (
            <button
              type="button"
              aria-label={`Comment on line ${n}`}
              onClick={() => setComposer({ line: n, side })}
              className="absolute left-0 flex size-5 items-center justify-center rounded-xs bg-primary text-primary-fg opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [&_svg]:size-3"
            >
              <MessageSquarePlus aria-hidden />
            </button>
          ) : null}
          {n || ""}
        </span>
      </td>
    );
  };

  const lineClass = (t: ReposDiffLine["type"] | undefined) =>
    t === "add" ? "bg-success-soft" : t === "del" ? "bg-danger-soft" : t === "note" ? "text-fg-muted italic" : "";
  const sign = (t: ReposDiffLine["type"] | undefined) => (t === "add" ? "+" : t === "del" ? "−" : " ");

  return (
    <div className="overflow-x-auto font-mono text-label font-normal">
      <table className="w-full border-collapse">
        <tbody>
          {rows.map((r, i) => {
            if (r.kind === "hunk") {
              return (
                <tr key={i} className="bg-info-soft text-info-strong">
                  <td colSpan={cols} className="whitespace-pre px-3 py-1 type-caption">
                    {r.header}
                  </td>
                </tr>
              );
            }
            if (r.kind === "line") {
              const l = r.line;
              return (
                <Fragment key={i}>
                  <tr className={cn("align-top", lineClass(l.type))}>
                    {num(l.type === "add" ? null : l, "old")}
                    {num(l.type === "del" ? null : l, "new")}
                    <td className="whitespace-pre px-3 text-fg">
                      <span className="mr-2 select-none text-fg-muted" aria-hidden>
                        {sign(l.type)}
                      </span>
                      {l.type === "note" ? l.text : <TokenLine tokens={tokens.get(l) ?? []} />}
                    </td>
                  </tr>
                  {l.type === "del" ? extra("old", l.old) : extra("new", l.new)}
                </Fragment>
              );
            }
            const { left, right } = r;
            return (
              <Fragment key={i}>
                <tr className="align-top">
                  {num(left, "old")}
                  <td className={cn("w-1/2 whitespace-pre-wrap border-r border-border px-3 text-fg [overflow-wrap:anywhere]", left?.type === "del" && "bg-danger-soft", left?.type === "note" && "italic text-fg-muted")}>
                    {left ? left.type === "note" ? left.text : <TokenLine tokens={tokens.get(left) ?? []} /> : null}
                  </td>
                  {num(right, "new")}
                  <td className={cn("w-1/2 whitespace-pre-wrap px-3 text-fg [overflow-wrap:anywhere]", right?.type === "add" && "bg-success-soft")}>
                    {right ? <TokenLine tokens={tokens.get(right) ?? []} /> : null}
                  </td>
                </tr>
                {left?.type === "del" ? extra("old", left.old) : null}
                {right ? extra("new", right.new) : null}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function LineComposer({
  onSubmit,
  onCancel,
  reviews,
  suggestion,
}: {
  onSubmit: (body: string, pending: boolean) => Promise<void>;
  onCancel: () => void;
  reviews: boolean;
  /** The line's current text, to start a suggestion from (undefined: no suggestions). */
  suggestion?: string | undefined;
}) {
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState<"now" | "pending" | null>(null);
  const send = async (pending: boolean) => {
    if (!body.trim()) return;
    setBusy(pending ? "pending" : "now");
    try {
      await onSubmit(body, pending);
      setBody("");
    } catch {
      /* the caller shows the error */
    } finally {
      setBusy(null);
    }
  };
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        void send(reviews);
      }}
    >
      <TextArea aria-label="Comment" placeholder="Leave a comment" value={body} onChange={(e) => setBody(e.target.value)} autoFocus minRows={3} />
      <div className="flex flex-wrap items-center justify-end gap-2">
        {suggestion !== undefined ? (
          <Tooltip content="Propose new text for this line; people who can push apply it with one click.">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              leftIcon={<FilePen />}
              className="mr-auto"
              onClick={() => setBody((b) => `${b}${b && !b.endsWith("\n") ? "\n" : ""}\`\`\`suggestion\n${suggestion}\n\`\`\`\n`)}
            >
              Suggest a change
            </Button>
          </Tooltip>
        ) : null}
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        {reviews ? (
          <>
            <Button type="button" variant="outline" size="sm" loading={busy === "now"} disabled={!body.trim() || !!busy} onClick={() => void send(false)}>
              Comment now
            </Button>
            <Button type="submit" size="sm" loading={busy === "pending"} disabled={!body.trim() || !!busy}>
              Add to review
            </Button>
          </>
        ) : (
          <Button type="submit" size="sm" loading={!!busy} disabled={!body.trim()}>
            Comment
          </Button>
        )}
      </div>
    </form>
  );
}

/** A changed image: before and after, read from git. */
function ImageDiff({ repoId, file, refs }: { repoId: string; file: ReposDiffFile; refs: { base: string; head: string } }) {
  const before = useReposBlob(repoId, { ref: refs.base, path: file.old_path || file.path }, file.status !== "added");
  const after = useReposBlob(repoId, { ref: refs.head, path: file.path }, file.status !== "deleted");
  const side = (label: string, q: typeof before, shown: boolean) => (
    <figure className="flex min-w-0 flex-1 flex-col items-center gap-2">
      <figcaption className={cn("type-caption font-semibold", label === "Before" ? "text-danger-strong" : "text-success-strong")}>{label}</figcaption>
      {!shown ? (
        <p className="type-label text-fg-muted">{label === "Before" ? "Added in this change." : "Deleted in this change."}</p>
      ) : q.isPending ? (
        <Skeleton className="h-40 w-full max-w-xs" />
      ) : q.data?.image && q.data.encoding === "base64" ? (
        // eslint-disable-next-line @next/next/no-img-element -- a data URL from the repository
        <img
          src={`data:${mimeOf(file.path)};base64,${q.data.content}`}
          alt={`${file.path}, ${label.toLowerCase()}`}
          className={cn("max-h-80 max-w-full rounded-xs border-2 bg-sunken", label === "Before" ? "border-danger" : "border-success")}
        />
      ) : (
        <p className="type-label text-fg-muted">{q.data?.too_large ? "Too large to show." : "Can't show this image."}</p>
      )}
    </figure>
  );
  return (
    <div className="flex flex-col gap-4 p-4 sm:flex-row">
      {side("Before", before, file.status !== "added")}
      {side("After", after, file.status !== "deleted")}
    </div>
  );
}

function mimeOf(p: string): string {
  const ext = (p.split(".").pop() ?? "").toLowerCase();
  return ext === "jpg" || ext === "jpeg" ? "image/jpeg" : ext === "gif" ? "image/gif" : ext === "webp" ? "image/webp" : ext === "ico" ? "image/x-icon" : ext === "bmp" ? "image/bmp" : "image/png";
}

/** Placeholder shaped like a file diff while it loads. */
export function DiffSkeleton({ files = 2 }: { files?: number }) {
  return (
    <SkeletonGroup label="Loading changes" className="flex flex-col gap-4">
      {Array.from({ length: files }, (_, i) => (
        <Card key={i} padding="none" className="overflow-hidden">
          <div className="flex h-12 items-center gap-3 border-b border-border bg-sunken px-4">
            <Skeleton className="h-4 w-48" />
          </div>
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 6 }, (_, j) => (
              <Skeleton key={j} className="h-4" width={`${40 + ((j * 37) % 50)}%`} />
            ))}
          </div>
        </Card>
      ))}
    </SkeletonGroup>
  );
}
