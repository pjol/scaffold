"use client";

import { motion } from "motion/react";
import { forwardRef, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useControllableState } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { useFieldControlProps } from "./Field";
import { InfoTip } from "./InfoTip";

export interface CheckboxProps {
  checked?: boolean;
  defaultChecked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  /** Inline label; the whole row toggles the box. May contain links or buttons. */
  label?: ReactNode;
  /** ⓘ after the label. */
  info?: ReactNode;
  disabled?: boolean;
  required?: boolean;
  /** Error outline (a required box left unchecked). */
  invalid?: boolean;
  id?: string;
  name?: string;
  className?: string;
  "aria-label"?: string;
  "aria-describedby"?: string;
}

/** Checkbox on a native `<input>` (forms and a11y) with a drawn-in check. */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { checked, defaultChecked = false, onCheckedChange, label, info, disabled, required, invalid, id, name, className, ...aria },
  ref,
) {
  const t = useMotionTokens();
  const auto = useId();
  const p = useFieldControlProps({ id: id ?? `cb${auto.replace(/[^a-zA-Z0-9]/g, "")}`, disabled });
  const [on, setOn] = useControllableState({ value: checked, defaultValue: defaultChecked, onChange: onCheckedChange });
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <label
        htmlFor={p.id}
        className={cn("group flex min-h-control flex-1 items-center gap-3 py-2 pointer-coarse:min-h-tap", p.disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer")}
      >
        <input
          ref={ref}
          id={p.id}
          type="checkbox"
          className="peer sr-only"
          checked={on}
          disabled={p.disabled}
          required={required}
          name={name}
          aria-invalid={invalid || undefined}
          aria-label={aria["aria-label"]}
          aria-describedby={aria["aria-describedby"]}
          onChange={(e) => setOn(e.target.checked)}
        />
        <motion.span
          aria-hidden
          whileTap={p.disabled ? undefined : { scale: 0.88 }}
          transition={t.spring.press}
          className={cn(
            "relative flex size-5 shrink-0 items-center justify-center rounded-xs border transition-colors duration-(--app-duration-fast)",
            "peer-focus-visible:outline-3 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ring",
            on ? "border-primary bg-primary text-primary-fg" : invalid ? "border-danger bg-surface" : "border-border-strong bg-surface group-hover:border-fg-muted",
          )}
        >
          <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={3.2} strokeLinecap="round" strokeLinejoin="round">
            <motion.path d="M5 12.5l4.5 4.5L19 7.5" initial={false} animate={{ pathLength: on ? 1 : 0, opacity: on ? 1 : 0 }} transition={{ ...t.spring.snappy, opacity: { duration: 0.08 } }} />
          </svg>
        </motion.span>
        {label && <span className="flex-1 type-body text-fg">{label}</span>}
      </label>
      {info && <InfoTip content={info} />}
    </div>
  );
});
