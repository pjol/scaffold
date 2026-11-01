"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import { ChevronLeft, ChevronRight, ImageOff, X, ZoomIn, ZoomOut } from "lucide-react";
import { AnimatePresence, animate, motion, useMotionValue, type PanInfo } from "motion/react";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type WheelEvent } from "react";
import { cn } from "../lib/cn";
import { useControllableState } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { Badge } from "./Badge";

/** A photo to show. Only `id` and `url` are required. */
export interface GalleryPhoto {
  id: string;
  url: string;
  caption?: string | null;
  /** Short tag shown as a badge ("Cover", "Before"). */
  label?: string | null;
  created_at?: string | null;
}

export function formatPhotoTime(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleString([], { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export interface PhotoLightboxProps {
  photos: GalleryPhoto[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Current photo. Uncontrolled when omitted (starts at `defaultIndex`). */
  index?: number;
  defaultIndex?: number;
  onIndexChange?: (index: number) => void;
}

const MAX_ZOOM = 4;
const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/**
 * Full-screen viewer on a dark stage: swipe, arrow keys or buttons to page; double-tap,
 * pinch or ctrl/⌘-wheel to zoom; drag to pan; captions and thumbnails on wide screens.
 */
export function PhotoLightbox({ photos, open, onOpenChange, index, defaultIndex = 0, onIndexChange }: PhotoLightboxProps) {
  const t = useMotionTokens();
  const [current, setCurrent] = useControllableState({ value: index, defaultValue: defaultIndex, onChange: onIndexChange });
  const i = clamp(current, 0, Math.max(0, photos.length - 1));
  const photo = photos[i];
  const [dir, setDir] = useState(0);
  const [zoom, setZoom] = useState(1);
  const [broken, setBroken] = useState<Record<string, boolean>>({});
  const x = useMotionValue(0);
  const y = useMotionValue(0);
  const stage = useRef<HTMLDivElement>(null);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{ d0: number; z0: number } | null>(null);
  const lastTap = useRef(0);

  const resetPan = useCallback(() => {
    animate(x, 0, t.spring.snappy);
    animate(y, 0, t.spring.snappy);
  }, [x, y, t.spring.snappy]);

  const go = useCallback(
    (delta: number) => {
      const next = i + delta;
      if (next < 0 || next >= photos.length) return;
      setDir(delta);
      setZoom(1);
      x.set(0);
      y.set(0);
      setCurrent(next);
    },
    [i, photos.length, setCurrent, x, y],
  );

  const setZoomTo = useCallback(
    (z: number) => {
      const next = clamp(z, 1, MAX_ZOOM);
      setZoom(next);
      if (next === 1) resetPan();
    },
    [resetPan],
  );

  useEffect(() => {
    if (!open) return;
    setZoom(1);
    x.set(0);
    y.set(0);
  }, [open, x, y]);

  useEffect(() => {
    if (!open) return;
    [photos[i - 1], photos[i + 1]].forEach((p) => {
      if (p) new Image().src = p.url;
    });
  }, [open, i, photos]);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowRight") go(1);
    else if (e.key === "ArrowLeft") go(-1);
    else if (e.key === "+" || e.key === "=") setZoomTo(zoom * 1.5);
    else if (e.key === "-") setZoomTo(zoom / 1.5);
    else if (e.key === "0") setZoomTo(1);
    else return;
    e.preventDefault();
  };

  const onWheel = (e: WheelEvent) => {
    if (!e.ctrlKey && !e.metaKey) return;
    setZoomTo(zoom * Math.exp(-e.deltaY * 0.01));
  };

  const onPointerDown = (e: ReactPointerEvent) => {
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()];
      if (a && b) pinch.current = { d0: Math.hypot(a.x - b.x, a.y - b.y), z0: zoom };
    }
  };
  const onPointerMove = (e: ReactPointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pinch.current && pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()];
      if (!a || !b) return;
      const d = Math.hypot(a.x - b.x, a.y - b.y);
      setZoom(clamp((pinch.current.z0 * d) / Math.max(1, pinch.current.d0), 1, MAX_ZOOM));
    }
  };
  const onPointerUp = (e: ReactPointerEvent) => {
    pointers.current.delete(e.pointerId);
    if (pointers.current.size < 2 && pinch.current) {
      pinch.current = null;
      if (zoom <= 1.02) setZoomTo(1);
    }
  };

  const onTap = () => {
    const now = performance.now();
    if (now - lastTap.current < 300) setZoomTo(zoom > 1 ? 1 : 2.5);
    lastTap.current = now;
  };

  const onDragEnd = (_: unknown, info: PanInfo) => {
    if (zoom > 1 || pinch.current) return;
    if (info.offset.x < -80 || info.velocity.x < -500) go(1);
    else if (info.offset.x > 80 || info.velocity.x > 500) go(-1);
  };

  const rect = stage.current?.getBoundingClientRect();
  const panX = rect ? (rect.width * (zoom - 1)) / 2 : 0;
  const panY = rect ? (rect.height * (zoom - 1)) / 2 : 0;
  const time = formatPhotoTime(photo?.created_at);
  const navBtn =
    "absolute top-1/2 z-(--app-z-raised) hidden size-12 -translate-y-1/2 items-center justify-center rounded-pill bg-ink-50/15 text-ink-50 transition-colors hover:bg-ink-50/25 disabled:pointer-events-none disabled:opacity-0 md:flex [&_svg]:size-6";
  const chromeBtn = "flex size-tap items-center justify-center rounded-pill bg-ink-50/15 text-ink-50 transition-colors hover:bg-ink-50/25 disabled:opacity-40 [&_svg]:size-5";

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AnimatePresence>
        {open && photo && (
          <DialogPrimitive.Portal forceMount>
            <DialogPrimitive.Overlay asChild forceMount>
              <motion.div className="fixed inset-0 z-(--app-z-modal) bg-ink-950/95" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: t.duration.base, ease: t.ease.out }} />
            </DialogPrimitive.Overlay>
            <DialogPrimitive.Content asChild forceMount aria-describedby={undefined} onKeyDown={onKeyDown}>
              <motion.div
                className="fixed inset-0 z-(--app-z-modal) flex flex-col pt-safe pb-safe text-ink-50 outline-none"
                initial={{ opacity: 0, scale: 0.96 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.98, transition: { duration: t.duration.fast } }}
                transition={t.spring.gentle}
              >
                <DialogPrimitive.Title className="sr-only">
                  Photo {i + 1} of {photos.length}
                </DialogPrimitive.Title>
                <div className="flex items-center gap-2 px-3 py-3 sm:px-6">
                  <span className="rounded-pill bg-ink-50/15 px-3 py-1.5 type-label font-semibold tabular-nums" aria-live="polite">
                    {i + 1} / {photos.length}
                  </span>
                  {photo.label && (
                    <Badge tone="neutral" variant="solid" size="md">
                      {photo.label}
                    </Badge>
                  )}
                  <div className="ml-auto flex items-center gap-2">
                    <button type="button" className={cn(chromeBtn, "hidden sm:flex")} aria-label={zoom > 1 ? "Zoom out" : "Zoom in"} onClick={() => setZoomTo(zoom > 1 ? 1 : 2.5)}>
                      {zoom > 1 ? <ZoomOut /> : <ZoomIn />}
                    </button>
                    <DialogPrimitive.Close className={chromeBtn} aria-label="Close">
                      <X />
                    </DialogPrimitive.Close>
                  </div>
                </div>

                <div
                  ref={stage}
                  className="relative min-h-0 flex-1 touch-none overflow-hidden select-none"
                  onWheel={onWheel}
                  onPointerDown={onPointerDown}
                  onPointerMove={onPointerMove}
                  onPointerUp={onPointerUp}
                  onPointerCancel={onPointerUp}
                >
                  <AnimatePresence initial={false} custom={dir} mode="popLayout">
                    <motion.div
                      key={photo.id}
                      custom={dir}
                      variants={{
                        enter: (d: number) => ({ x: d > 0 ? "60%" : d < 0 ? "-60%" : 0, opacity: 0 }),
                        center: { x: 0, opacity: 1 },
                        exit: (d: number) => ({ x: d > 0 ? "-60%" : d < 0 ? "60%" : 0, opacity: 0 }),
                      }}
                      initial="enter"
                      animate="center"
                      exit="exit"
                      transition={{ ...t.spring.snappy, opacity: { duration: t.duration.fast } }}
                      className="absolute inset-0 flex items-center justify-center p-2 sm:p-6"
                    >
                      <motion.div
                        className="flex size-full items-center justify-center"
                        style={{ x, y }}
                        drag={zoom > 1 ? true : "x"}
                        dragConstraints={zoom > 1 ? { left: -panX, right: panX, top: -panY, bottom: panY } : { left: 0, right: 0 }}
                        dragElastic={zoom > 1 ? 0.15 : 0.6}
                        dragMomentum={zoom > 1}
                        onDragEnd={onDragEnd}
                        onTap={onTap}
                      >
                        {broken[photo.id] ? (
                          <div className="flex flex-col items-center gap-3 text-ink-300">
                            <ImageOff className="size-10" />
                            <span className="type-body font-semibold">Photo unavailable</span>
                          </div>
                        ) : (
                          <motion.img
                            src={photo.url}
                            alt={photo.caption || `Photo ${i + 1}`}
                            draggable={false}
                            animate={{ scale: zoom }}
                            transition={t.spring.snappy}
                            onError={() => setBroken((b) => ({ ...b, [photo.id]: true }))}
                            className={cn("max-h-full max-w-full rounded-md object-contain", zoom > 1 ? "cursor-grab active:cursor-grabbing" : "cursor-zoom-in")}
                          />
                        )}
                      </motion.div>
                    </motion.div>
                  </AnimatePresence>
                  <button type="button" className={cn(navBtn, "left-4")} aria-label="Previous photo" onClick={() => go(-1)} disabled={i === 0}>
                    <ChevronLeft />
                  </button>
                  <button type="button" className={cn(navBtn, "right-4")} aria-label="Next photo" onClick={() => go(1)} disabled={i >= photos.length - 1}>
                    <ChevronRight />
                  </button>
                </div>

                <div className="flex flex-col gap-3 px-4 pt-3 pb-4 sm:px-6">
                  {(photo.caption || time) && (
                    <div className="mx-auto flex w-full max-w-2xl items-baseline gap-3">
                      {photo.caption && <p className="min-w-0 flex-1 type-body font-semibold text-ink-50">{photo.caption}</p>}
                      {time && <span className="ml-auto shrink-0 type-label tabular-nums text-ink-300">{time}</span>}
                    </div>
                  )}
                  {photos.length > 1 && (
                    <div className="mx-auto hidden max-w-full gap-2 overflow-x-auto scrollbar-none md:flex" role="tablist" aria-label="Photos">
                      {photos.map((p, k) => (
                        <button
                          key={p.id}
                          type="button"
                          role="tab"
                          aria-selected={k === i}
                          aria-label={`Photo ${k + 1}`}
                          onClick={() => go(k - i)}
                          className={cn("relative size-14 shrink-0 overflow-hidden rounded-md transition-opacity", k === i ? "opacity-100 ring-2 ring-ink-50" : "opacity-55 hover:opacity-90")}
                        >
                          <img src={p.url} alt="" className="size-full object-cover" draggable={false} />
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </motion.div>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        )}
      </AnimatePresence>
    </DialogPrimitive.Root>
  );
}
