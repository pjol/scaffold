"use client";

import { motion, type HTMLMotionProps } from "motion/react";
import { useMotionTokens } from "./tokens";

export type PageTransitionProps = HTMLMotionProps<"div">;

/**
 * Page entrance: content fades in and rises 8 px on the gentle spring. Use it in a
 * route `template.tsx`, which re-mounts on every navigation.
 */
export function PageTransition({ children, ...rest }: PageTransitionProps) {
  const t = useMotionTokens();
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ ...t.spring.gentle, opacity: { duration: t.duration.base, ease: t.ease.out } }}
      {...rest}
    >
      {children}
    </motion.div>
  );
}
