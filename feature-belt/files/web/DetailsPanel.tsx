"use client";

import {
  errorMessage,
  filesCanEdit,
  filesFormatSize,
  filesKindLabel,
  orgsNoun,
  useAccessResource,
  useFilesActivity,
  useFilesGetUrl,
  useFilesItem,
  useFilesRestoreVersion,
  useFilesVersions,
  type FilesActivity,
  type FilesItem,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Sheet, SkeletonGroup, SkeletonRow, Tab, TabPanel, Tabs, TabsList, toast } from "@scaffoldapp/ui-web";
import { Download, Globe, Lock, RotateCcw, Upload, Users } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ShareButton } from "@/features/access";
import { formatDate, formatDateTime, formatRelative } from "@/lib/format";
import { startDownload, useFilesActions } from "./actions";
import { filesIconTone, filesItemIcon } from "./icons";

/** The details sheet for the item `useFilesActions().details` opened (side panel ≥ 768 px, bottom sheet on phones). */
export function FilesDetailsSheet() {
  const a = useFilesActions();
  const detail = useFilesItem(a.detailsId);
  const item = detail.data?.item;
  return (
    <Sheet open={a.detailsId !== null} onOpenChange={(o) => !o && a.closeDetails()} title={item?.name ?? "Details"} size="md">
      {detail.isPending ? (
        <SkeletonGroup label="Loading details" className="flex flex-col gap-2">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : detail.isError ? (
        <ErrorState compact title="Couldn't load the details" info={errorMessage(detail.error, "")} onRetry={() => void detail.refetch()} />
      ) : item ? (
        <FilesDetails item={item} path={detail.data.path.map((c) => c.name)} />
      ) : null}
    </Sheet>
  );
}

/** Details, versions and activity of one item (the sheet's body; the file page's side column). */
export function FilesDetails({ item, path }: { item: FilesItem; path: string[] }) {
  const [tab, setTab] = useState("details");
  return (
    <Tabs value={tab} onValueChange={setTab}>
      <TabsList aria-label="Item information">
        <Tab value="details">Details</Tab>
        {item.type === "file" ? (
          <Tab value="versions" count={item.version_count > 1 ? item.version_count : undefined}>
            Versions
          </Tab>
        ) : null}
        <Tab value="activity">Activity</Tab>
      </TabsList>
      <TabPanel value="details" className="pt-6">
        <DetailsTab item={item} path={path} />
      </TabPanel>
      {item.type === "file" ? (
        <TabPanel value="versions" className="pt-6">
          <VersionsTab item={item} />
        </TabPanel>
      ) : null}
      <TabPanel value="activity" className="pt-6">
        <ActivityTab item={item} />
      </TabPanel>
    </Tabs>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="type-caption font-semibold text-fg-muted">{label}</dt>
      <dd className="type-body text-fg">{children}</dd>
    </div>
  );
}

function name(p: FilesItem["created_by"]): string {
  return p ? p.name || "Deleted user" : "Deleted user";
}

function DetailsTab({ item, path }: { item: FilesItem; path: string[] }) {
  const sharing = useAccessResource("files.item", item.id);
  const s = sharing.data;
  const Icon = filesItemIcon(item);
  const location = item.drive === "shared" ? ["Shared folders", ...path] : item.owner_id && path.length === 0 && item.parent_id === null ? ["My drive"] : path;
  return (
    <div className="flex flex-col gap-section">
      <section className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <Icon className={`size-8 ${filesIconTone(item)}`} aria-hidden />
          <Badge>{filesKindLabel(item)}</Badge>
          {item.trashed_at ? <Badge tone="danger">In the trash</Badge> : null}
        </div>
        <dl className="flex flex-col gap-4">
          {item.type === "file" ? <Row label="Size">{filesFormatSize(item.size)}</Row> : null}
          <Row label="Location">{location.length ? location.join(" / ") : "Shared with you"}</Row>
          <Row label="Created">
            {formatDateTime(item.created_at)} by {name(item.created_by)}
          </Row>
          <Row label="Modified">
            {formatDateTime(item.updated_at)} by {name(item.updated_by)}
          </Row>
          {item.type === "file" ? <Row label="Type">{item.mime_type || "Unknown"}</Row> : null}
        </dl>
      </section>
      <section className="flex flex-col gap-3">
        <div className="flex min-h-control items-center gap-3">
          <h3 className="flex-1 type-title-sm text-fg">Who has access</h3>
          {item.trashed_at ? null : <ShareButton kind="files.item" id={item.id} title={item.name} />}
        </div>
        {sharing.isPending ? (
          <SkeletonGroup label="Loading sharing" className="flex flex-col">
            <SkeletonRow />
          </SkeletonGroup>
        ) : sharing.isError ? (
          <ErrorState compact title="Couldn't load sharing" onRetry={() => void sharing.refetch()} />
        ) : s ? (
          <ul className="flex flex-col gap-2 type-body text-fg">
            <li className="flex items-center gap-2">
              <Users className="size-4 shrink-0 text-fg-muted" aria-hidden />
              {s.owner ? `Owner: ${s.owner.name}` : `Owned by the ${orgsNoun()}`}
            </li>
            <li className="flex items-center gap-2">
              {s.general.access === "org" ? <Globe className="size-4 shrink-0 text-fg-muted" aria-hidden /> : <Lock className="size-4 shrink-0 text-fg-muted" aria-hidden />}
              {s.general.access === "org" ? `Everyone in the ${orgsNoun()} can ${s.general.level ?? "view"}` : "Only people with access"}
            </li>
            {s.can_manage && s.grants.length > 0 ? (
              <li className="flex items-center gap-2">
                <Users className="size-4 shrink-0 text-fg-muted" aria-hidden />
                {s.grants.length} {s.grants.length === 1 ? "person or team" : "people and teams"}
              </li>
            ) : null}
            {s.can_manage && s.links.length > 0 ? (
              <li className="flex items-center gap-2">
                <Globe className="size-4 shrink-0 text-fg-muted" aria-hidden />
                {s.links.length === 1 ? "A public link is on" : `${s.links.length} public links are on`}
              </li>
            ) : null}
            {item.public_disabled ? (
              <li>
                <Badge tone="danger">Staff turned public links off</Badge>
              </li>
            ) : null}
            <li className="type-label text-fg-muted">You can {s.my_level}.</li>
          </ul>
        ) : null}
      </section>
    </div>
  );
}

function VersionsTab({ item }: { item: FilesItem }) {
  const versions = useFilesVersions(item.id);
  const restore = useFilesRestoreVersion(item.id);
  const getUrl = useFilesGetUrl();
  const a = useFilesActions();
  const [confirm, setConfirm] = useState<{ id: string; number: number } | null>(null);
  const edit = filesCanEdit(item) && !item.trashed_at;
  return (
    <div className="flex flex-col gap-3">
      {edit ? (
        <Button variant="secondary" size="auto" leftIcon={<Upload />} className="self-start" onClick={() => a.newVersion(item)}>
          Upload new version
        </Button>
      ) : null}
      {versions.isPending ? (
        <SkeletonGroup label="Loading versions" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : versions.isError ? (
        <ErrorState compact title="Couldn't load versions" onRetry={() => void versions.refetch()} />
      ) : (
        <ol className="flex flex-col">
          {versions.data.items.map((v) => (
            <li key={v.id} className="flex min-h-tap-lg items-center gap-3 border-b border-border py-2 last:border-b-0">
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex items-center gap-2 type-body font-medium text-fg">
                  Version {v.number}
                  {v.current ? <Badge tone="primary">Current</Badge> : null}
                </span>
                <span className="truncate type-caption text-fg-muted" title={formatDateTime(v.created_at)}>
                  {[formatRelative(v.created_at), name(v.uploaded_by), filesFormatSize(v.size), v.note].filter(Boolean).join(" · ")}
                </span>
              </div>
              <IconButton
                label={`Download version ${v.number}`}
                icon={<Download />}
                variant="ghost"
                size="sm"
                tooltip
                onClick={() =>
                  getUrl.mutate({ id: item.id, version: v.id, disposition: "attachment" }, { onSuccess: (u) => startDownload(u.url), onError: (e) => toast.error(errorMessage(e, "Couldn't start the download.")) })
                }
              />
              {edit && !v.current ? (
                <IconButton label={`Restore version ${v.number}`} icon={<RotateCcw />} variant="ghost" size="sm" tooltip onClick={() => setConfirm({ id: v.id, number: v.number })} />
              ) : null}
            </li>
          ))}
        </ol>
      )}
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Restore version ${confirm?.number ?? ""}?`}
        confirmLabel="Restore"
        onConfirm={async () => {
          if (!confirm) return;
          await restore.mutateAsync(confirm.id);
          toast.success(`Version ${confirm.number} is current again`);
          setConfirm(null);
        }}
      >
        <p className="type-body text-fg-muted">A copy of it becomes the newest version. Every other version stays.</p>
      </ConfirmDialog>
    </div>
  );
}

const LEVELS = ["", "view", "comment", "edit", "manage"];

/** One activity line, meaning first. */
export function activityText(a: FilesActivity, self: string): string {
  const who = a.actor ? a.actor.name || "Deleted user" : "Deleted user";
  const by = typeof a.data["agent"] === "string" ? ` (with ${a.data["agent"]})` : "";
  const what = a.item_id === self ? "" : ` “${a.item_name}”`;
  switch (a.action) {
    case "created":
      return `${who} created${what || " it"}${by}`;
    case "uploaded":
      return `${who} uploaded${what || " it"}${by}`;
    case "version":
      return `${who} uploaded a new version${what ? ` of${what}` : ""}${by}`;
    case "version_restored":
      return `${who} restored version ${String(a.data["number"] ?? "")}${what ? ` of${what}` : ""}${by}`;
    case "edited":
      return `${who} edited${what || ""} lines ${String(a.data["start_line"] ?? "")}–${String(a.data["end_line"] ?? "")}${by}`;
    case "renamed":
      return `${who} renamed “${String(a.data["from"] ?? "")}” to “${String(a.data["to"] ?? "")}”${by}`;
    case "moved":
      return `${who} moved${what || " it"} to ${typeof a.data["to_name"] === "string" ? `“${a.data["to_name"]}”` : "My drive"}${by}`;
    case "copied":
      return `${who} made${what || " this"} as a copy of “${String(a.data["from_name"] ?? "")}”${by}`;
    case "trashed":
      return `${who} moved${what || " it"} to the trash${by}`;
    case "restored":
      return `${who} restored${what || " it"} from the trash${by}`;
    case "shared":
      return `${who} shared${what || " it"} with ${String(a.data["subject_name"] ?? "")} (${LEVELS[Number(a.data["level"])] ?? "view"})`;
    case "link_created":
      return `${who} made a public link${a.data["revoked"] ? " (now revoked)" : ""}`;
  }
  return `${who} changed it`;
}

function ActivityTab({ item }: { item: FilesItem }) {
  const activity = useFilesActivity(item.id);
  const rows = activity.data?.items ?? [];
  if (activity.isPending) {
    return (
      <SkeletonGroup label="Loading activity" className="flex flex-col">
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    );
  }
  if (activity.isError) return <ErrorState compact title="Couldn't load activity" onRetry={() => void activity.refetch()} />;
  if (rows.length === 0) return <EmptyState compact title="No activity yet" />;
  return (
    <div className="flex flex-col">
      <ol className="flex flex-col gap-4">
        {rows.map((a) => (
          <li key={a.id} className="flex flex-col gap-1">
            <span className="type-body text-fg">
              {a.item_id !== item.id ? (
                <Link href={`/files/${a.item_id}`} className="hover:underline">
                  {activityText(a, item.id)}
                </Link>
              ) : (
                activityText(a, item.id)
              )}
            </span>
            <time className="type-caption text-fg-muted" dateTime={a.created_at} title={formatDate(a.created_at)}>
              {formatRelative(a.created_at)}
            </time>
          </li>
        ))}
      </ol>
      <LoadMore hasNextPage={!!activity.hasNextPage} isFetchingNextPage={activity.isFetchingNextPage} fetchNextPage={activity.fetchNextPage} />
    </div>
  );
}
