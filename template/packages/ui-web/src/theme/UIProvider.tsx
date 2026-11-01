"use client";

import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { MotionConfig } from "motion/react";
import type { ReactNode } from "react";

export interface UIProviderProps {
  children: ReactNode;
  /** Reduced-motion policy for every `motion` animation. `"user"` follows the OS setting. @default "user" */
  reducedMotion?: "user" | "always" | "never";
  /** Tooltip open delay in ms. @default 250 */
  tooltipDelay?: number;
}

/**
 * Root provider for ui-web: `MotionConfig reducedMotion="user"` (transforms and layout
 * animation stop when the OS asks for reduced motion) and the shared Radix tooltip
 * provider. Mount once in the app's client providers.
 */
export function UIProvider({ children, reducedMotion = "user", tooltipDelay = 250 }: UIProviderProps) {
  return (
    <MotionConfig reducedMotion={reducedMotion}>
      <TooltipPrimitive.Provider delayDuration={tooltipDelay} skipDelayDuration={400}>
        {children}
      </TooltipPrimitive.Provider>
    </MotionConfig>
  );
}
