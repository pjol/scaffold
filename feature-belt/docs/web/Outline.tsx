"use client";

import { docsOutline, type DocsBlock } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";

/**
 * A page's headings as a floating outline beside the content (wide screens): the heading in view is
 * marked, a click scrolls to it. Renders nothing with fewer than two headings.
 */
export function Outline({ blocks, scrollRoot, className }: { blocks: readonly DocsBlock[]; scrollRoot?: string; className?: string }) {
  const items = docsOutline(blocks);
  const [active, setActive] = useState<string | null>(null);
  const ids = items.map((i) => i.id).join(",");
  useEffect(() => {
    if (!ids) return undefined;
    const root = scrollRoot ? document.getElementById(scrollRoot) : null;
    const seen = new Map<string, boolean>();
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) seen.set((e.target as HTMLElement).dataset["blockId"] ?? "", e.isIntersecting);
        const first = ids.split(",").find((id) => seen.get(id));
        if (first) setActive(first);
      },
      { root, rootMargin: "0px 0px -60% 0px" },
    );
    for (const id of ids.split(",")) {
      const el = document.querySelector(`[data-block-id="${id}"]`);
      if (el) obs.observe(el);
    }
    return () => obs.disconnect();
  }, [ids, scrollRoot]);
  if (items.length < 2) return null;
  const min = Math.min(...items.map((i) => i.level));
  return (
    <nav aria-label="Outline" className={cn("flex max-h-[70vh] w-56 flex-col gap-0.5 overflow-y-auto", className)}>
      <p className="px-2 pb-1 type-caption font-semibold text-fg-muted">On this page</p>
      {items.map((i) => (
        <button
          key={i.id}
          type="button"
          aria-current={active === i.id ? "location" : undefined}
          onClick={() => {
            document.querySelector(`[data-block-id="${i.id}"]`)?.scrollIntoView({ block: "start", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
            setActive(i.id);
          }}
          style={{ paddingLeft: `calc(var(--spacing) * ${2 + (i.level - min) * 3})` }}
          className={cn(
            "truncate rounded-sm py-1 pr-2 text-left type-caption hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring",
            active === i.id ? "font-semibold text-fg" : "text-fg-muted",
          )}
        >
          {i.text}
        </button>
      ))}
    </nav>
  );
}
