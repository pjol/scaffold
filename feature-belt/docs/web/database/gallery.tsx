"use client";

import { docsGroupRowsBy, docsTimeAgo, type DocsRow, type DocsValue } from "@scaffoldapp/shared";
import { Avatar, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Plus } from "lucide-react";
import { CoverArt } from "../Dialogs";
import { CardImage, CardProps, CardTitle, firstImage } from "./cards";
import { GroupHeader, HiddenGroups, LoadMoreRow, NewRowButton, useGroupState } from "./cells";
import { groupValue, visibleProps } from "./model";
import type { ViewProps } from "./types";

const GRID: Record<"small" | "medium" | "large", string> = {
  small: "grid-cols-2 sm:grid-cols-3 lg:grid-cols-5",
  medium: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
  large: "grid-cols-1 lg:grid-cols-2",
};

const PREVIEW_H: Record<"small" | "medium" | "large", string> = { small: "h-24", medium: "h-36", large: "h-52" };

/** A card's preview: the page cover, the start of its content, or a files property's first image. */
function Preview({ row, props }: { row: DocsRow; props: ViewProps }) {
  const card = props.config.card;
  const size = card?.size ?? "medium";
  const preview = card?.preview ?? "none";
  const h = PREVIEW_H[size];
  if (preview === "none") return null;
  if (preview === "cover") {
    return row.cover ? <CoverArt cover={row.cover} token={props.token ?? null} className={cn(h, "border-b border-border")} /> : <div className={cn(h, "border-b border-border bg-sunken")} />;
  }
  if (preview === "content") {
    return (
      <div className={cn(h, "overflow-hidden border-b border-border bg-sunken/50 px-3 py-2")}>
        {row.snippet ? <p className="line-clamp-5 type-caption text-fg-muted">{row.snippet}</p> : null}
      </div>
    );
  }
  const img = firstImage(row, preview);
  return img ? <CardImage file={img} fit={card?.fit} className={cn(h, "border-b border-border")} token={props.token} /> : <div className={cn(h, "border-b border-border bg-sunken")} />;
}

function Cards({ rows, props, values }: { rows: DocsRow[]; props: ViewProps; values?: Record<string, DocsValue> | undefined }) {
  const size = props.config.card?.size ?? "medium";
  const shown = visibleProps(props.schema, props.config, "gallery");
  return (
    <div className={cn("grid gap-3", GRID[size])}>
      {rows.map((row) => (
        <article key={row.id} className="flex min-w-0 flex-col overflow-hidden rounded-md border border-border bg-surface">
          <Preview row={row} props={props} />
          <div className="flex flex-col gap-1 p-2">
            <CardTitle row={row} props={props} />
            <CardProps row={row} props={props} shown={shown} />
          </div>
        </article>
      ))}
      {props.editable ? (
        <button
          type="button"
          onClick={() => props.onCreateRow(values)}
          className="flex min-h-24 items-center justify-center gap-2 rounded-md border border-dashed border-border type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring"
        >
          <Plus aria-hidden className="size-4" />
          New
        </button>
      ) : null}
    </div>
  );
}

export function GalleryView(props: ViewProps) {
  const groupProp = props.schema.find((p) => p.id === props.config.group_by);
  const groups = groupProp ? props.meta?.groups : undefined;
  const gs = useGroupState(props);
  if (props.loading && props.rows.length === 0) {
    return (
      <div className={cn("grid gap-3", GRID[props.config.card?.size ?? "medium"])}>
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className="h-40 w-full rounded-md" />
        ))}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {groups ? (
        docsGroupRowsBy(props.rows, groups)
          .filter((s) => !gs.isHidden(s.group) && !(s.group.key === "" && s.group.count === 0))
          .map(({ group, rows }) => {
            const preset = groupValue(groupProp, group.key);
            return (
              <section key={group.key} aria-label={group.label} className="flex flex-col gap-2">
                <GroupHeader group={group} prop={groupProp} props={props} collapsed={gs.isCollapsed(group.key)} onToggle={() => gs.toggle(group.key)} onHide={props.editable ? () => gs.hide(group.key) : undefined} />
                {gs.isCollapsed(group.key) ? null : <Cards rows={rows} props={props} values={preset !== undefined && groupProp ? { [groupProp.id]: preset } : undefined} />}
              </section>
            );
          })
      ) : props.rows.length || props.editable ? (
        <Cards rows={props.rows} props={props} />
      ) : null}
      {!props.loading && props.rows.length === 0 ? <p className="px-1 type-label text-fg-muted">No rows match this view.</p> : null}
      {groups ? <HiddenGroups groups={groups.filter((g) => gs.isHidden(g))} prop={groupProp} props={props} onShow={props.editable ? gs.show : undefined} /> : null}
      <LoadMoreRow props={props} />
    </div>
  );
}

/** Rows as a stream of cards: who added each and when, the start of its content and its properties. */
export function FeedView(props: ViewProps) {
  const shown = visibleProps(props.schema, props.config, "feed");
  if (props.loading && props.rows.length === 0) {
    return (
      <div className="flex flex-col gap-3">
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className="h-28 w-full rounded-md" />
        ))}
      </div>
    );
  }
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-3">
      {props.rows.map((row) => {
        const by = row.created_by ? props.refs.users[row.created_by] : undefined;
        return (
          <article key={row.id} className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3">
            <div className="flex items-center gap-2 type-caption text-fg-muted">
              {by ? <Avatar size="xs" name={by.name} {...(by.avatar_url ? { src: by.avatar_url } : {})} /> : null}
              <span className="truncate">{by?.name ?? "Someone"}</span>
              <span aria-hidden>·</span>
              <time dateTime={row.created_at} title={new Date(row.created_at).toLocaleString()}>
                {docsTimeAgo(row.created_at)}
              </time>
            </div>
            {row.cover ? <CoverArt cover={row.cover} token={props.token ?? null} className="h-32 rounded-sm" /> : null}
            <CardTitle row={row} props={props} className="type-title-sm" />
            {row.snippet ? <p className="line-clamp-4 whitespace-pre-line type-body text-fg-muted">{row.snippet}</p> : null}
            <CardProps row={row} props={props} shown={shown} />
          </article>
        );
      })}
      {!props.loading && props.rows.length === 0 ? <p className="px-1 type-label text-fg-muted">No rows match this view.</p> : null}
      <LoadMoreRow props={props} />
      <NewRowButton props={props} />
    </div>
  );
}
