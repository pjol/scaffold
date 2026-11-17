"use client";

import { catalogAttrLabel, errorMessage, formatCatalogPrice, useCatalogItem, useMe, useRestoreCatalogItem, useRetireCatalogItem } from "@scaffoldapp/shared";
import { Badge, ConfirmDialog, EmptyState, ErrorState, InfoTip, ListGroup, ListRow, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Archive, ArchiveRestore, ChevronLeft, ScanSearch } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Section } from "@/components/shell/PageHeader";
import type { ResourceMenuAction } from "@/features/registry";
import { formatDate, formatDateTime } from "@/lib/format";
import { CatalogRetiredBadge, CatalogThumb } from "./components";
import { catalogPaths } from "./nav";

function Back() {
  return (
    <Link href={catalogPaths.list} className="inline-flex h-tap-sm items-center gap-1 self-start type-label text-fg-muted hover:text-fg">
      <ChevronLeft className="size-4" aria-hidden />
      Catalog
    </Link>
  );
}

/** /catalog/[key]: one item with its other names, attributes and reference price. Admins retire or restore it from the ⋮ menu. */
export function CatalogItemScreen({ itemKey }: { itemKey: string }) {
  const me = useMe();
  const item = useCatalogItem(itemKey);
  const retire = useRetireCatalogItem();
  const restore = useRestoreCatalogItem();
  const [confirming, setConfirming] = useState<"retire" | "restore" | null>(null);
  const isAdmin = me.data?.admin_role === "admin";

  if (item.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
        <SkeletonGroup label="Loading the item" className="flex flex-col gap-6">
          <Skeleton className="h-6 w-24" />
          <Skeleton className="h-10 w-64" />
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </SkeletonGroup>
      </div>
    );
  }
  if (item.isError) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
        <Back />
        {item.error.status === 404 ? (
          <EmptyState icon={<ScanSearch />} title="No such item" />
        ) : (
          <ErrorState title="Couldn't load this item" info={item.error.message} onRetry={() => void item.refetch()} retrying={item.isFetching} />
        )}
      </div>
    );
  }

  const it = item.data;
  const price = formatCatalogPrice(it.ref_price, it.currency);
  const attrs = Object.entries(it.attrs).sort(([a], [b]) => a.localeCompare(b));
  const actions: ResourceMenuAction[] = [];
  if (isAdmin) {
    actions.push(
      it.retired
        ? { id: "restore", label: "Restore…", icon: ArchiveRestore, onSelect: () => setConfirming("restore") }
        : { id: "retire", label: "Retire…", icon: Archive, destructive: true, onSelect: () => setConfirming("retire") },
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <div className="flex flex-col gap-6">
        <Back />
        <div className="flex items-start gap-4">
          {it.image_url ? <CatalogThumb item={it} size="lg" /> : null}
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <h1 className="type-title text-fg lg:type-display">{it.name}</h1>
            <div className="flex flex-wrap items-center gap-2">
              {it.category ? <Badge size="sm">{it.category}</Badge> : null}
              <CatalogRetiredBadge item={it} />
              {it.subtitle ? <span className="type-label text-fg-muted">{it.subtitle}</span> : null}
            </div>
            {price ? (
              <div className="flex items-center gap-2">
                <span className="type-headline text-fg tabular-nums">{price}</span>
                <InfoTip content="A reference price from the catalog's source, not a live quote." />
              </div>
            ) : null}
          </div>
          <ResourceMenu feature="catalog" kind="catalog.item" id={it.key} title={it.name} actions={actions} />
        </div>
      </div>

      <Section title="Details">
        <ListGroup>
          <ListRow title="Key" trailing={<span className="font-mono type-label">{it.key}</span>} />
          {it.category ? <ListRow title="Category" trailing={it.category} /> : null}
          {it.retired && it.retired_at ? (
            <ListRow
              title="Retired"
              info={it.retired_by === "staff" ? "Retired by staff: it stays out of search until staff restore it." : "The source no longer lists it. It comes back if the source lists it again."}
              trailing={<time dateTime={it.retired_at}>{formatDate(it.retired_at)}</time>}
            />
          ) : null}
          <ListRow title="First seen" trailing={<time dateTime={it.first_seen_at}>{formatDate(it.first_seen_at)}</time>} />
          <ListRow title="Updated" trailing={<time dateTime={it.updated_at}>{formatDateTime(it.updated_at)}</time>} />
        </ListGroup>
      </Section>

      {it.aliases.length > 0 ? (
        <Section title="Also known as" info="Other names this item is found and resolved by.">
          <ul className="flex flex-wrap gap-2">
            {it.aliases.map((a) => (
              <li key={a}>
                <Badge size="md" variant="outline">
                  {a}
                </Badge>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}

      {attrs.length > 0 ? (
        <Section title="Attributes">
          <ListGroup>
            {attrs.map(([k, v]) => (
              <ListRow key={k} title={catalogAttrLabel(k)} trailing={<span className="max-w-64 truncate text-right sm:max-w-md">{v || "—"}</span>} />
            ))}
          </ListGroup>
        </Section>
      ) : null}

      {isAdmin ? (
        <ConfirmDialog
          open={confirming !== null}
          onOpenChange={(open) => !open && setConfirming(null)}
          title={confirming === "retire" ? `Retire ${it.name}?` : `Restore ${it.name}?`}
          confirmLabel={confirming === "retire" ? "Retire" : "Restore"}
          tone={confirming === "retire" ? "danger" : "primary"}
          onConfirm={() =>
            (confirming === "retire" ? retire : restore).mutateAsync(it.key).then(
              () => toast.success(confirming === "retire" ? "Item retired" : "Item restored"),
              (e: unknown) => {
                throw new Error(errorMessage(e, "Couldn't change the item."));
              },
            )
          }
        >
          {confirming === "retire" ? (
            <p className="type-body text-fg">It leaves search and lists, and stays retired through syncs until it is restored. Names still resolve to it.</p>
          ) : it.retired_by === "source" ? (
            <p className="type-body text-fg">The source no longer lists it, so the next sync retires it again.</p>
          ) : (
            <p className="type-body text-fg">It comes back to search and lists.</p>
          )}
        </ConfirmDialog>
      ) : null}
    </div>
  );
}
