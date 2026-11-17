"use client";

import { catalogMonogram, formatCatalogPrice, type CatalogItem, type CatalogListItem } from "@scaffoldapp/shared";
import { Badge, ListGroup, ListRow, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import Image from "next/image";
import Link from "next/link";
import { isOptimisable } from "@/lib/images";
import { catalogPaths } from "./nav";

/** An item's picture, or its initials on a tile. Source images may come from any host (next.config.ts lists the ones next/image optimises). */
export function CatalogThumb({ item, size = "md" }: { item: Pick<CatalogListItem, "name" | "image_url">; size?: "md" | "lg" }) {
  const px = size === "lg" ? 96 : 40;
  const box = size === "lg" ? "size-24 rounded-lg" : "size-10 rounded-md";
  if (item.image_url) {
    return (
      <Image
        src={item.image_url}
        alt=""
        width={px}
        height={px}
        unoptimized={!isOptimisable(item.image_url)}
        className={cn(box, "shrink-0 border border-border bg-sunken object-contain")}
      />
    );
  }
  return (
    <span aria-hidden className={cn(box, "flex shrink-0 items-center justify-center bg-sunken font-semibold text-fg-muted", size === "lg" ? "type-title" : "type-label")}>
      {catalogMonogram(item.name)}
    </span>
  );
}

/** "Retired" with who retired it. */
export function CatalogRetiredBadge({ item }: { item: Pick<CatalogItem, "retired"> & Partial<Pick<CatalogItem, "retired_by">> }) {
  if (!item.retired) return null;
  return (
    <Badge tone="warning" size="sm">
      {item.retired_by === "staff" ? "Retired by staff" : "Retired"}
    </Badge>
  );
}

function RowMeta({ item }: { item: CatalogListItem }) {
  const price = formatCatalogPrice(item.ref_price, item.currency);
  return (
    <>
      {item.subtitle ? <span className="hidden max-w-56 truncate type-label sm:inline">{item.subtitle}</span> : null}
      {price ? <span className="type-label text-fg tabular-nums">{price}</span> : null}
      <CatalogRetiredBadge item={item} />
    </>
  );
}

/** Items as rows that open the item page; thumbnails when any item has a picture. */
export function CatalogItemRows({ items }: { items: CatalogListItem[] }) {
  const thumbs = items.some((i) => i.image_url);
  return (
    <ListGroup>
      {items.map((it) => (
        <ListRow key={it.key} asChild title={it.name} leading={thumbs ? <CatalogThumb item={it} /> : undefined} trailing={<RowMeta item={it} />}>
          <Link href={catalogPaths.item(it.key)} />
        </ListRow>
      ))}
    </ListGroup>
  );
}

export function CatalogRowsSkeleton({ label, rows = 8 }: { label: string; rows?: number }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col gap-3">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-12" />
      ))}
    </SkeletonGroup>
  );
}
