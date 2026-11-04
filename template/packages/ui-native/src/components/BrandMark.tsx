import { Image, type ImageSource } from "expo-image";
import type { ImageStyle, StyleProp } from "react-native";
import { useTheme } from "../theme/ThemeProvider";

export interface BrandMarkProps {
  /** The logo: `require("../../assets/brand/github.png")` or a URL. One-colour marks need transparency (PNG or SVG). */
  source: ImageSource | number | string;
  /** A one-colour mark: tinted with `fill`. Leave false for a full-colour logo, shown as it is. */
  mask?: boolean;
  /** Tint for a mask. @default the theme's text colour */
  fill?: string;
  /** dp. @default 16 */
  size?: number;
  /** Accessible name. Omit when the name is written next to the mark. */
  label?: string;
  style?: StyleProp<ImageStyle>;
}

/** A real organisation's or product's logo; one-colour marks take the surface's colour (expo-image `tintColor`). */
export function BrandMark({ source, mask = false, fill, size = 16, label, style }: BrandMarkProps) {
  const theme = useTheme();
  const a11y = label ? { accessible: true, accessibilityRole: "image" as const, accessibilityLabel: label } : { accessible: false };
  return (
    <Image
      source={source}
      tintColor={mask ? (fill ?? theme.colors.text) : undefined}
      contentFit="contain"
      style={[{ width: size, height: size }, style]}
      {...a11y}
    />
  );
}
