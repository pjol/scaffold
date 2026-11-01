"use client";

import { Button, Spinner } from "@scaffoldapp/ui-web";
import { useEffect, useRef } from "react";

/** Keyset pagination footer: loads the next page when scrolled into view, with a button as fallback. */
export function LoadMore({ hasNextPage, isFetchingNextPage, fetchNextPage, label }: { hasNextPage: boolean; isFetchingNextPage: boolean; fetchNextPage: () => unknown; label?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const fetchRef = useRef(fetchNextPage);
  fetchRef.current = fetchNextPage;
  useEffect(() => {
    const el = ref.current;
    if (!el || !hasNextPage || typeof IntersectionObserver === "undefined") return undefined;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) void fetchRef.current();
    }, { rootMargin: "240px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage]);
  if (!hasNextPage && !isFetchingNextPage) return label ? <p className="py-3 text-center type-caption text-fg-muted">{label}</p> : null;
  return (
    <div ref={ref} className="flex min-h-row items-center justify-center">
      {isFetchingNextPage ? (
        <Spinner size={18} label="Loading more" />
      ) : (
        <Button variant="ghost" size="sm" onClick={() => void fetchNextPage()}>
          Load more
        </Button>
      )}
    </div>
  );
}
