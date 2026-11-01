"use client";

import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { forwardRef, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useFieldControlProps } from "./Field";
import { controlClasses, controlHeight } from "./Input";

export interface SelectOption {
  value: string;
  label: ReactNode;
  icon?: ReactNode;
  disabled?: boolean;
}

export interface SelectProps {
  options: SelectOption[];
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  name?: string;
  id?: string;
  className?: string;
  /** Accessible label when not inside a Field. */
  "aria-label"?: string;
}

/** Radix Select with an options array; inherits id and invalid state from its {@link Field}. */
export const Select = forwardRef<HTMLButtonElement, SelectProps>(function Select(
  { options, value, defaultValue, onValueChange, placeholder = "Choose", disabled, required, name, id, className, ...aria },
  ref,
) {
  const p = useFieldControlProps({ id, disabled, required });
  return (
    <SelectPrimitive.Root
      {...(value !== undefined ? { value } : {})}
      {...(defaultValue !== undefined ? { defaultValue } : {})}
      {...(onValueChange ? { onValueChange } : {})}
      {...(p.disabled !== undefined ? { disabled: p.disabled } : {})}
      {...(p.required !== undefined ? { required: p.required } : {})}
      {...(name ? { name } : {})}
    >
      <SelectPrimitive.Trigger
        ref={ref}
        id={p.id}
        aria-invalid={p["aria-invalid"]}
        aria-describedby={p["aria-describedby"]}
        aria-label={aria["aria-label"]}
        className={cn(controlClasses, controlHeight, "group flex items-center justify-between gap-2 px-3 text-left data-[placeholder]:text-fg-subtle", className)}
      >
        <span className="flex min-w-0 items-center gap-2 truncate [&_svg]:size-4">
          <SelectPrimitive.Value placeholder={placeholder} />
        </span>
        <SelectPrimitive.Icon asChild>
          <ChevronDown className="size-4 shrink-0 text-fg-muted transition-transform spring-snappy group-data-[state=open]:rotate-180" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          collisionPadding={12}
          className={cn(
            "z-(--app-z-dropdown) max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) overflow-hidden rounded-lg border border-border bg-surface-raised p-1 shadow-lg",
            "origin-(--radix-select-content-transform-origin) animate-pop-in data-[state=closed]:animate-pop-out",
          )}
        >
          <SelectPrimitive.Viewport>
            {options.map((o) => (
              <SelectPrimitive.Item
                key={o.value}
                value={o.value}
                {...(o.disabled ? { disabled: true } : {})}
                className={cn(
                  "relative flex min-h-control cursor-pointer select-none items-center gap-2 rounded-sm py-2 pr-10 pl-3 type-body text-fg outline-none pointer-coarse:min-h-tap",
                  "data-[highlighted]:bg-sunken data-[state=checked]:font-semibold data-[disabled]:opacity-50 [&_svg]:size-4",
                )}
              >
                {o.icon}
                <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-3 inline-flex text-primary-strong">
                  <Check strokeWidth={2.5} />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
});
