import { Minus, Plus } from "lucide-react-native";
import { View, type StyleProp, type ViewStyle } from "react-native";
import { useControllableState } from "../lib/useControllable";
import { useTheme } from "../theme/ThemeProvider";
import { AnimatedNumber } from "./AnimatedNumber";
import { IconButton } from "./IconButton";

export interface StepperProps {
  value?: number;
  defaultValue?: number;
  onValueChange?: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
  /** Accessible name. @default "Quantity" */
  label?: string;
  disabled?: boolean;
  format?: (n: number) => string;
  style?: StyleProp<ViewStyle>;
}

/** − / + with a rolling count; adjustable for screen readers. */
export function Stepper({ value, defaultValue = 1, onValueChange, min = 0, max = 99, step = 1, label = "Quantity", disabled, format, style }: StepperProps) {
  const theme = useTheme();
  const s = theme.space;
  const [n, setN] = useControllableState({ value, defaultValue, onChange: onValueChange });
  const set = (v: number) => setN(Math.min(max, Math.max(min, v)));
  return (
    <View
      accessibilityRole="adjustable"
      accessibilityLabel={label}
      accessibilityValue={{ min, max, now: n }}
      accessibilityActions={[{ name: "increment" }, { name: "decrement" }]}
      onAccessibilityAction={(e) => set(e.nativeEvent.actionName === "increment" ? n + step : n - step)}
      style={[{ flexDirection: "row", alignItems: "center", gap: s[1], padding: s[1], borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, alignSelf: "flex-start", opacity: disabled ? 0.5 : 1 }, style]}
    >
      <IconButton label={`Decrease ${label.toLowerCase()}`} icon={Minus} variant="ghost" size="md" disabled={disabled || n <= min} onPress={() => set(n - step)} />
      <View style={{ minWidth: s[10], alignItems: "center" }}>
        <AnimatedNumber value={n} role="titleSm" {...(format ? { format } : {})} />
      </View>
      <IconButton label={`Increase ${label.toLowerCase()}`} icon={Plus} variant="ghost" size="md" disabled={disabled || n >= max} onPress={() => set(n + step)} />
    </View>
  );
}
