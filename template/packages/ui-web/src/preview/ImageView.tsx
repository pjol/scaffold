"use client";

import { Maximize2, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type WheelEvent } from "react";
import { IconButton } from "../components/IconButton";
import { cn } from "../lib/cn";

const MIN = 1;
const MAX = 8;

interface View {
  s: number;
  x: number;
  y: number;
}

/**
 * An image fitted to its box that zooms (buttons, + / − / 0, ctrl/⌘ + wheel or trackpad pinch,
 * two-finger pinch, double click) and pans when zoomed (drag, arrow keys).
 */
export function ImageView({ src, alt, className, onError }: { src: string; alt: string; className?: string; onError?: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>({ s: 1, x: 0, y: 0 });
  const [dragging, setDragging] = useState(false);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{ d: number; s: number } | null>(null);

  const clamp = useCallback((v: View): View => {
    const el = box.current;
    const s = Math.min(MAX, Math.max(MIN, v.s));
    if (!el || s === 1) return { s, x: 0, y: 0 };
    const mx = (el.clientWidth * (s - 1)) / 2;
    const my = (el.clientHeight * (s - 1)) / 2;
    return { s, x: Math.min(mx, Math.max(-mx, v.x)), y: Math.min(my, Math.max(-my, v.y)) };
  }, []);

  /** Zoom to `s` keeping the point (px, py), relative to the box centre, still. */
  const zoomAt = useCallback(
    (s: number, px = 0, py = 0) =>
      setView((v) => {
        const next = Math.min(MAX, Math.max(MIN, s));
        const k = next / v.s;
        return clamp({ s: next, x: px - (px - v.x) * k, y: py - (py - v.y) * k });
      }),
    [clamp],
  );

  useEffect(() => setView({ s: 1, x: 0, y: 0 }), [src]);

  const rel = (clientX: number, clientY: number) => {
    const r = box.current!.getBoundingClientRect();
    return { px: clientX - r.left - r.width / 2, py: clientY - r.top - r.height / 2 };
  };

  const onWheel = (e: WheelEvent<HTMLDivElement>) => {
    if (!e.ctrlKey && !e.metaKey && view.s === 1) return;
    e.preventDefault();
    const { px, py } = rel(e.clientX, e.clientY);
    if (e.ctrlKey || e.metaKey) zoomAt(view.s * Math.exp(-e.deltaY / 200), px, py);
    else setView((v) => clamp({ ...v, x: v.x - e.deltaX, y: v.y - e.deltaY }));
  };

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    e.currentTarget.setPointerCapture(e.pointerId);
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()];
      pinch.current = { d: Math.hypot(a!.x - b!.x, a!.y - b!.y), s: view.s };
    }
    setDragging(true);
  };
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const prev = pointers.current.get(e.pointerId);
    if (!prev) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 2 && pinch.current) {
      const [a, b] = [...pointers.current.values()];
      const d = Math.hypot(a!.x - b!.x, a!.y - b!.y);
      const { px, py } = rel((a!.x + b!.x) / 2, (a!.y + b!.y) / 2);
      zoomAt((pinch.current.s * d) / pinch.current.d, px, py);
    } else if (view.s > 1) {
      setView((v) => clamp({ ...v, x: v.x + e.clientX - prev.x, y: v.y + e.clientY - prev.y }));
    }
  };
  const onPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    pointers.current.delete(e.pointerId);
    if (pointers.current.size < 2) pinch.current = null;
    if (pointers.current.size === 0) setDragging(false);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = 48;
    const keys: Record<string, () => void> = {
      "+": () => zoomAt(view.s * 1.5),
      "=": () => zoomAt(view.s * 1.5),
      "-": () => zoomAt(view.s / 1.5),
      "0": () => setView({ s: 1, x: 0, y: 0 }),
      ArrowLeft: () => setView((v) => clamp({ ...v, x: v.x + step })),
      ArrowRight: () => setView((v) => clamp({ ...v, x: v.x - step })),
      ArrowUp: () => setView((v) => clamp({ ...v, y: v.y + step })),
      ArrowDown: () => setView((v) => clamp({ ...v, y: v.y - step })),
    };
    const fn = keys[e.key];
    if (!fn || (e.key.startsWith("Arrow") && view.s === 1)) return;
    e.preventDefault();
    fn();
  };

  return (
    <div className={cn("relative overflow-hidden rounded-lg bg-sunken", className)}>
      <div
        ref={box}
        role="img"
        aria-label={`${alt}. Zoom with plus and minus, move with the arrow keys.`}
        tabIndex={0}
        className={cn(
          "flex size-full items-center justify-center outline-none select-none focus-visible:ring-2 focus-visible:ring-ring",
          view.s > 1 ? (dragging ? "cursor-grabbing" : "cursor-grab") : "cursor-zoom-in",
        )}
        style={{ touchAction: view.s > 1 ? "none" : "pan-y" }}
        onWheel={onWheel}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={(e) => {
          const { px, py } = rel(e.clientX, e.clientY);
          if (view.s > 1) setView({ s: 1, x: 0, y: 0 });
          else zoomAt(2.5, px, py);
        }}
        onKeyDown={onKeyDown}
      >
        {/* eslint-disable-next-line @next/next/no-img-element -- short-lived links from storage; next/image can't optimise them */}
        <img
          src={src}
          alt=""
          draggable={false}
          onError={onError}
          className={cn("max-h-full max-w-full object-contain", !dragging && "motion-safe:transition-transform motion-safe:duration-150")}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.s})` }}
        />
      </div>
      <div className="absolute right-2 bottom-2 flex items-center gap-1 rounded-pill bg-surface-raised p-1 shadow-md">
        <IconButton size="sm" variant="ghost" label="Zoom out" icon={<ZoomOut />} disabled={view.s <= MIN} onClick={() => zoomAt(view.s / 1.5)} />
        <span className="w-12 text-center type-caption text-fg-muted tabular-nums" aria-live="polite">
          {Math.round(view.s * 100)}%
        </span>
        <IconButton size="sm" variant="ghost" label="Zoom in" icon={<ZoomIn />} disabled={view.s >= MAX} onClick={() => zoomAt(view.s * 1.5)} />
        <IconButton size="sm" variant="ghost" label="Fit" icon={<Maximize2 />} disabled={view.s === 1} onClick={() => setView({ s: 1, x: 0, y: 0 })} />
      </div>
    </div>
  );
}
