"use client";

import { errorMessage, reposBasename, reposDirname, reposEditDiffFile, reposFormatBytes, useReposBlob, useReposGitInfo, useReposTree, type ApiError, type ReposChange } from "@scaffoldapp/shared";
import { Banner, Button, Card, ErrorState, IconButton, Input, Skeleton, SkeletonGroup, Tab, Tabs, TabsList, cn, toast } from "@scaffoldapp/ui-web";
import { FileUp, Trash2, Upload, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { CommitDialog } from "./CommitDialog";
import { DiffView } from "./DiffView";
import { PathCrumbs } from "./parts";
import { repoHref, useRepoContext } from "./RepoLayout";

/** Warn before leaving with unsaved edits. */
function useUnsavedGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return undefined;
    const onLeave = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onLeave);
    return () => window.removeEventListener("beforeunload", onLeave);
  }, [dirty]);
}

/** Edit a text file (or make a new one) in the browser, review the change, then commit it. */
export function EditScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [path] = useUrlState("path", "");
  const [isNew] = useUrlState("new", "");
  const [initialName] = useUrlState("name", "");
  const creating = isNew === "1";
  const blob = useReposBlob(repo.id, { ref: gitRef, path }, !creating);
  const dir = creating ? path : reposDirname(path);
  const tree = useReposTree(repo.id, { ref: gitRef, path: "" }, creating && !repo.empty);
  const [name, setName] = useState(creating ? (dir ? `${dir}/` : "") + initialName : path);
  const [content, setContent] = useState<string | null>(creating ? (initialName.toLowerCase() === "readme.md" ? `# ${repo.name}\n` : "") : null);
  const [tab, setTab] = useState<"edit" | "changes">("edit");
  const [committing, setCommitting] = useState(false);
  // What the edit started from, pinned on first load: live updates refetch the file, but the
  // commit must name this commit so a branch that moved meanwhile is refused, not overwritten.
  const [base, setBase] = useState<{ sha: string; content: string | null } | null>(null);
  const original = base?.content ?? null;

  useEffect(() => {
    if (base) return;
    if (!creating && blob.data?.encoding === "utf-8") {
      setBase({ sha: blob.data.sha, content: blob.data.content });
      setContent(blob.data.content);
    } else if (creating && (repo.empty || tree.data)) {
      setBase({ sha: repo.empty ? "" : (tree.data?.sha ?? ""), content: null });
    }
  }, [base, blob.data, creating, repo.empty, tree.data]);

  const target = name.trim().replace(/^\/+|\/+$/g, "");
  const dirty = content !== null && (creating ? content !== "" || target !== "" : content !== original || target !== path);
  useUnsavedGuard(dirty && !committing);

  const changes: ReposChange[] = useMemo(() => {
    if (content === null || !target) return [];
    if (creating || target === path) return [{ op: "put", path: target, content }];
    return [
      { op: "delete", path },
      { op: "put", path: target, content, ...(blob.data?.mode === "100755" ? { executable: true } : {}) },
    ];
  }, [content, target, creating, path, blob.data?.mode]);
  const diffFile = useMemo(() => (content === null ? null : reposEditDiffFile(target || "untitled", original, content, creating ? undefined : path)), [content, original, target, creating, path]);
  const expectedHead = base?.sha ?? "";
  const isBranch = !/^[0-9a-f]{40}$/.test(gitRef);
  const badName = target === "" || target.split("/").some((p) => p === "" || p === "." || p === ".." || p.toLowerCase() === ".git");

  if (!repo.can_push) {
    return (
      <Card>
        <ErrorState title="You can't edit this repository" info="Ask someone who manages it for edit access." />
      </Card>
    );
  }
  if (!creating && blob.isError) {
    return (
      <Card>
        <ErrorState title={(blob.error as ApiError).status === 404 ? "File not found" : "Couldn't load this file"} info={errorMessage(blob.error, "Try again.")} onRetry={() => void blob.refetch()} />
      </Card>
    );
  }
  if (!creating && blob.data && !blob.data.editable) {
    return (
      <Card>
        <ErrorState title="This file can't be edited here" info={blob.data.too_large ? "It's larger than the in-browser limit. Clone the repository to edit it." : "It's a binary file, a link or a submodule."} />
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <PathCrumbs repoId={repo.id} repoName={repo.name} gitRef={gitRef} path={dir} />
        <span className="text-fg-subtle" aria-hidden>
          /
        </span>
        <Input
          aria-label="File path"
          value={name.startsWith(dir ? `${dir}/` : "") ? name.slice(dir ? dir.length + 1 : 0) : name}
          onChange={(e) => setName(dir ? `${dir}/${e.target.value}` : e.target.value)}
          placeholder="Name your file"
          className="font-mono"
          wrapperClassName="w-full sm:w-80"
        />
        <span className="type-caption text-fg-muted">
          on <span className="font-mono">{gitRef}</span>
        </span>
      </div>
      {!isBranch ? <Banner tone="warning">You're viewing a commit, not a branch. Switch to a branch to edit.</Banner> : null}
      <Tabs value={tab} onValueChange={(v) => setTab(v as "edit" | "changes")}>
        <TabsList aria-label="Editor">
          <Tab value="edit">Edit</Tab>
          <Tab value="changes" count={diffFile ? diffFile.additions + diffFile.deletions : undefined}>
            Changes
          </Tab>
        </TabsList>
      </Tabs>
      {content === null ? (
        <SkeletonGroup label="Loading file" className="flex flex-col gap-2 rounded-lg border border-border p-4">
          {Array.from({ length: 10 }, (_, i) => (
            <Skeleton key={i} className="h-4" width={`${30 + ((i * 41) % 60)}%`} />
          ))}
        </SkeletonGroup>
      ) : tab === "edit" ? (
        <CodeEditor value={content} onChange={setContent} />
      ) : diffFile && dirty ? (
        <DiffView files={[diffFile]} mode="unified" repoId={repo.id} tree={false} />
      ) : (
        <p className="type-body text-fg-muted">No changes yet.</p>
      )}
      <div className="sticky bottom-0 -mx-gutter flex justify-end gap-2 border-t border-border bg-canvas px-gutter py-3 sm:static sm:mx-0 sm:border-0 sm:bg-transparent sm:px-0">
        <Button variant="ghost" onClick={() => router.back()}>
          Cancel
        </Button>
        <Button disabled={!dirty || badName || !isBranch || changes.length === 0 || !base} onClick={() => setCommitting(true)}>
          Commit changes
        </Button>
      </div>
      <CommitDialog
        open={committing}
        onOpenChange={setCommitting}
        title="Commit changes"
        branch={gitRef}
        expectedHead={expectedHead}
        changes={changes}
        defaultMessage={creating ? `Create ${reposBasename(target)}` : target !== path ? `Rename ${reposBasename(path)} to ${reposBasename(target)}` : `Update ${reposBasename(path)}`}
        onDone={(res) => router.push(repoHref(repo.id, "blob", { ref: res.branch, path: target }))}
      />
    </div>
  );
}

/** A plain-text code editor: monospace, line numbers, no wrapping, Tab indents. */
function CodeEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const gutter = useRef<HTMLDivElement>(null);
  const lines = Math.max(1, value.split("\n").length);
  return (
    <div className="flex max-h-[70vh] min-h-80 overflow-hidden rounded-lg border border-border-strong bg-surface focus-within:outline-2 focus-within:outline-ring">
      <div ref={gutter} aria-hidden className="select-none overflow-hidden border-r border-border bg-sunken py-3 text-right font-mono text-label font-normal text-fg-subtle tabular-nums">
        {Array.from({ length: lines }, (_, i) => (
          <div key={i} className="px-3">
            {i + 1}
          </div>
        ))}
      </div>
      <textarea
        aria-label="File content"
        value={value}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        wrap="off"
        onChange={(e) => onChange(e.target.value)}
        onScroll={(e) => {
          if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop;
        }}
        onKeyDown={(e) => {
          // Tab indents with two spaces; Escape then Tab leaves the editor (keyboard users).
          if (e.key === "Tab" && !e.shiftKey && !e.metaKey && !e.ctrlKey && (e.currentTarget.dataset["escaped"] ?? "") !== "1") {
            e.preventDefault();
            const t = e.currentTarget;
            const { selectionStart: s, selectionEnd: end } = t;
            const next = value.slice(0, s) + "  " + value.slice(end);
            onChange(next);
            requestAnimationFrame(() => t.setSelectionRange(s + 2, s + 2));
          }
          e.currentTarget.dataset["escaped"] = e.key === "Escape" ? "1" : "";
        }}
        className="min-w-0 flex-1 resize-none overflow-auto bg-transparent px-3 py-3 font-mono text-label font-normal text-fg outline-none"
      />
    </div>
  );
}

interface Picked {
  file: File;
  path: string;
}

/** Upload files into a folder as one commit. */
export function UploadScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [dir] = useUrlState("path", "");
  const info = useReposGitInfo();
  const tree = useReposTree(repo.id, { ref: gitRef, path: "" }, !repo.empty);
  const [picked, setPicked] = useState<Picked[]>([]);
  const [changes, setChanges] = useState<ReposChange[] | null>(null);
  const [baseSha, setBaseSha] = useState<string | null>(null);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const total = picked.reduce((n, p) => n + p.file.size, 0);
  const limit = info.data?.max_commit_bytes ?? 25 << 20;
  useUnsavedGuard(picked.length > 0 && !changes);

  const add = (files: FileList | null) => {
    if (!files) return;
    if (baseSha === null) setBaseSha(repo.empty ? "" : (tree.data?.sha ?? null));
    const next = [...picked];
    for (const f of Array.from(files)) {
      const rel = (f as File & { webkitRelativePath?: string }).webkitRelativePath || f.name;
      const p = dir ? `${dir}/${rel}` : rel;
      if (!next.some((x) => x.path === p)) next.push({ file: f, path: p });
    }
    setPicked(next);
  };

  const prepare = async () => {
    try {
      const out: ReposChange[] = [];
      for (const p of picked) out.push({ op: "put", path: p.path, content: await toBase64(p.file), encoding: "base64" });
      setChanges(out);
    } catch {
      toast.error("Couldn't read one of the files. Choose it again.");
    }
  };

  if (!repo.can_push) {
    return (
      <Card>
        <ErrorState title="You can't add files to this repository" info="Ask someone who manages it for edit access." />
      </Card>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <PathCrumbs repoId={repo.id} repoName={repo.name} gitRef={gitRef} path={dir} />
        <span className="type-caption text-fg-muted">
          on <span className="font-mono">{gitRef}</span>
        </span>
      </div>
      <label
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          add(e.dataTransfer.files);
        }}
        className={cn(
          "flex min-h-48 cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed p-6 text-center transition-colors",
          over ? "border-primary bg-primary-soft" : "border-border-strong bg-surface hover:bg-sunken",
        )}
      >
        <FileUp className="size-8 text-fg-muted" aria-hidden />
        <span className="type-body text-fg">Drop files here or choose them</span>
        <Button type="button" variant="outline" size="sm" leftIcon={<Upload />} onClick={() => input.current?.click()}>
          Choose files
        </Button>
        <input ref={input} type="file" multiple className="sr-only" onChange={(e) => add(e.target.files)} />
      </label>
      {picked.length > 0 ? (
        <Card padding="none" className="p-1">
          <ul className="flex flex-col">
            {picked.map((p) => (
              <li key={p.path} className="flex min-h-control items-center gap-3 px-3">
                <span className="min-w-0 flex-1 truncate font-mono type-label">{p.path}</span>
                <span className="type-caption text-fg-muted">{reposFormatBytes(p.file.size)}</span>
                <IconButton label={`Remove ${p.path}`} icon={<X />} variant="ghost" size="sm" onClick={() => setPicked(picked.filter((x) => x !== p))} />
              </li>
            ))}
          </ul>
        </Card>
      ) : null}
      {total > limit ? <Banner tone="danger">These files add up to {reposFormatBytes(total)}; one commit takes up to {reposFormatBytes(limit)}. Remove some, or push them with git.</Banner> : null}
      <div className="flex justify-end gap-2">
        {picked.length > 0 ? (
          <Button variant="ghost" leftIcon={<Trash2 />} onClick={() => setPicked([])}>
            Clear
          </Button>
        ) : null}
        <Button disabled={picked.length === 0 || total > limit} onClick={() => void prepare()}>
          Commit {picked.length || ""} {picked.length === 1 ? "file" : "files"}
        </Button>
      </div>
      <CommitDialog
        open={changes !== null}
        onOpenChange={(o) => !o && setChanges(null)}
        title="Commit uploaded files"
        branch={gitRef}
        expectedHead={baseSha ?? (repo.empty ? "" : (tree.data?.sha ?? ""))}
        changes={changes ?? []}
        defaultMessage={picked.length === 1 ? `Add ${picked[0]!.file.name}` : `Add ${picked.length} files`}
        onDone={(res) => {
          setPicked([]);
          setBaseSha(null);
          router.push(repoHref(repo.id, "", { ref: res.branch, path: dir }));
        }}
      />
    </div>
  );
}

function toBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ""));
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}
