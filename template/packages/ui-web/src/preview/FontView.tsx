"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Skeleton } from "../components/Skeleton";
import { cn } from "../lib/cn";
import { fetchBytes, useLoaded } from "./load";

const MAX_FONT_BYTES = 20 * 1024 * 1024;
const SIZES = [16, 24, 36, 56];
const SAMPLE = "The quick brown fox jumps over the lazy dog";
const GLYPHS = ["ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz", "0123456789 !?&@#%*()[]{}.,;:'\"-+=/"];

let seq = 0;

/** A font file drawn as sample text at several sizes (loaded with the FontFace API, removed on unmount). */
export default function FontView({ url, className, fallback }: { url: string; className?: string; fallback: (reason: string) => ReactNode }) {
  const [family] = useState(() => `file-preview-font-${++seq}`);
  const state = useLoaded(url, async (signal) => {
    const { bytes } = await fetchBytes(url, MAX_FONT_BYTES, signal);
    const face = new FontFace(family, bytes.slice().buffer);
    await face.load();
    return face;
  });
  const face = state.status === "ready" ? state.data : null;
  useEffect(() => {
    if (!face) return;
    document.fonts.add(face);
    return () => {
      document.fonts.delete(face);
    };
  }, [face]);
  if (state.status === "loading") return <Skeleton className={cn("h-72 w-full rounded-lg", className)} />;
  if (state.status === "error") return fallback(state.error === "too_large" ? "too_large" : "unreadable");
  return (
    <div className={cn("flex flex-col gap-4 overflow-auto rounded-lg border border-border bg-surface p-card text-fg", className)} style={{ fontFamily: `"${family}", var(--font-body)` }}>
      {GLYPHS.map((g) => (
        <p key={g} className="break-all" style={{ fontSize: 24, lineHeight: 1.3 }}>
          {g}
        </p>
      ))}
      {SIZES.map((s) => (
        <div key={s} className="flex flex-col gap-1">
          <span className="font-sans type-caption text-fg-subtle" style={{ fontFamily: "var(--font-body)" }}>
            {s} px
          </span>
          <p style={{ fontSize: s, lineHeight: 1.2 }}>{SAMPLE}</p>
        </div>
      ))}
    </div>
  );
}
