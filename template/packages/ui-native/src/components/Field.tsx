import { CircleAlert } from "lucide-react-native";
import { createContext, useContext, type ReactNode } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { enterUp, fadeOut, layoutSpring } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

const FieldContext = createContext<{ invalid: boolean; label: string } | null>(null);

/** The surrounding Field (Input reads it for the invalid border and accessible label). */
export const useField = () => useContext(FieldContext);

export interface FieldProps {
  /** Short label. No helper text: context goes in `info`. */
  label: string;
  info?: ReactNode;
  error?: string | null | undefined;
  required?: boolean;
  /** Right side of the label row (a text button, a counter). */
  labelEnd?: ReactNode;
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Label (+ⓘ) · control · animated error. Label→control 8, control→error 6. No outer margin. */
export function Field({ label, info, error, required, labelEnd, children, style }: FieldProps) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <FieldContext.Provider value={{ invalid: Boolean(error), label }}>
      <Animated.View layout={layoutSpring(theme)} style={[{ gap: s[2] }, style]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" style={{ flexShrink: 1 }}>
            {label}
            {required ? (
              <Text role="label" tone="danger" accessibilityLabel="required">
                {" *"}
              </Text>
            ) : null}
          </Text>
          {info ? <InfoTip content={info} label={`About ${label}`} /> : null}
          {labelEnd ? <View style={{ marginLeft: "auto" }}>{labelEnd}</View> : null}
        </View>
        <View style={{ gap: s[1.5] }}>
          {children}
          {error ? (
            <Animated.View entering={enterUp(theme)} exiting={fadeOut(theme)} accessibilityLiveRegion="assertive" style={{ flexDirection: "row", alignItems: "center", gap: s[1.5] }}>
              <CircleAlert size={16} color={theme.colors.dangerStrong} strokeWidth={2.25} />
              <Text role="label" tone="danger" style={{ flexShrink: 1 }}>
                {error}
              </Text>
            </Animated.View>
          ) : null}
        </View>
      </Animated.View>
    </FieldContext.Provider>
  );
}
