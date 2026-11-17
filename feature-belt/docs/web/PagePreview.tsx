"use client";

import { docsTimeAgo, docsTitle, useDocsPagePreview } from "@scaffoldapp/shared";
import { Skeleton, useHoverCapable } from "@scaffoldapp/ui-web";
import { Lock } from "lucide-react";
import { useEffect, useRef, useState, type RefObject } from "react";
import { CoverArt } from "./Dialogs";
import { PageIcon } from "./PageIcon";

const OPEN_DELAY = 450;
const CLOSE_DELAY = 200;

function idOf(el: Element): string | null {
  const direct = el.getAttribute("data-page-id");
  if (direct) return direct;
  const href = el.getAttribute("href") ?? "";
  return /^\/docs\/([0-9a-f-]{36})(?:[?#].*)?$/.exec(href)?.[1] ?? null;
}

function Card({ pageId }: { pageId: string }) {
  const q = useDocsPagePreview(pageId);
  if (q.isPending) {
    return (
      <div className="flex flex-col gap-2 p-3">
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-5/6" />
      </div>
    );
  }
  if (q.isError) {
    return (
      <p className="flex items-center gap-2 p-3 type-label text-fg-muted">
        <Lock aria-hidden className="size-4" />
        You can&apos;t open this page.
      </p>
    );
  }
  const d = q.data;
  const p = d.page;
  const lines: string[] = [];
  for (const b of d.blocks) {
    if (lines.length >= 4) break;
    const t = (b.props.text ?? []).map((s) => s.text).join("").trim();
    if (t && !b.parent_id) lines.push(t);
  }
  return (
    <div className="flex flex-col">
      {p.cover ? <CoverArt cover={p.cover} className="h-14" /> : null}
      <div className="flex flex-col gap-2 p-3">
        {d.path.length ? <p className="truncate type-caption text-fg-muted">{d.path.map((c) => c.title).join(" / ")}</p> : null}
        <p className="flex items-center gap-2 type-label font-semibold text-fg">
          <PageIcon icon={p.icon} custom={p.custom_icon} kind={p.kind} size="xs" />
          <span className="truncate">{docsTitle(p.title)}</span>
        </p>
        {lines.length ? (
          <div className="flex flex-col gap-1">
            {lines.map((l, i) => (
              <p key={i} className="line-clamp-2 type-caption text-fg-muted">
                {l}
              </p>
            ))}
          </div>
        ) : (
          <p className="type-caption text-fg-subtle">{p.kind === "database" ? "A database" : "Empty page"}</p>
        )}
        <p className="type-caption text-fg-subtle">Edited {docsTimeAgo(p.updated_at)}</p>
      </div>
    </div>
  );
}

/**
 * Hover cards for page links inside `container`: page mention chips and page links
 * (`[data-page-id]`, `a[href="/docs/<id>"]`). Pointer devices only; the page is read without
 * counting a visit.
 */
export function PageHoverPreviews({ container }: { container: RefObject<HTMLElement | null> }) {
  const hover = useHoverCapable();
  const [state, setState] = useState<{ id: string; x: number; y: number } | null>(null);
  const openTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const root = container.current;
    if (!root || !hover) return undefined;
    const clear = () => {
      if (openTimer.current) clearTimeout(openTimer.current);
      openTimer.current = null;
    };
    const onOver = (e: MouseEvent) => {
      const el = (e.target as Element).closest?.("[data-page-id], a[href^='/docs/']");
      if (!el || !root.contains(el)) return;
      const id = idOf(el);
      if (!id) return;
      if (closeTimer.current) clearTimeout(closeTimer.current);
      clear();
      openTimer.current = setTimeout(() => {
        const r = el.getBoundingClientRect();
        setState({ id, x: Math.min(r.left, window.innerWidth - 336), y: r.bottom + 6 });
      }, OPEN_DELAY);
    };
    const onOut = (e: MouseEvent) => {
      const el = (e.target as Element).closest?.("[data-page-id], a[href^='/docs/']");
      if (!el) return;
      clear();
      closeTimer.current = setTimeout(() => setState(null), CLOSE_DELAY);
    };
    const onScroll = () => setState(null);
    root.addEventListener("mouseover", onOver);
    root.addEventListener("mouseout", onOut);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      clear();
      root.removeEventListener("mouseover", onOver);
      root.removeEventListener("mouseout", onOut);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [container, hover]);
  if (!state) return null;
  return (
    <div
      role="tooltip"
      className="fixed z-50 w-80 overflow-hidden rounded-md border border-border bg-surface-raised shadow-lg"
      style={{ left: Math.max(8, state.x), top: Math.min(state.y, window.innerHeight - 220) }}
      onMouseEnter={() => closeTimer.current && clearTimeout(closeTimer.current)}
      onMouseLeave={() => setState(null)}
    >
      <Card pageId={state.id} />
    </div>
  );
}
