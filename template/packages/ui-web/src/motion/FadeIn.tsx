"use client";

import { motion, type HTMLMotionProps } from "motion/react";
import { forwardRef } from "react";
import { useMotionTokens } from "./tokens";

export interface FadeInProps extends HTMLMotionProps<"div"> {
  /** Seconds before starting. */
  delay?: number;
  /** Starting vertical offset in px. @default 8 */
  y?: number;
  /** Animate when scrolled into view instead of on mount. */
  inView?: boolean;
}

/** Mount entrance: fade plus a short rise on the gentle spring. Reduced motion: opacity only. */
export const FadeIn = forwardRef<HTMLDivElement, FadeInProps>(function FadeIn({ delay = 0, y = 8, inView = false, transition, ...rest }, ref) {
  const t = useMotionTokens();
  const hidden = { opacity: 0, y };
  const shown = { opacity: 1, y: 0 };
  return (
    <motion.div
      ref={ref}
      initial={hidden}
      {...(inView ? { whileInView: shown, viewport: { once: true, margin: "-10% 0px" } } : { animate: shown })}
      transition={{ ...t.spring.gentle, opacity: { duration: t.duration.base, ease: t.ease.out }, delay, ...transition }}
      {...rest}
    />
  );
});
