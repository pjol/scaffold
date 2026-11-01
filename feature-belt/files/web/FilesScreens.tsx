"use client";

import {
  errorMessage,
  filesCanEdit,
  filesKindLabel,
  filesSectionLabel,
  hasErrorCode,
  orgsNoun,
  useActiveOrg,
  useFilesBatch,
  useFilesChildren,
  useFilesDrive,
  useFilesEmptyTrash,
  useFilesItem,
  useFilesSection,
  type FilesCrumb,
  type FilesItemDetail,
  type FilesSection,
} from "@scaffoldapp/shared";
import { Banner, Button, ConfirmDialog, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, SkeletonRow, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronRight, Clock, Download, FolderOpen, HardDrive, RotateCcw, Star, Trash, UserPlus, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type DragEvent } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { ShareButton } from "@/features/access";
import { useFilesActions, useFilesItemMenu } from "./actions";
import { FilesDetails } from "./DetailsPanel";
import { DriveListing, useFilesSortParam } from "./DriveListing";
import { FILES_SECTION_PATH, SectionTitle } from "./FilesLayout";
import { FILES_DRAG_TYPE } from "./ItemList";
import { FilesPreview } from "./Preview";

const EMPTY: Record<FilesSection, { title: string; icon: React.ReactNode }> = {
  my: { title: "No files yet", icon: <HardDrive /> },
  shared: { title: "No shared folders yet", icon: <Users /> },
  shared_with_me: { title: "Nothing shared with you yet", icon: <UserPlus /> },
  recent: { title: "Nothing opened yet", icon: <Clock /> },
  starred: { title: "No starred items", icon: <Star /> },
  trash: { title: "The trash is empty", icon: <Trash /> },
};

/** One drive section: My drive, Shared folders, Shared with me, Recent, Starred or Trash. */
export function FilesSectionScreen({ section }: { section: FilesSection }) {
  const { org } = useActiveOrg();
  const sort = useFilesSortParam();
  const sortable = section === "my" || section === "shared";
  const query = useFilesSection(org?.id, section, { ...(sortable ? { sort: sort.by, dir: sort.desc ? "desc" : "asc" } : {}), thumbs: true });
  const drive = useFilesDrive(org?.id);
  const a = useFilesActions();
  if (!org) return <ListingSkeleton />;
  const empty = { ...EMPTY[section] };
  return (
    <DriveListing
      orgId={org.id}
      title={
        <>
          <SectionTitle current={section} />
          {section === "trash" && drive.data ? <InfoTip size="md" content={`Items are deleted for good ${drive.data.trash_days} days after they're trashed.`} /> : null}
          {section === "shared" ? <InfoTip size="md" content={`Folders the ${orgsNoun()} owns, shared with teams or everyone. They stay when people leave.`} /> : null}
        </>
      }
      query={query}
      target={section === "my" ? { parentId: null, canAdd: true } : null}
      sharedRoot={section === "shared"}
      sortable={sortable}
      trash={section === "trash"}
      empty={{
        ...empty,
        ...(section === "shared" && drive.data?.can_create_shared
          ? { action: <Button variant="secondary" onClick={() => a.newFolder(null, true)}>New shared folder</Button> }
          : {}),
      }}
      topicId={section === "shared" ? org.id : null}
      headerActions={section === "trash" && (query.data?.items.length ?? 0) > 0 ? <EmptyTrashButton orgId={org.id} /> : undefined}
    />
  );
}

function EmptyTrashButton({ orgId }: { orgId: string }) {
  const [open, setOpen] = useState(false);
  const empty = useFilesEmptyTrash(orgId);
  return (
    <>
      <Button variant="danger-ghost" size="auto" onClick={() => setOpen(true)}>
        Empty trash
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Empty the trash?"
        tone="danger"
        confirmLabel="Delete forever"
        onConfirm={async () => {
          const { deleted } = await empty.mutateAsync();
          toast.success(deleted === 1 ? "1 item deleted" : `${deleted} items deleted`);
        }}
      >
        <p className="type-body text-fg-muted">Everything in it that you manage is deleted for good, with every version. This can&apos;t be undone.</p>
      </ConfirmDialog>
    </>
  );
}

function ListingSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-10 w-48" />
      <SkeletonGroup label="Loading files" className="flex flex-col">
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    </div>
  );
}

// ---- one item ---------------------------------------------------------------------------

/** /files/[itemID]: a folder's listing, a file's preview, or a registered type's own page. */
export function FilesItemScreen({ itemId }: { itemId: string }) {
  const router = useRouter();
  const detail = useFilesItem(itemId);
  const item = detail.data?.item;
  useEffect(() => {
    if (item && item.type !== "folder" && item.type !== "file" && item.link !== `/files/${item.id}`) router.replace(item.link);
  }, [item, router]);
  if (detail.isPending) return <ListingSkeleton />;
  if (detail.isError) {
    if (hasErrorCode(detail.error, "not_found")) {
      return (
        <EmptyState
          icon={<FolderOpen />}
          title="This item isn't available"
          action={
            <Button asChild variant="secondary">
              <Link href="/files">Go to My drive</Link>
            </Button>
          }
        />
      );
    }
    return <ErrorState title="Couldn't load this item" info={errorMessage(detail.error, "")} onRetry={() => void detail.refetch()} />;
  }
  if (detail.data.item.type === "folder") return <FolderView detail={detail.data} />;
  return <FileView detail={detail.data} />;
}

/** Breadcrumbs: where the path starts, then each folder you can open; crumbs take dropped items. */
function Breadcrumbs({ detail }: { detail: FilesItemDetail }) {
  const batch = useFilesBatch();
  const [over, setOver] = useState<string | null>(null);
  const root = detail.root;
  const crumbs: (FilesCrumb & { href: string; dropTo?: string | null })[] = [
    { id: `root-${root}`, name: filesSectionLabel(root), type: "section", href: FILES_SECTION_PATH[root], ...(root === "my" ? { dropTo: null } : {}) },
    ...detail.path.map((c) => ({ ...c, href: `/files/${c.id}`, dropTo: c.id })),
  ];
  const drop = (target: string | null) => ({
    onDragOver: (e: DragEvent) => {
      if (!Array.from(e.dataTransfer.types).includes(FILES_DRAG_TYPE)) return;
      e.preventDefault();
      setOver(target ?? "root");
    },
    onDragLeave: () => setOver(null),
    onDrop: (e: DragEvent) => {
      setOver(null);
      const raw = e.dataTransfer.getData(FILES_DRAG_TYPE);
      if (!raw) return;
      e.preventDefault();
      batch.mutate(
        { action: "move", ids: JSON.parse(raw) as string[], parentId: target },
        { onSuccess: ({ results }) => (results.every((r) => r.ok) ? toast.success("Moved") : toast.error(results.find((r) => !r.ok)?.error?.message ?? "Couldn't move that.")) },
      );
    },
  });
  const shown = crumbs.length > 3 ? [crumbs[0], null, ...crumbs.slice(-2)] : crumbs;
  return (
    <nav aria-label="Breadcrumb">
      <ol className="flex min-w-0 flex-wrap items-center gap-1 type-label text-fg-muted">
        {shown.map((c, i) => (
          <li key={c ? c.id : "gap"} className="flex min-w-0 items-center gap-1">
            {i > 0 ? <ChevronRight className="size-3.5 shrink-0" aria-hidden /> : null}
            {c ? (
              <Link
                href={c.href}
                className={cn("max-w-48 truncate rounded-xs px-1 py-1 hover:text-fg hover:underline", over === (c.dropTo ?? "root") && c.dropTo !== undefined && "bg-primary-soft text-primary-strong")}
                {...(c.dropTo !== undefined ? drop(c.dropTo) : {})}
              >
                {c.name}
              </Link>
            ) : (
              <span aria-label="More folders" className="px-1">
                …
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

function FolderView({ detail }: { detail: FilesItemDetail }) {
  const folder = detail.item;
  const sort = useFilesSortParam();
  const a = useFilesActions();
  const query = useFilesChildren(folder.trashed_at ? null : folder.id, { sort: sort.by, dir: sort.desc ? "desc" : "asc", thumbs: true });
  const actions = useFilesItemMenu(folder, { showOpen: false });
  if (folder.trashed_at) {
    return (
      <div className="flex flex-col gap-4">
        <Breadcrumbs detail={detail} />
        <h1 className="type-title text-fg">{folder.name}</h1>
        <Banner
          tone="warning"
          action={
            filesCanEdit(folder) ? (
              <Button variant="ghost" size="sm" leftIcon={<RotateCcw />} onClick={() => a.restore([folder])}>
                Restore
              </Button>
            ) : undefined
          }
        >
          This folder is in the trash.
        </Banner>
      </div>
    );
  }
  return (
    <DriveListing
      orgId={folder.org_id}
      above={<Breadcrumbs detail={detail} />}
      title={<span className="truncate">{folder.name}</span>}
      query={query}
      target={{ parentId: folder.id, canAdd: filesCanEdit(folder) }}
      sortable
      empty={{ title: "This folder is empty", icon: <FolderOpen /> }}
      topicId={folder.id}
      headerActions={
        <>
          <span className="hidden sm:flex">
            <ShareButton kind="files.item" id={folder.id} title={folder.name} variant="ghost" />
          </span>
          <ResourceMenu feature="files" kind="files.item" id={folder.id} title={folder.name} orgId={folder.org_id} actions={actions} />
        </>
      }
      key={folder.id}
    />
  );
}

function FileView({ detail }: { detail: FilesItemDetail }) {
  const item = detail.item;
  const a = useFilesActions();
  const actions = useFilesItemMenu(item, { showOpen: false, showDetails: false });
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <Breadcrumbs detail={detail} />
        <div className="flex min-h-control-lg items-center gap-2">
          <h1 className="min-w-0 flex-1 truncate type-title text-fg" title={item.name}>
            {item.name}
          </h1>
          {item.trashed_at ? null : (
            <>
              <Button variant="secondary" size="auto" leftIcon={<Download />} onClick={() => a.download([item])}>
                <span className="hidden sm:inline">Download</span>
                <span className="sr-only sm:hidden">Download</span>
              </Button>
              <span className="hidden sm:flex xl:hidden">
                <ShareButton kind="files.item" id={item.id} title={item.name} variant="ghost" />
              </span>
            </>
          )}
          <ResourceMenu feature="files" kind="files.item" id={item.id} title={item.name} orgId={item.org_id} actions={actions} />
        </div>
      </header>
      {item.trashed_at ? (
        <Banner
          tone="warning"
          action={
            filesCanEdit(item) ? (
              <Button variant="ghost" size="sm" leftIcon={<RotateCcw />} onClick={() => a.restore([item])}>
                Restore
              </Button>
            ) : undefined
          }
        >
          This file is in the trash.
        </Banner>
      ) : null}
      <div className="flex flex-col gap-section xl:grid xl:grid-cols-[minmax(0,1fr)_20rem] xl:gap-8">
        <div className="min-w-0" aria-label={`${filesKindLabel(item)} preview`}>
          <FilesPreview item={item} source={{ kind: "app" }} onDownload={() => a.download([item])} link={item.link} />
        </div>
        <aside className="min-w-0" aria-label="Details">
          <FilesDetails item={item} path={detail.path.map((c) => c.name)} />
        </aside>
      </div>
    </div>
  );
}
