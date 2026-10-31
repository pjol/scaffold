"use client";

import * as SwitchPrimitive from "@radix-ui/react-switch";
import { forwardRef, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useFieldControlProps } from "./Field";
import { InfoTip } from "./InfoTip";

export interface SwitchProps {
  checked?: boolean;
  defaultChecked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
  /** Inline label; the whole row toggles the switch. */
  label?: ReactNode;
  /** ⓘ next to the label. */
  info?: ReactNode;
  id?: string;
  name?: string;
  className?: string;
  "aria-label"?: string;
}

/** Toggle switch with a spring thumb. With `label`, a full-width row (48 px on touch). */
export const Switch = forwardRef<HTMLButtonElement, SwitchProps>(function Switch(
  { checked, defaultChecked, onCheckedChange, disabled, label, info, id, name, className, ...aria },
  ref,
) {
  const auto = useId();
  const p = useFieldControlProps({ id: id ?? (label ? `sw${auto.replace(/[^a-zA-Z0-9]/g, "")}` : undefined), disabled });
  const control = (
    <SwitchPrimitive.Root
      ref={ref}
      id={p.id}
      {...(checked !== undefined ? { checked } : {})}
      {...(defaultChecked !== undefined ? { defaultChecked } : {})}
      {...(onCheckedChange ? { onCheckedChange } : {})}
      {...(p.disabled ? { disabled: true } : {})}
      {...(name ? { name } : {})}
      aria-label={aria["aria-label"]}
      className={cn(
        "group relative inline-flex h-6 w-10 shrink-0 cursor-pointer items-center rounded-pill p-0.5 transition-colors duration-(--app-duration-fast)",
        "before:absolute before:-inset-x-1 before:-inset-y-2 before:content-[''] pointer-coarse:before:-inset-y-3",
        "bg-border-strong data-[state=checked]:bg-primary disabled:cursor-not-allowed disabled:opacity-50",
        !label && className,
      )}
    >
      <SwitchPrimitive.Thumb className="block size-5 rounded-pill bg-surface transition-transform spring-snappy data-[state=checked]:translate-x-4" />
    </SwitchPrimitive.Root>
  );
  if (!label) return control;
  return (
    <div className={cn("flex min-h-control items-center gap-3 pointer-coarse:min-h-tap", className)}>
      <div className="flex flex-1 items-center gap-2">
        <label htmlFor={p.id} className="cursor-pointer type-body text-fg">
          {label}
        </label>
        {info && <InfoTip content={info} />}
      </div>
      {control}
    </div>
  );
});
