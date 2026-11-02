import { View, type StyleProp, type ViewStyle } from "react-native";
import Svg, { Path } from "react-native-svg";
import { Text } from "../components/Text";
import { useTheme } from "../theme/ThemeProvider";
import { sourceMark } from "./marks";

export interface ProviderIconProps {
  /** Platform id: google, microsoft, github, gitlab, notion, hubspot, salesforce, zoom (others get a monogram). */
  provider: string;
  /** Product within the platform (drive, docs, calendar, meet…): its own mark when it has one. */
  product?: string | null;
  /** dp. @default 16 */
  size?: number;
  /** Mark colour. @default the theme's text colour */
  color?: string;
  /** Accessible name. Omit when a visible label sits next to the icon. */
  title?: string;
  style?: StyleProp<ViewStyle>;
}

/** A platform's mark (Simple Icons, CC0) in one colour, or a neutral monogram tile (Microsoft, Salesforce, Zoom). */
export function ProviderIcon({ provider, product, size = 16, color, title, style }: ProviderIconProps) {
  const theme = useTheme();
  const c = theme.colors;
  const mark = sourceMark(provider, product);
  const a11y = title ? { accessible: true, accessibilityRole: "image" as const, accessibilityLabel: title } : { accessible: false, importantForAccessibility: "no-hide-descendants" as const };
  if ("path" in mark) {
    return (
      <View style={[{ width: size, height: size }, style]} {...a11y}>
        <Svg width={size} height={size} viewBox="0 0 24 24">
          <Path d={mark.path} fill={color ?? c.text} />
        </Svg>
      </View>
    );
  }
  return (
    <View
      style={[{ width: size, height: size, borderRadius: theme.radius.xs, backgroundColor: c.sunken, alignItems: "center", justifyContent: "center" }, style]}
      {...a11y}
    >
      <Text role="caption" weight={600} allowFontScaling={false} style={{ color: color ?? c.textMuted, fontSize: Math.max(9, Math.round(size * 0.62)), lineHeight: size }}>
        {mark.monogram}
      </Text>
    </View>
  );
}
