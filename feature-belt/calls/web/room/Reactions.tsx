"use client";

import type { CallsReaction } from "@scaffoldapp/shared";
import { AnimatePresence, motion, useReducedMotion } from "@scaffoldapp/ui-web";

export interface FloatingReaction {
  id: number;
  emoji: CallsReaction;
  name: string;
  /** 0..1 horizontal offset so bursts don't stack exactly. */
  x: number;
}

/** Reactions rise from the bottom left of the stage and fade (a fade only with reduced motion). */
export function ReactionsLayer({ items }: { items: FloatingReaction[] }) {
  const reduce = useReducedMotion();
  return (
    <div className="pointer-events-none absolute inset-y-0 left-0 w-40 overflow-hidden sm:w-56" aria-live="polite">
      <AnimatePresence>
        {items.map((r) => (
          <motion.div
            key={r.id}
            className="absolute bottom-4 flex flex-col items-center gap-1"
            style={{ left: `${8 + r.x * 60}%` }}
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 0, scale: 0.6 }}
            animate={reduce ? { opacity: 1 } : { opacity: 1, y: -240, scale: 1 }}
            exit={{ opacity: 0 }}
            transition={reduce ? { duration: 0.16 } : { duration: 3, ease: "easeOut" }}
          >
            <span className="text-display" aria-hidden>
              {r.emoji}
            </span>
            <span className="max-w-32 truncate rounded-pill bg-ink-950/70 px-2 type-caption text-ink-50">{r.name}</span>
            <span className="sr-only">
              {r.name} reacted {r.emoji}
            </span>
          </motion.div>
        ))}
      </AnimatePresence>
    </div>
  );
}
