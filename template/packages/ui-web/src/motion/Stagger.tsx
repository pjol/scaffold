"use client";

import { motion, type HTMLMotionProps, type Variants } from "motion/react";
import { createContext, forwardRef, useContext, useMemo } from "react";
import { useMotionTokens } from "./tokens";

type StaggerTag = "div" | "ul" | "ol" | "section";
type ItemTag = "div" | "li" | "article";

const ItemVariantsContext = createContext<Variants | null>(null);

export interface StaggerProps extends Omit<HTMLMotionProps<"div">, "variants"> {
  as?: StaggerTag;
  /** Seconds between children. Defaults to the palette stagger (45 ms). */
  stagger?: number;
  /** Seconds before the first child. */
  delay?: number;
  /** Start when scrolled into view. */
  inView?: boolean;
  /** Item entrance offset in px. @default 8 */
  y?: number;
}

/**
 * Staggered list entrance (list insertions, tiles). Wrap each child in {@link StaggerItem}.
 *
 * ```tsx
 * <Stagger as="ul">{rows.map((r) => <StaggerItem as="li" key={r.id}>…</StaggerItem>)}</Stagger>
 * ```
 */
export const Stagger = forwardRef<HTMLDivElement, StaggerProps>(function Stagger({ as = "div", stagger, delay = 0, inView = false, y = 8, children, ...rest }, ref) {
  const t = useMotionTokens();
  const container: Variants = useMemo(() => ({ hidden: {}, show: { transition: { staggerChildren: stagger ?? t.stagger, delayChildren: delay } } }), [stagger, delay, t.stagger]);
  const item: Variants = useMemo(
    () => ({
      hidden: { opacity: 0, y },
      show: { opacity: 1, y: 0, transition: { ...t.spring.gentle, opacity: { duration: t.duration.base, ease: t.ease.out } } },
    }),
    [y, t],
  );
  const Comp = motion[as] as typeof motion.div;
  return (
    <ItemVariantsContext.Provider value={item}>
      <Comp ref={ref} variants={container} initial="hidden" {...(inView ? { whileInView: "show", viewport: { once: true, margin: "-8% 0px" } } : { animate: "show" })} {...rest}>
        {children}
      </Comp>
    </ItemVariantsContext.Provider>
  );
});

export interface StaggerItemProps extends Omit<HTMLMotionProps<"div">, "variants"> {
  as?: ItemTag;
}

/** A child of {@link Stagger}. Supports `layout` for smooth reordering. */
export const StaggerItem = forwardRef<HTMLDivElement, StaggerItemProps>(function StaggerItem({ as = "div", ...rest }, ref) {
  const variants = useContext(ItemVariantsContext) ?? undefined;
  const Comp = motion[as] as typeof motion.div;
  return <Comp ref={ref} variants={variants} {...rest} />;
});
