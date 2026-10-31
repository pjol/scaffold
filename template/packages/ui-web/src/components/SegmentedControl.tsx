"use client";

import { motion } from "motion/react";
import { useId, useRef, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useControllableState } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";

export interface SegmentOption<V extends string = string> {
  value: V;
  label: ReactNode;
  icon?: ReactNode;
  disabled?: boolean;
}

export interface SegmentedControlProps<V extends string = string> {
  options: SegmentOption<V>[];
  value?: V;
  defaultValue?: V;
  onValueChange?: (value: V) => void;
  /** Stretch segments to fill the width. @default true */
  fullWidth?: boolean;
  className?: string;
  "aria-label"?: string;
}

/**
 * Single choice among 2–4 options (radio group) with a spring-animated indicator.
 * Arrow keys, Home and End move the selection.
 */
export function SegmentedControl<V extends string = string>({ options, value, defaultValue, onValueChange, fullWidth = true, className, ...aria }: SegmentedControlProps<V>) {
  const t = useMotionTokens();
  const layoutId = useId();
  const [current, setCurrent] = useControllableState<V>({ value, defaultValue: defaultValue ?? (options[0]?.value as V), onChange: onValueChange });
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const enabled = options.filter((o) => !o.disabled);
    const idx = enabled.findIndex((o) => o.value === current);
    let next = -1;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") next = (idx + 1) % enabled.length;
    if (e.key === "ArrowLeft" || e.key === "ArrowUp") next = (idx - 1 + enabled.length) % enabled.length;
    if (e.key === "Home") next = 0;
    if (e.key === "End") next = enabled.length - 1;
    if (next < 0) return;
    e.preventDefault();
    const opt = enabled[next];
    if (!opt) return;
    setCurrent(opt.value);
    refs.current[options.indexOf(opt)]?.focus();
  };

  return (
    <div
      role="radiogroup"
      aria-label={aria["aria-label"]}
      onKeyDown={onKeyDown}
      className={cn("relative inline-flex h-control gap-1 rounded-md bg-sunken p-1 pointer-coarse:h-tap", fullWidth && "flex w-full", className)}
    >
      {options.map((o, i) => {
        const selected = o.value === current;
        return (
          <button
            key={o.value}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected ? 0 : -1}
            disabled={o.disabled}
            onClick={() => setCurrent(o.value)}
            className={cn(
              "relative z-0 inline-flex min-w-0 items-center justify-center gap-1.5 rounded-sm px-3 text-label font-semibold transition-colors duration-(--app-duration-fast) [&_svg]:size-4 disabled:opacity-40",
              fullWidth && "flex-1",
              selected ? "text-fg" : "text-fg-muted hover:text-fg",
            )}
          >
            {selected && <motion.span layoutId={layoutId} className="absolute inset-0 -z-10 rounded-sm border border-border bg-surface" transition={t.spring.snappy} />}
            {o.icon}
            <span className="truncate">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}
