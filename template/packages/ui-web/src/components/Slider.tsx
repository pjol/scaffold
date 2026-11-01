"use client";

import * as SliderPrimitive from "@radix-ui/react-slider";
import { forwardRef, useState } from "react";
import { cn } from "../lib/cn";

export interface SliderProps {
  value?: number[];
  defaultValue?: number[];
  onValueChange?: (value: number[]) => void;
  onValueCommit?: (value: number[]) => void;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  /** Format the value shown above the thumb while dragging or focused. Omit to hide it. */
  formatValue?: (value: number) => string;
  className?: string;
  name?: string;
  "aria-label"?: string;
}

/** Range slider (one or more thumbs). */
export const Slider = forwardRef<HTMLSpanElement, SliderProps>(function Slider(
  { value, defaultValue, onValueChange, onValueCommit, min = 0, max = 100, step = 1, disabled, formatValue, className, name, ...aria },
  ref,
) {
  const [inner, setInner] = useState<number[]>(defaultValue ?? [min]);
  const values = value ?? inner;
  return (
    <SliderPrimitive.Root
      ref={ref}
      value={values}
      onValueChange={(v) => {
        if (!value) setInner(v);
        onValueChange?.(v);
      }}
      {...(onValueCommit ? { onValueCommit } : {})}
      {...(name ? { name } : {})}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      className={cn("relative flex h-control w-full touch-none select-none items-center data-[disabled]:opacity-50 pointer-coarse:h-tap", className)}
    >
      <SliderPrimitive.Track className="relative h-1.5 grow overflow-hidden rounded-pill bg-sunken">
        <SliderPrimitive.Range className="absolute h-full rounded-pill bg-primary" />
      </SliderPrimitive.Track>
      {values.map((v, i) => (
        <SliderPrimitive.Thumb
          key={i}
          aria-label={aria["aria-label"]}
          className="group relative block size-5 cursor-grab rounded-pill border-2 border-primary bg-surface outline-none transition-transform spring-snappy before:absolute before:-inset-3 before:content-[''] active:cursor-grabbing focus-visible:ring-2 focus-visible:ring-ring"
        >
          {formatValue && (
            <span className="pointer-events-none absolute -top-10 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-sm bg-inverse px-2 py-1 type-caption tabular-nums text-fg-inverse opacity-0 shadow-md transition-opacity group-focus-visible:opacity-100 group-active:opacity-100">
              {formatValue(v)}
            </span>
          )}
        </SliderPrimitive.Thumb>
      ))}
    </SliderPrimitive.Root>
  );
});
