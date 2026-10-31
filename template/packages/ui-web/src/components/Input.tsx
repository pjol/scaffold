"use client";

import { forwardRef, useCallback, useEffect, useRef, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from "react";
import { cn } from "../lib/cn";
import { composeRefs } from "../lib/slot";
import { useFieldControlProps } from "./Field";

/**
 * Shared control chrome (inputs, select trigger, textarea): 16 px text everywhere (no
 * iOS zoom), a strong outline, and a focus ring in the `ring` colour.
 */
export const controlClasses =
  "w-full rounded-md border border-border-strong bg-surface text-body text-fg placeholder:text-fg-subtle outline-none " +
  "transition-[border-color,box-shadow] duration-(--app-duration-fast) " +
  "focus:border-ring focus:ring-1 focus:ring-ring focus-visible:outline-none " +
  "aria-invalid:border-danger aria-invalid:focus:ring-danger " +
  "disabled:cursor-not-allowed disabled:opacity-50";

/** 40 px on fine pointers, 48 on touch (design.md). */
export const controlHeight = "h-control pointer-coarse:h-tap";

export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "size"> {
  /** Icon inside the left edge. */
  leadingIcon?: ReactNode;
  /** Node inside the right edge (unit, clear button). */
  trailing?: ReactNode;
  /** Class for the outer wrapper. */
  wrapperClassName?: string;
}

/** Text input wired to its {@link Field}. */
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ leadingIcon, trailing, className, wrapperClassName, ...props }, ref) {
  const p = useFieldControlProps(props);
  return (
    <div className={cn("relative flex items-center", wrapperClassName)}>
      {leadingIcon && <span className="pointer-events-none absolute left-3 flex text-fg-muted [&_svg]:size-4">{leadingIcon}</span>}
      <input ref={ref} className={cn(controlClasses, controlHeight, leadingIcon ? "pl-10" : "pl-3", trailing ? "pr-12" : "pr-3", className)} {...p} />
      {trailing && <span className="absolute right-1 flex items-center text-fg-muted">{trailing}</span>}
    </div>
  );
});

export interface TextAreaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  /** Grow with content up to `maxRows`. @default true */
  autoGrow?: boolean;
  minRows?: number;
  maxRows?: number;
}

/** Multi-line input that grows with its content. */
export const TextArea = forwardRef<HTMLTextAreaElement, TextAreaProps>(function TextArea({ autoGrow = true, minRows = 3, maxRows = 10, className, onChange, ...props }, ref) {
  const p = useFieldControlProps(props);
  const inner = useRef<HTMLTextAreaElement | null>(null);
  const resize = useCallback(() => {
    const el = inner.current;
    if (!el || !autoGrow) return;
    const cs = getComputedStyle(el);
    const lh = parseFloat(cs.lineHeight) || 24;
    const pad = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom) + parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth);
    el.style.height = "auto";
    el.style.height = `${Math.min(Math.max(el.scrollHeight + parseFloat(cs.borderTopWidth) + parseFloat(cs.borderBottomWidth), lh * minRows + pad), lh * maxRows + pad)}px`;
  }, [autoGrow, minRows, maxRows]);
  useEffect(resize, [resize, props.value]);
  return (
    <textarea
      ref={composeRefs(ref, inner)}
      rows={minRows}
      className={cn(controlClasses, "resize-none px-3 py-2", className)}
      onChange={(e) => {
        onChange?.(e);
        resize();
      }}
      {...p}
    />
  );
});
