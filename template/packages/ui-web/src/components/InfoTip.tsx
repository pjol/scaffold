"use client";

import * as PopoverPrimitive from "@radix-ui/react-popover";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { Info } from "lucide-react";
import { forwardRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useHoverCapable } from "../lib/hooks";
import { bubbleClasses } from "./Tooltip";

export interface InfoTipProps {
  /** The explanation. Context goes here instead of a subtitle. */
  content: ReactNode;
  /** Accessible name for the trigger. @default "More info" */
  label?: string;
  /** Icon size: `sm` 16 px (inline with labels), `md` 20 px (titles). @default "sm" */
  size?: "sm" | "md";
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  /** Icon colour. @default "muted" */
  tone?: "muted" | "primary" | "current";
  className?: string;
}

const toneClass = {
  muted: "text-fg-muted hover:text-fg",
  primary: "text-primary-strong",
  current: "text-current",
} as const;

// Hit area: the 16/20 px icon grows to 24 px with ::before, and to 44 px on coarse pointers.
const hitClass = {
  sm: "before:-inset-1 pointer-coarse:before:-inset-3.5",
  md: "before:-inset-0.5 pointer-coarse:before:-inset-3",
} as const;

type TriggerProps = { label: string; size: "sm" | "md"; tone: keyof typeof toneClass } & ButtonHTMLAttributes<HTMLButtonElement>;

const Trigger = forwardRef<HTMLButtonElement, TriggerProps>(function Trigger({ label, size, tone, className, ...rest }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      aria-label={label}
      // Dialogs and sheets skip ⓘ triggers when choosing what to focus first.
      data-info-tip=""
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center rounded-pill align-middle transition-colors",
        "before:absolute before:content-['']",
        hitClass[size],
        "data-[state=open]:text-primary-strong data-[state=delayed-open]:text-primary-strong data-[state=instant-open]:text-primary-strong",
        toneClass[tone],
        className,
      )}
      {...rest}
    >
      <Info className={size === "sm" ? "size-4" : "size-5"} strokeWidth={2.25} aria-hidden />
    </button>
  );
});

/**
 * The ⓘ: the only place for explanatory text (no subtitles anywhere). Fine pointers get
 * a tooltip on hover and focus, and a click pins it open until Escape, an outside click
 * or a second click. Touch gets a tap-to-open popover that closes on an outside tap.
 *
 * ```tsx
 * <h2 className="flex items-center gap-2">Members <InfoTip content="People who can post here." /></h2>
 * ```
 */
export function InfoTip({ content, label = "More info", size = "sm", side = "top", align = "center", tone = "muted", className }: InfoTipProps) {
  const hoverCapable = useHoverCapable();
  const [open, setOpen] = useState(false);
  const [pinned, setPinned] = useState(false);

  if (hoverCapable) {
    return (
      <TooltipPrimitive.Provider delayDuration={120} skipDelayDuration={300}>
        <TooltipPrimitive.Root open={open || pinned} onOpenChange={setOpen}>
          <TooltipPrimitive.Trigger asChild>
            <Trigger
              label={label}
              size={size}
              tone={tone}
              className={className}
              aria-expanded={pinned}
              // Radix closes on pointerdown; the click then toggles the pin.
              onClick={() => setPinned((p) => !p)}
              onBlur={() => setPinned(false)}
            />
          </TooltipPrimitive.Trigger>
          <TooltipPrimitive.Portal>
            <TooltipPrimitive.Content
              side={side}
              align={align}
              sideOffset={8}
              collisionPadding={12}
              className={bubbleClasses}
              onEscapeKeyDown={() => setPinned(false)}
              onPointerDownOutside={() => setPinned(false)}
            >
              {content}
              <TooltipPrimitive.Arrow className="fill-inverse" width={12} height={6} />
            </TooltipPrimitive.Content>
          </TooltipPrimitive.Portal>
        </TooltipPrimitive.Root>
      </TooltipPrimitive.Provider>
    );
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <Trigger label={label} size={size} tone={tone} className={className} />
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          side={side}
          align={align}
          sideOffset={8}
          collisionPadding={12}
          className={cn(bubbleClasses, "origin-(--radix-popover-content-transform-origin) outline-none")}
        >
          {content}
          <PopoverPrimitive.Arrow className="fill-inverse" width={12} height={6} />
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}
