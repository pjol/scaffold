"use client";

import { errorMessage, useFilesShortcut, type FilesItemDetail } from "@scaffoldapp/shared";
import { withReturnTo, Button, EmptyState, ErrorState, ProviderIcon, Skeleton } from "@scaffoldapp/ui-web";
import { ExternalLink } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

/**
 * A shortcut (type link) opened from a Files folder: checked live with the viewer's own
 * connection. Opens the connected item when their account can; otherwise says what to do
 * (connect, reconnect, ask for access on the platform). Never uses the pinner's connection.
 */
export function ShortcutView({ detail, header }: { detail: FilesItemDetail; header: ReactNode }) {
  const router = useRouter();
  const item = detail.item;
  const target = useFilesShortcut(item.trashed_at ? null : item.id);
  const t = target.data;
  useEffect(() => {
    if (t?.status === "ok" && t.link) router.replace(t.link);
  }, [t, router]);
  const self = `/files/${item.id}`;
  const icon = t ? <ProviderIcon provider={t.provider} product={t.product} size={40} /> : null;
  let body: ReactNode;
  if (item.trashed_at) body = <EmptyState title="This shortcut is in the trash" />;
  else if (target.isPending || t?.status === "ok") body = <Skeleton className="h-64 w-full rounded-lg" />;
  else if (target.isError) body = <ErrorState title="Couldn't open this shortcut" info={errorMessage(target.error, "")} onRetry={() => void target.refetch()} />;
  else if (t?.status === "not_connected" || t?.status === "needs_reauth")
    body = (
      <EmptyState
        icon={icon}
        title={t.status === "not_connected" ? `Connect ${t.product_label} to open` : `Reconnect ${t.product_label} to open`}
        action={
          t.connect_url ? (
            <Button asChild>
              <Link href={withReturnTo(t.connect_url, self)}>{t.status === "not_connected" ? `Connect ${t.product_label}` : "Reconnect"}</Link>
            </Button>
          ) : undefined
        }
      />
    );
  else if (t?.status === "no_access")
    body = (
      <EmptyState
        icon={icon}
        title={`Ask for access in ${t.product_label}`}
        {...(t.account ? { info: `${t.account} can't open it yet. The owner can share it with you in ${t.product_label}.` } : {})}
        action={
          t.web_url ? (
            <Button asChild rightIcon={<ExternalLink />}>
              <a href={t.web_url} target="_blank" rel="noopener noreferrer">
                Ask for access
              </a>
            </Button>
          ) : undefined
        }
      />
    );
  else body = <EmptyState icon={icon} title={t?.message ?? "This shortcut can't open right now"} action={<Button variant="secondary" onClick={() => void target.refetch()}>Try again</Button>} />;
  return (
    <div className="flex flex-col gap-6">
      {header}
      <div className="rounded-lg border border-border bg-surface px-card py-section">{body}</div>
    </div>
  );
}
