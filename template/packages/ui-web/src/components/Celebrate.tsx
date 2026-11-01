"use client";

import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";

interface Particle {
  id: number;
  x: number;
  y: number;
  rotate: number;
  color: string;
  shape: "rect" | "circle" | "strip";
  size: number;
  delay: number;
  duration: number;
}

export interface CelebrateProps {
  /** Change this value (increment a counter) to fire a burst. Falsy = idle. */
  fire: number | boolean | string | null | undefined;
  /** @default 36 */
  particles?: number;
  /** `radial` (all directions) or `fountain` (upward fan). @default "radial" */
  mode?: "radial" | "fountain";
  /** Max travel in px. @default 160 */
  power?: number;
  /** Colours. Defaults to the palette's intent fills. */
  colors?: string[];
  /** Wrap a target: the burst starts from its centre. */
  children?: ReactNode;
  className?: string;
  onDone?: () => void;
}

const DEFAULT_COLORS = ["var(--app-primary)", "var(--app-accent)", "var(--app-success)", "var(--app-warning)", "var(--app-info)", "var(--app-danger)"];

function makeParticles(n: number, mode: "radial" | "fountain", power: number, colors: string[], seed: number): Particle[] {
  const shapes: Particle["shape"][] = ["rect", "circle", "strip"];
  return Array.from({ length: n }, (_, i) => {
    const angle = mode === "fountain" ? -Math.PI / 2 + (Math.random() - 0.5) * Math.PI * 0.85 : (i / n) * Math.PI * 2 + Math.random() * 0.5;
    const dist = power * (0.45 + Math.random() * 0.55);
    return {
      id: seed * 1000 + i,
      x: Math.cos(angle) * dist,
      y: Math.sin(angle) * dist,
      rotate: (Math.random() - 0.5) * 720,
      color: colors[i % colors.length] ?? "currentColor",
      shape: shapes[i % 3] ?? "rect",
      size: 6 + Math.random() * 6,
      delay: Math.random() * 0.08,
      duration: 0.8 + Math.random() * 0.4,
    };
  });
}

/**
 * Confetti + ring burst for success moments (use sparingly: once per real achievement).
 * Reduced motion: one soft ring, no particles. The slot for OriginKit's `emojiburst`.
 *
 * ```tsx
 * const [n, setN] = useState(0);
 * <Celebrate fire={n}><Button onClick={() => { send(); setN(n + 1); }}>Send</Button></Celebrate>
 * ```
 */
export function Celebrate({ fire, particles = 36, mode = "radial", power = 160, colors = DEFAULT_COLORS, children, className, onDone }: CelebrateProps) {
  const reduce = useReducedMotion();
  const [bursts, setBursts] = useState<{ key: number; items: Particle[] }[]>([]);
  const first = useRef(true);
  const counter = useRef(0);
  const done = useRef(onDone);
  done.current = onDone;

  useEffect(() => {
    if (first.current) {
      first.current = false;
      return undefined;
    }
    if (!fire) return undefined;
    const key = ++counter.current;
    setBursts((b) => [...b, { key, items: reduce ? [] : makeParticles(particles, mode, power, colors, key) }]);
    const timer = setTimeout(() => {
      setBursts((b) => b.filter((x) => x.key !== key));
      done.current?.();
    }, 1500);
    return () => clearTimeout(timer);
    // Fires only when `fire` changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fire]);

  return (
    <span className={cn("relative inline-flex", !children && "pointer-events-none absolute inset-0", className)}>
      {children}
      <span aria-hidden className="pointer-events-none absolute inset-0 z-(--app-z-raised) flex items-center justify-center overflow-visible">
        <AnimatePresence>
          {bursts.map((burst) => (
            <span key={burst.key} className="absolute top-1/2 left-1/2">
              <motion.span
                className="absolute -top-8 -left-8 size-16 rounded-pill border-4 border-primary"
                initial={{ scale: 0.2, opacity: 0.9 }}
                animate={{ scale: reduce ? 1.4 : 3, opacity: 0 }}
                transition={{ duration: 0.6, ease: [0.16, 1, 0.3, 1] }}
              />
              {burst.items.map((p) => (
                <motion.span
                  key={p.id}
                  className={cn("absolute block", p.shape === "circle" ? "rounded-pill" : "rounded-xs")}
                  style={{
                    width: p.shape === "strip" ? p.size * 0.45 : p.size,
                    height: p.shape === "strip" ? p.size * 1.6 : p.size,
                    background: p.color,
                    marginLeft: -p.size / 2,
                    marginTop: -p.size / 2,
                  }}
                  initial={{ x: 0, y: 0, scale: 0.4, opacity: 1, rotate: 0 }}
                  animate={{ x: [0, p.x, p.x * 1.08], y: [0, p.y, p.y + 80], scale: [0.4, 1, 0.9], opacity: [1, 1, 0], rotate: p.rotate }}
                  transition={{ duration: p.duration, delay: p.delay, times: [0, 0.45, 1], ease: [0.16, 1, 0.3, 1] }}
                />
              ))}
            </span>
          ))}
        </AnimatePresence>
      </span>
    </span>
  );
}
