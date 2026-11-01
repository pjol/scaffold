/**
 * Motion layer, built on `motion` (`motion/react`, the successor of framer-motion).
 * Springs come from the palette's motion tokens (`useMotionTokens`); `UIProvider`
 * mounts `MotionConfig reducedMotion="user"`. OriginKit components are added per
 * project, never vendored: see `./originkit/README.md`.
 */
export { Presence, Collapse, SwapText, type PresenceProps, type CollapseProps, type SwapTextProps } from "./Presence";
export { FadeIn, type FadeInProps } from "./FadeIn";
export { Stagger, StaggerItem, type StaggerProps, type StaggerItemProps } from "./Stagger";
export { PageTransition, type PageTransitionProps } from "./PageTransition";
export { motionTokens, useMotionTokens, type MotionTokensWeb, type SpringName } from "./tokens";
export { AnimatePresence, LayoutGroup, MotionConfig, motion, useReducedMotion, useSpring, useMotionValue, useTransform, animate } from "motion/react";
