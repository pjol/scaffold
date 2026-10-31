"use client";

import { Minus, Plus } from "lucide-react";
import { cn } from "../lib/cn";
import { useControllableState } from "../lib/hooks";
import { AnimatedNumber } from "./AnimatedNumber";
import { IconButton } from "./IconButton";

export interface StepperProps {
  value?: number;
  defaultValue?: number;
  onValueChange?: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  /** Accessible name, e.g. "Quantity". @default "Quantity" */
  label?: string;
  disabled?: boolean;
  className?: string;
  /** Format the number (`(n) => \`${n} seats\``). */
  format?: (n: number) => string;
}

/** − / + with a rolling number between them. */
export function Stepper({ value, defaultValue = 1, onValueChange, min = 0, max = 99, step = 1, label = "Quantity", disabled, className, format }: StepperProps) {
  const [n, setN] = useControllableState({ value, defaultValue, onChange: onValueChange });
  const set = (next: number) => setN(Math.min(max, Math.max(min, next)));
  return (
    <div role="group" aria-label={label} className={cn("inline-flex w-fit items-center gap-2 rounded-md bg-sunken p-1", disabled && "opacity-50", className)}>
      <IconButton label={`Decrease ${label.toLowerCase()}`} icon={<Minus />} shape="square" variant="ghost" disabled={disabled || n <= min} onClick={() => set(n - step)} />
      <output aria-live="polite" className="min-w-10 text-center type-body font-semibold text-fg">
        <AnimatedNumber value={n} {...(format ? { format } : {})} />
      </output>
      <IconButton label={`Increase ${label.toLowerCase()}`} icon={<Plus />} shape="square" variant="ghost" disabled={disabled || n >= max} onClick={() => set(n + step)} />
    </div>
  );
}
