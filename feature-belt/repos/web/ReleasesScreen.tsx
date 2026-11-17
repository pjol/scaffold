"use client";

import {
  errorMessage,
  reposFormatBytes,
  reposRelativeTime,
  reposShortSha,
  useAddReposReleaseAsset,
  useDeleteReposRelease,
  useDeleteReposReleaseAsset,
  useDownloadReposReleaseAsset,
  useReposGitInfo,
  useReposRefs,
  useReposReleaseNotes,
  useReposReleases,
  useSaveReposRelease,
  useUpload,
  type ReposRelease,
  type ReposReleaseAsset,
  type ReposRepo,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Select,
  Skeleton,
  SkeletonGroup,
  Switch,
  TextArea,
  Tooltip,
  toast,
} from "@scaffoldapp/ui-web";
import { Download, FileArchive, MoreHorizontal, Package, Paperclip, Pencil, Plus, Rocket, Sparkles, Tag, Trash2 } from "lucide-react";
import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { RepoMarkdown } from "./Markdown";
import { Person } from "./parts";
import { repoHref, useRepoContext } from "./RepoLayout";

/** Releases: a tag with notes and files. People who can edit make, publish, edit and delete them. */
export function ReleasesScreen() {
  const repo = useRepoContext();
  const releases = useReposReleases(repo.id);
  const [editing, setEditing] = useState<ReposRelease | "new" | null>(null);
  const items = releases.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
          <Package className="size-5 text-fg-muted" aria-hidden />
          Releases
        </h2>
        {repo.can_push && !repo.empty ? (
          <Button size="auto" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New release
          </Button>
        ) : null}
      </div>
      {releases.isPending ? (
        <SkeletonGroup label="Loading releases" className="flex flex-col gap-4">
          {Array.from({ length: 2 }, (_, i) => (
            <Skeleton key={i} className="h-48 w-full" />
          ))}
        </SkeletonGroup>
      ) : releases.isError ? (
        <Card>
          <ErrorState info={errorMessage(releases.error, "Try again.")} onRetry={() => void releases.refetch()} retrying={releases.isFetching} />
        </Card>
      ) : items.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Package />}
            title="No releases yet"
            info="A release names a tag, says what changed and carries files people download."
            {...(repo.can_push && !repo.empty
              ? {
                  action: (
                    <Button variant="outline" onClick={() => setEditing("new")}>
                      Make the first release
                    </Button>
                  ),
                }
              : {})}
          />
        </Card>
      ) : (
        <div className="flex flex-col gap-6">
          {items.map((r) => (
            <ReleaseCard key={r.id} repo={repo} release={r} onEdit={() => setEditing(r)} />
          ))}
          <LoadMore hasNextPage={!!releases.hasNextPage} isFetchingNextPage={releases.isFetchingNextPage} fetchNextPage={releases.fetchNextPage} />
        </div>
      )}
      {editing ? <ReleaseDialog repo={repo} release={editing === "new" ? null : editing} previousTag={items.find((r) => !r.draft)?.tag} onClose={() => setEditing(null)} /> : null}
    </div>
  );
}

function ReleaseCard({ repo, release: r, onEdit }: { repo: ReposRepo; release: ReposRelease; onEdit: () => void }) {
  const [deleting, setDeleting] = useState(false);
  const [deleteTag, setDeleteTag] = useState(false);
  const del = useDeleteReposRelease(repo.id);
  const save = useSaveReposRelease(repo.id);
  const when = r.published_at ?? r.created_at;
  return (
    <Card className="flex flex-col gap-4" aria-labelledby={`release-${r.id}`}>
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <h3 id={`release-${r.id}`} className="flex min-w-0 flex-wrap items-center gap-2 type-title-sm text-fg">
            <span className="min-w-0 [overflow-wrap:anywhere]">{r.name || r.tag}</span>
            {r.latest ? <Badge tone="success">Latest</Badge> : null}
            {r.prerelease ? <Badge tone="warning">Pre-release</Badge> : null}
            {r.draft ? <Badge tone="neutral">Draft</Badge> : null}
          </h3>
          <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 type-label text-fg-muted">
            <Link href={repoHref(repo.id, "", { ref: r.tag })} className="flex items-center gap-1 font-mono text-fg hover:underline">
              <Tag className="size-4 text-fg-muted" aria-hidden />
              {r.tag}
            </Link>
            <Link href={repoHref(repo.id, `commits/${r.target_sha}`)} className="font-mono hover:underline">
              {reposShortSha(r.target_sha)}
            </Link>
            <span className="flex min-w-0 items-center gap-1">
              <Person user={r.author} />
              <Tooltip content={new Date(when).toLocaleString()}>
                <span tabIndex={0} className="rounded-xs focus-visible:outline-2 focus-visible:outline-ring">
                  {r.draft ? "drafted" : "released"} {reposRelativeTime(when)}
                </span>
              </Tooltip>
            </span>
          </div>
        </div>
        {repo.can_push ? (
          <div className="flex shrink-0 items-center gap-1">
            {r.draft ? (
              <Button
                size="sm"
                leftIcon={<Rocket />}
                loading={save.isPending}
                onClick={async () => {
                  try {
                    await save.mutateAsync({ releaseId: r.id, body: { draft: false } });
                    toast.success(`Published ${r.name || r.tag}`);
                  } catch (e) {
                    toast.error(errorMessage(e, "Couldn't publish it."));
                  }
                }}
              >
                Publish
              </Button>
            ) : null}
            <Menu trigger={<IconButton label={`More for ${r.name || r.tag}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
              <MenuItem icon={<Pencil />} onSelect={onEdit}>
                Edit
              </MenuItem>
              <MenuSeparator />
              <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                Delete release
              </MenuItem>
            </Menu>
          </div>
        ) : null}
      </div>
      {r.body ? (
        <RepoMarkdown source={r.body} repoId={repo.id} gitRef={r.tag} dir="" />
      ) : (
        <p className="type-label text-fg-muted">No notes.</p>
      )}
      <AssetList repo={repo} release={r} />
      <ConfirmDialog
        open={deleting}
        onOpenChange={(o) => {
          setDeleting(o);
          if (!o) setDeleteTag(false);
        }}
        title={`Delete ${r.name || r.tag}?`}
        tone="danger"
        confirmLabel="Delete release"
        onConfirm={async () => {
          await del.mutateAsync({ releaseId: r.id, deleteTag: deleteTag && !r.draft });
          toast.success("Release deleted");
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        <div className="flex flex-col gap-3">
          <p>Its notes and {r.assets.length === 1 ? "file go" : `${r.assets.length} files go`} for good.</p>
          {!r.draft ? <Checkbox checked={deleteTag} onCheckedChange={setDeleteTag} label={`Also delete the tag ${r.tag}`} info="The commit stays; only the tag that names it goes." /> : null}
        </div>
      </ConfirmDialog>
    </Card>
  );
}

function AssetList({ repo, release: r }: { repo: ReposRepo; release: ReposRelease }) {
  const download = useDownloadReposReleaseAsset(repo.id);
  const remove = useDeleteReposReleaseAsset(repo.id, r.id);
  const add = useAddReposReleaseAsset(repo.id, r.id);
  const upload = useUpload();
  const git = useReposGitInfo();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [removing, setRemoving] = useState<ReposReleaseAsset | null>(null);

  const open = async (a: ReposReleaseAsset) => {
    try {
      const { url } = await download.mutateAsync({ releaseId: r.id, assetId: a.id });
      window.location.assign(url);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't download it."));
    }
  };
  const addFiles = async (files: FileList | null) => {
    if (!files) return;
    for (const f of Array.from(files)) {
      setBusy(f.name);
      try {
        const up = await upload.mutateAsync({ file: f, purpose: "repos.release_asset", maxBytes: git.data?.max_push_bytes });
        await add.mutateAsync({ key: up.key, name: f.name });
      } catch (e) {
        toast.error(errorMessage(e, `Couldn't add ${f.name}.`));
      }
    }
    setBusy(null);
    if (input.current) input.current.value = "";
  };

  if (r.assets.length === 0 && !repo.can_push) return null;
  return (
    <section aria-label="Files" className="flex flex-col gap-2 border-t border-border pt-4">
      <div className="flex items-center gap-2">
        <h4 className="flex-1 type-label font-semibold text-fg">
          Files <span className="font-normal text-fg-muted tabular-nums">{r.assets.length}</span>
        </h4>
        {repo.can_push ? (
          <>
            <input ref={input} type="file" multiple className="sr-only" tabIndex={-1} aria-hidden onChange={(e) => void addFiles(e.target.files)} />
            <Button size="sm" variant="outline" leftIcon={<Paperclip />} loading={!!busy} onClick={() => input.current?.click()}>
              {busy ? `Adding ${busy}` : "Add files"}
            </Button>
          </>
        ) : null}
      </div>
      {r.assets.length > 0 ? (
        <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
          {r.assets.map((a) => (
            <li key={a.id} className="flex min-h-tap-sm flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
              <FileArchive className="size-4 shrink-0 text-fg-muted" aria-hidden />
              <button
                type="button"
                className="min-w-0 flex-1 truncate text-left type-label font-semibold text-primary-strong hover:underline focus-visible:outline-2 focus-visible:outline-ring"
                onClick={() => void open(a)}
              >
                {a.name}
              </button>
              <span className="type-caption text-fg-muted tabular-nums">{reposFormatBytes(a.size_bytes)}</span>
              <span className="type-caption text-fg-muted tabular-nums">
                {a.download_count} {a.download_count === 1 ? "download" : "downloads"}
              </span>
              <IconButton label={`Download ${a.name}`} icon={<Download />} variant="ghost" size="sm" tooltip onClick={() => void open(a)} />
              {repo.can_push ? <IconButton label={`Remove ${a.name}`} icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setRemoving(a)} /> : null}
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-caption text-fg-muted">No files.</p>
      )}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.name ?? "this file"}?`}
        tone="danger"
        confirmLabel="Remove file"
        onConfirm={() => (removing ? remove.mutateAsync(removing.id) : undefined)}
        errorText={(e) => errorMessage(e, "Couldn't remove it.")}
      >
        Download links to it stop working.
      </ConfirmDialog>
    </section>
  );
}

/** New release, or edit one (a published release keeps its tag). */
function ReleaseDialog({ repo, release, previousTag, onClose }: { repo: ReposRepo; release: ReposRelease | null; previousTag: string | undefined; onClose: () => void }) {
  const refs = useReposRefs(repo.id);
  const save = useSaveReposRelease(repo.id);
  const notes = useReposReleaseNotes(repo.id);
  const [tag, setTag] = useState(release?.tag ?? "");
  const [target, setTarget] = useState(repo.default_branch);
  const [name, setName] = useState(release?.name ?? "");
  const [body, setBody] = useState(release?.body ?? "");
  const [pre, setPre] = useState(release?.prerelease ?? false);
  const all = refs.data?.items ?? [];
  const tags = useMemo(() => all.filter((r) => r.kind === "tag"), [all]);
  const branches = all.filter((r) => r.kind === "branch");
  const existing = tags.find((t) => t.name === tag.trim());
  const [previous, setPrevious] = useState(previousTag && previousTag !== release?.tag ? previousTag : "");
  const editing = !!release;

  const submit = async (draft: boolean) => {
    try {
      if (release) {
        await save.mutateAsync({ releaseId: release.id, body: { name: name.trim(), body, prerelease: pre, ...(release.draft && !draft ? { draft: false } : {}) } });
        toast.success(release.draft && !draft ? `Published ${name.trim() || release.tag}` : "Release saved");
      } else {
        await save.mutateAsync({ body: { tag: tag.trim(), target: existing ? undefined : target, name: name.trim(), body, draft, prerelease: pre } });
        toast.success(draft ? "Draft saved" : `Released ${name.trim() || tag.trim()}`);
      }
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the release."));
    }
  };
  const write = async () => {
    try {
      const res = await notes.mutateAsync({ target: existing ? tag.trim() : release?.tag ?? target, ...(previous ? { previous } : {}) });
      if (!res.body) {
        toast.info("Nothing merged since the previous release.");
        return;
      }
      setBody((b) => (b.trim() ? `${b.trimEnd()}\n\n${res.body}` : res.body));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't write the notes."));
    }
  };
  const canSave = editing || !!tag.trim();
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={release ? `Edit ${release.name || release.tag}` : "New release"}
      size="lg"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          {!release || release.draft ? (
            <Button variant="outline" loading={save.isPending} disabled={!canSave} onClick={() => void submit(true)}>
              Save draft
            </Button>
          ) : null}
          <Button loading={save.isPending} disabled={!canSave} onClick={() => void submit(false)}>
            {release && !release.draft ? "Save" : "Publish release"}
          </Button>
        </>
      }
    >
      <form className="flex flex-col gap-4" onSubmit={(e) => e.preventDefault()}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Tag"
            info={editing ? "A release keeps its tag." : existing ? `Uses the existing tag at ${reposShortSha(existing.sha)}.` : "A new tag is made at the target when you publish."}
          >
            <Input value={tag} onChange={(e) => setTag(e.target.value)} placeholder="v1.0.0" className="font-mono" list="repos-release-tags" disabled={editing} autoFocus={!editing} />
          </Field>
          <datalist id="repos-release-tags">
            {tags.map((t) => (
              <option key={t.name} value={t.name} />
            ))}
          </datalist>
          <Field label="Target" info="The branch the new tag points at. An existing tag keeps its commit.">
            <Select
              value={target}
              onValueChange={setTarget}
              options={branches.map((b) => ({ value: b.name, label: b.name }))}
              disabled={editing || !!existing || branches.length === 0}
            />
          </Field>
        </div>
        <Field label="Title">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={tag.trim() || release?.tag || "v1.0.0"} maxLength={200} />
        </Field>
        <Field label="Notes" info="Markdown. #12 links an issue or pull request.">
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} minRows={8} />
        </Field>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Since" className="min-w-0 flex-1" info="The previous release's tag: the notes list pull requests merged after it.">
            <Select value={previous || "_none"} onValueChange={(v) => setPrevious(v === "_none" ? "" : v)} options={[{ value: "_none", label: "The beginning" }, ...tags.filter((t) => t.name !== tag.trim()).map((t) => ({ value: t.name, label: t.name }))]} />
          </Field>
          <Button variant="outline" leftIcon={<Sparkles />} loading={notes.isPending} onClick={() => void write()}>
            Write from merged pull requests
          </Button>
        </div>
        <Switch checked={pre} onCheckedChange={setPre} label="Pre-release" info="Not ready for everyone; it never shows as Latest." />
      </form>
    </Dialog>
  );
}
