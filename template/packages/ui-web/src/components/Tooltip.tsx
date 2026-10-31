"use client";

import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

/** Shared look for tooltip and info bubbles: inverse, high contrast, pops in. */
export const bubbleClasses =
  "z-(--app-z-tooltip) max-w-72 rounded-md bg-inverse px-3 py-2 type-label text-fg-inverse shadow-lg animate-pop-in data-[state=closed]:animate-pop-out origin-(--radix-tooltip-content-transform-origin)";

export interface TooltipProps {
  /** Short text. */
  content: ReactNode;
  /** One focusable element (rendered with `asChild`). */
  children: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  /** ms before opening. @default 250 */
  delay?: number;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  className?: string;
}

/** Hover/focus label for pointer devices (icon buttons). For explanations use `InfoTip`, which also works on touch. */
export function Tooltip({ content, children, side = "top", align = "center", delay = 250, open, onOpenChange, className }: TooltipProps) {
  return (
    <TooltipPrimitive.Provider delayDuration={delay} skipDelayDuration={400}>
      <TooltipPrimitive.Root {...(open !== undefined ? { open } : {})} {...(onOpenChange ? { onOpenChange } : {})}>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content side={side} align={align} sideOffset={8} collisionPadding={12} className={cn(bubbleClasses, className)}>
            {content}
            <TooltipPrimitive.Arrow className="fill-inverse" width={12} height={6} />
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
}
