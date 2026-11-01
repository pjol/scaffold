import { useCallback } from "react";
import { useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { useTheme } from "../theme/ThemeProvider";

/**
 * Press feedback for a Pressable: an animated scale style plus handlers. Uses the
 * `press` spring and `pressScale` tokens; Reanimated skips the motion under the OS
 * reduce-motion setting (`ReduceMotion.System`).
 */
export function usePressScale(scaleTo?: number) {
  const theme = useTheme();
  const target = scaleTo ?? theme.motion.pressScale;
  const spring = theme.motion.spring.press;
  const pressed = useSharedValue(0);
  const style = useAnimatedStyle(() => ({ transform: [{ scale: 1 - (1 - target) * pressed.value }] }));
  const onPressIn = useCallback(() => {
    pressed.value = withSpring(1, spring);
  }, [pressed, spring]);
  const onPressOut = useCallback(() => {
    pressed.value = withSpring(0, spring);
  }, [pressed, spring]);
  return { style, pressed, onPressIn, onPressOut };
}
