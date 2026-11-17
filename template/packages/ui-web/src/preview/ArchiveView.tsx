"use client";

import { fileFormatSize } from "@scaffoldapp/shared/preview";
import { useMemo, type ReactNode } from "react";
import { Skeleton } from "../components/Skeleton";
import { cn } from "../lib/cn";
import { useLoaded } from "./load";
import { VirtualTable } from "./TextViews";
import { listZip } from "./zip";

/** A zip's contents (names, sizes, dates) read from its central directory. */
export default function ArchiveView({ url, name, size, className, fallback }: { url: string; name: string; size?: number; className?: string; fallback: (reason: string) => ReactNode }) {
  const state = useLoaded(url, (signal) => listZip(url, size, signal));
  const table = useMemo(() => {
    if (state.status !== "ready") return null;
    const files = state.data.entries.filter((e) => !e.dir);
    const rows = [...state.data.entries].sort((a, b) => a.path.localeCompare(b.path)).map((e) => [e.path, e.dir ? "Folder" : fileFormatSize(e.size), e.modified]);
    return { rows, count: files.length, total: files.reduce((a, e) => a + e.size, 0), truncated: state.data.truncated };
  }, [state]);
  if (state.status === "loading") return <Skeleton className={cn("h-72 w-full rounded-lg", className)} />;
  if (state.status === "error" || !table) return fallback(state.status === "error" && state.error === "too_large" ? "too_large" : "unreadable");
  return (
    <div className="flex flex-col gap-2">
      <VirtualTable header={["Name", "Size", "Modified"]} rows={table.rows} className={className} label={`Contents of ${name}`} />
      <p className="type-caption text-fg-muted">
        {table.count.toLocaleString()} {table.count === 1 ? "file" : "files"} · {fileFormatSize(table.total)} unpacked
        {table.truncated ? " · showing the first 20,000 entries" : ""}
      </p>
    </div>
  );
}
