"use client";

import { ImageOff, Images } from "lucide-react";
import { motion } from "motion/react";
import { useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { Badge } from "./Badge";
import { PhotoLightbox, type GalleryPhoto } from "./PhotoLightbox";
import { Skeleton } from "./Skeleton";

export interface PhotoGridProps {
  photos: GalleryPhoto[];
  /** @default 3 */
  columns?: 2 | 3 | 4;
  /** Built-in lightbox on tap; pass `false` and use `onOpen` to handle it yourself. @default true */
  lightbox?: boolean;
  onOpen?: (index: number) => void;
  /** Skeleton tiles while loading. */
  loading?: boolean;
  /** Shown when there are no photos (default: a compact "No photos" row). */
  empty?: ReactNode;
  className?: string;
}

const cols = { 2: "grid-cols-2", 3: "grid-cols-3", 4: "grid-cols-3 sm:grid-cols-4" } as const;

/** Square tiles with optional label badges and caption scrims; tap opens the {@link PhotoLightbox}. */
export function PhotoGrid({ photos, columns = 3, lightbox = true, onOpen, loading, empty, className }: PhotoGridProps) {
  const t = useMotionTokens();
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const [broken, setBroken] = useState<Record<string, boolean>>({});

  if (loading) {
    return (
      <div className={cn("grid gap-2", cols[columns], className)} aria-busy>
        {Array.from({ length: columns }, (_, k) => (
          <Skeleton key={k} className="aspect-square rounded-lg" />
        ))}
      </div>
    );
  }
  if (photos.length === 0) {
    return (
      <>
        {empty ?? (
          <div className={cn("flex min-h-row items-center gap-2 rounded-lg bg-sunken px-4 py-3 type-label text-fg-muted [&_svg]:size-4", className)}>
            <Images />
            No photos
          </div>
        )}
      </>
    );
  }

  return (
    <>
      <ul className={cn("grid gap-2", cols[columns], className)} aria-label="Photos">
        {photos.map((p, k) => (
          <motion.li key={p.id} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ ...t.spring.gentle, delay: Math.min(k, 8) * t.stagger }}>
            <motion.button
              type="button"
              whileTap={{ scale: t.pressScale }}
              transition={t.spring.press}
              onClick={() => {
                onOpen?.(k);
                if (lightbox) {
                  setIndex(k);
                  setOpen(true);
                }
              }}
              aria-label={[p.label, p.caption || `Photo ${k + 1}`].filter(Boolean).join(": ")}
              className="group relative block aspect-square w-full overflow-hidden rounded-lg bg-sunken"
            >
              {broken[p.id] ? (
                <span className="flex size-full items-center justify-center text-fg-muted [&_svg]:size-6">
                  <ImageOff />
                </span>
              ) : (
                <img
                  src={p.url}
                  alt=""
                  loading="lazy"
                  draggable={false}
                  onError={() => setBroken((b) => ({ ...b, [p.id]: true }))}
                  className="size-full object-cover transition-transform duration-(--app-duration-base) group-hover:scale-[1.03]"
                />
              )}
              {p.label && (
                <Badge tone="neutral" variant="solid" className="absolute top-2 left-2">
                  {p.label}
                </Badge>
              )}
              {p.caption && (
                <span className="absolute inset-x-0 bottom-0 bg-linear-to-t from-ink-950/85 to-transparent px-2 pt-6 pb-2 text-left type-caption text-ink-50">
                  <span className="line-clamp-2">{p.caption}</span>
                </span>
              )}
            </motion.button>
          </motion.li>
        ))}
      </ul>
      {lightbox && <PhotoLightbox photos={photos} open={open} onOpenChange={setOpen} index={index} onIndexChange={setIndex} />}
    </>
  );
}
