"use client";

import { reposRelativeTime, reposShortSha, useReposPaths, type ReposCommit, type ReposUser } from "@scaffoldapp/shared";
import { Dialog, Input, Tooltip, cn } from "@scaffoldapp/ui-web";
import { FileCode, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { repoHref } from "./RepoLayout";

/** repo / src / components: each part opens that folder. */
export function PathCrumbs({ repoId, repoName, gitRef, path, leaf }: { repoId: string; repoName: string; gitRef: string; path: string; leaf?: "file" | "folder" }) {
  const parts = path ? path.split("/") : [];
  return (
    <nav aria-label="Path" className="flex min-w-0 flex-wrap items-center gap-1 type-label">
      <Link href={repoHref(repoId, "", { ref: gitRef })} className="font-semibold text-primary-strong hover:underline">
        {repoName}
      </Link>
      {parts.map((p, i) => {
        const sub = parts.slice(0, i + 1).join("/");
        const last = i === parts.length - 1;
        return (
          <span key={sub} className="flex min-w-0 items-center gap-1">
            <span className="text-fg-subtle" aria-hidden>
              /
            </span>
            {last ? (
              <span className={cn("truncate", leaf === "file" ? "font-semibold text-fg" : "text-fg")} aria-current="page">
                {p}
              </span>
            ) : (
              <Link href={repoHref(repoId, "", { ref: gitRef, path: sub })} className="truncate text-primary-strong hover:underline">
                {p}
              </Link>
            )}
          </span>
        );
      })}
    </nav>
  );
}

/** "Sam Rivera · Filter routes by wall · 5e2bc8f · 3 days ago" for a commit. */
export function CommitLine({ repoId, commit, className }: { repoId: string; commit: ReposCommit; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 type-label", className)}>
      <span className="font-semibold text-fg">{commit.author.name}</span>
      <Link href={repoHref(repoId, `commits/${commit.sha}`)} className="min-w-0 flex-1 truncate text-fg-muted hover:text-fg hover:underline">
        {commit.subject}
      </Link>
      <Link href={repoHref(repoId, `commits/${commit.sha}`)} className="font-mono type-caption text-fg-muted hover:underline">
        {reposShortSha(commit.sha)}
      </Link>
      <Tooltip content={new Date(commit.committer.at).toLocaleString()}>
        <span className="type-caption text-fg-muted">{reposRelativeTime(commit.committer.at)}</span>
      </Tooltip>
    </div>
  );
}

/** A person's avatar and name (deleted accounts read "Deleted user"). */
export function Person({ user, size = "xs", showName = true }: { user: ReposUser | null; size?: "xs" | "sm"; showName?: boolean }) {
  const name = user?.display_name || "Deleted user";
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <AppAvatar name={name} src={user?.avatar_url ?? null} size={size} />
      {showName ? <span className="truncate font-semibold text-fg">{name}</span> : null}
    </span>
  );
}

/** "Go to file": fuzzy-find a path; T opens it, arrows move, Enter opens. */
export function FileFinder({ repoId, gitRef, open, onOpenChange }: { repoId: string; gitRef: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const router = useRouter();
  const paths = useReposPaths(repoId, q, gitRef, open);
  const items = paths.data?.items ?? [];
  useEffect(() => setActive(0), [q]);
  const go = (p: string) => {
    onOpenChange(false);
    setQ("");
    router.push(repoHref(repoId, "blob", { ref: gitRef, path: p }));
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Go to file" size="lg">
      <div className="flex flex-col gap-3">
        <Input
          autoFocus
          aria-label="File name"
          placeholder="Type part of a file name"
          leadingIcon={<Search aria-hidden />}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive((a) => Math.min(a + 1, items.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(a - 1, 0));
            } else if (e.key === "Enter" && items[active]) {
              e.preventDefault();
              go(items[active]);
            }
          }}
          role="combobox"
          aria-expanded
          aria-controls="repos-finder-list"
          aria-activedescendant={items[active] ? `repos-finder-${active}` : undefined}
        />
        <ul id="repos-finder-list" role="listbox" aria-label="Files" className="flex max-h-96 flex-col overflow-y-auto">
          {items.map((p, i) => (
            <li key={p} id={`repos-finder-${i}`} role="option" aria-selected={i === active}>
              <button
                type="button"
                onClick={() => go(p)}
                onMouseEnter={() => setActive(i)}
                className={cn("flex min-h-control w-full items-center gap-2 rounded-sm px-2 text-left type-label", i === active ? "bg-sunken" : "hover:bg-sunken")}
              >
                <FileCode className="size-4 shrink-0 text-fg-muted" aria-hidden />
                <span className="truncate font-mono">{p}</span>
              </button>
            </li>
          ))}
          {!paths.isPending && items.length === 0 ? <li className="px-2 py-3 type-label text-fg-muted">No files match.</li> : null}
        </ul>
      </div>
    </Dialog>
  );
}

/** Opens the file finder on T (not while typing). */
export function useFinderShortcut(open: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key !== "t" || e.metaKey || e.ctrlKey || e.altKey) return;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
      e.preventDefault();
      open();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);
}

/** A panel with a header row (file and folder views). */
export function Panel({ header, children, className }: { header: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("overflow-hidden rounded-lg border border-border bg-surface", className)}>
      <div className="flex min-h-12 flex-wrap items-center gap-3 border-b border-border bg-sunken px-4 py-2">{header}</div>
      {children}
    </section>
  );
}
