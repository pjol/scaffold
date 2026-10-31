import type { NativeTheme } from "@scaffoldapp/tokens/native";
import { FadeIn, FadeInDown, FadeOut, LinearTransition, ZoomIn } from "react-native-reanimated";

/**
 * Entering/exiting/layout animations built from the motion tokens, mass always explicit
 * (Reanimated 4 defaults it to 4). Reanimated's builders default to
 * `ReduceMotion.System`, so each collapses to an instant change under the OS setting.
 *
 * ```tsx
 * <Animated.View entering={enterUp(theme, index)} exiting={fadeOut(theme)} layout={layoutSpring(theme)} />
 * ```
 */

/** Fade + 8 dp rise; `index` staggers list children by the `stagger` token (capped at 10). */
export function enterUp(theme: NativeTheme, index = 0) {
  const s = theme.motion.spring.gentle;
  return FadeInDown.withInitialValues({ opacity: 0, transform: [{ translateY: 8 }] })
    .delay(Math.min(index, 10) * theme.motion.stagger)
    .springify()
    .stiffness(s.stiffness)
    .damping(s.damping)
    .mass(s.mass);
}

/** Scale-in with visible overshoot (badges, success marks). Use sparingly. */
export function popIn(theme: NativeTheme, delay = 0) {
  const s = theme.motion.spring.bouncy;
  return ZoomIn.delay(delay).springify().stiffness(s.stiffness).damping(s.damping).mass(s.mass);
}

export function fadeIn(theme: NativeTheme, delay = 0) {
  return FadeIn.delay(delay).duration(theme.motion.duration.base);
}

export function fadeOut(theme: NativeTheme) {
  return FadeOut.duration(theme.motion.duration.fast);
}

/** Layout transition for rows inserted, removed or resized. */
export function layoutSpring(theme: NativeTheme) {
  const s = theme.motion.spring.gentle;
  return LinearTransition.springify().stiffness(s.stiffness).damping(s.damping).mass(s.mass);
}
