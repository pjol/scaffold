"use client";

import {
  errorMessage,
  filesKindLabel,
  useFilesConnectedPreviewUrl,
  useFilesPreviewUrl,
  useFilesPublicPreviewUrl,
  type FilesItem,
  type FilesPublicItem,
} from "@scaffoldapp/shared";
import { Button, ErrorState, FilePreview, Skeleton, cn } from "@scaffoldapp/ui-web";
import { ExternalLink } from "lucide-react";
import Link from "next/link";
import { filesIconTone, filesItemIcon } from "./icons";

/** Where the preview reads from: the signed-in API (optionally an old version), a public link, or a connected platform (streamed through the API with your connection). */
export type FilesPreviewSource =
  | { kind: "app"; version?: string | null }
  | { kind: "public"; token: string; proof?: string | undefined }
  | { kind: "connected"; connectionId: string; ref: string; orgId?: string | null };

type PreviewItem = Pick<FilesItem | FilesPublicItem, "id" | "name" | "preview" | "size" | "mime_type" | "type" | "type_info">;

/**
 * Shows a drive item with the core `FilePreview` from a short-lived inline link (presigned,
 * public or streamed): the link's serving rules keep anything that can run script an
 * attachment, and the kit draws text kinds itself. Registered item types get an Open button.
 */
export function FilesPreview({ item, source, onDownload, link, className }: { item: PreviewItem; source: FilesPreviewSource; onDownload?: () => void; link?: string; className?: string }) {
  const file = item.type === "file";
  const app = source.kind === "app";
  const conn = source.kind === "connected" ? source : null;
  const token = source.kind === "public" ? source.token : null;
  const proof = source.kind === "public" ? source.proof : undefined;
  const appUrl = useFilesPreviewUrl(app && file ? item.id : null, app ? (source.version ?? null) : null, app && file);
  const pubUrl = useFilesPublicPreviewUrl(token, token && file ? item.id : null, !!token && file, proof);
  const connUrl = useFilesConnectedPreviewUrl(conn?.connectionId, conn?.ref, conn?.orgId, !!conn && file);
  const url = app ? appUrl : conn ? connUrl : pubUrl;

  if (!file) {
    const Icon = filesItemIcon(item);
    return (
      <div className="flex flex-col items-center gap-4 rounded-lg border border-border bg-surface px-card py-12 text-center">
        <Icon className={cn("size-12", filesIconTone(item))} aria-hidden />
        <p className="type-title-sm text-fg">{item.name}</p>
        {link ? (
          <Button asChild leftIcon={<ExternalLink />}>
            <Link href={link}>Open {item.type_info?.title.toLowerCase() ?? filesKindLabel(item).toLowerCase()}</Link>
          </Button>
        ) : null}
      </div>
    );
  }
  if (url.isPending) return <Skeleton className="h-96 w-full rounded-lg" />;
  if (url.isError) return <ErrorState title="Couldn't load the preview" info={errorMessage(url.error, "")} onRetry={() => void url.refetch()} />;
  return <FilePreview url={url.data.url} name={item.name} mime={item.mime_type} size={item.size} onDownload={onDownload} className={className} />;
}
